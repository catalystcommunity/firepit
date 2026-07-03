package store

import (
	"context"
	"errors"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// uuidPattern matches a well-formed UUID textual representation, the only
// shape every id column in this schema (uuid-typed, populated by
// generate_ulid()) will ever accept. Checked before querying so a malformed
// target_id (a client typo, or a caller probing the API) reports as "not
// found" — the correct, safe read per csil/types/errors.csil ("a resource
// the caller isn't permitted to know exists... should also read as
// NotFound") — rather than surfacing a raw "invalid input syntax for type
// uuid" Postgres error as an internal failure.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Subscription mirrors the `subscriptions` table: a subscribe to a board,
// a post, or a comment subtree. Muted carves holes out of a broader
// subscription (e.g. sub a board, mute one post).
type Subscription struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	UserID     string    `gorm:"column:user_id;type:uuid;not null"`
	TargetType string    `gorm:"column:target_type;not null"`
	TargetID   string    `gorm:"column:target_id;type:uuid;not null"`
	Muted      bool      `gorm:"column:muted;not null;default:false"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (Subscription) TableName() string { return "subscriptions" }

// targetExists reports whether targetID names a live (non-deleted) row of
// targetType ("board", "post", or "comment"), for validating subscription
// and read/unread targets before writing anything referencing them. It's a
// deliberately minimal ad hoc query (rather than a shared GetBoard/GetPost
// GetComment repository method) so this file doesn't need to add methods to
// board.go/post.go/comment.go, which other Wave B tasks (B3, B4) own and are
// landing concurrently — see doc.go's package comment on cross-task file
// ownership.
func targetExists(ctx context.Context, db *gorm.DB, targetType, targetID string) (bool, error) {
	var table string
	switch targetType {
	case "board":
		table = "boards"
	case "post":
		table = "posts"
	case "comment":
		table = "comments"
	default:
		return false, nil
	}
	if !uuidPattern.MatchString(targetID) {
		return false, nil
	}
	var count int64
	q := db.WithContext(ctx).Table(table).Where("id = ?", targetID)
	if table != "boards" {
		q = q.Where("deleted_at IS NULL")
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// TargetExists is the exported form of targetExists, for callers outside
// this package (csilservices) that need to validate a TargetRef before
// acting on it.
func (s *Store) TargetExists(ctx context.Context, targetType, targetID string) (bool, error) {
	return targetExists(ctx, s.DB, targetType, targetID)
}

// FindSubscription returns the caller's subscription to (targetType,
// targetID), or ErrNotFound (see IsNotFound) if none exists.
func (s *Store) FindSubscription(ctx context.Context, userID, targetType, targetID string) (*Subscription, error) {
	var sub Subscription
	err := s.DB.WithContext(ctx).
		Where("user_id = ? AND target_type = ? AND target_id = ?", userID, targetType, targetID).
		First(&sub).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// Subscribe creates a subscription to (targetType, targetID) for userID, or
// returns the existing one unchanged if the caller is already subscribed —
// subscribing is idempotent (PLANDOC.md §7 B6 accept criteria), and a
// repeat call must not clobber an existing mute.
func (s *Store) Subscribe(ctx context.Context, userID, targetType, targetID string) (*Subscription, error) {
	return s.upsertSubscription(ctx, userID, targetType, targetID, nil)
}

// SetSubscriptionMuted sets muted on the caller's subscription to
// (targetType, targetID), creating the subscription first if it doesn't
// already exist — set-muted is how a caller carves a mute hole out of a
// broader subscription (e.g. a board sub) for a specific post/comment they
// were never separately subscribed to (PLANDOC.md §4): the subscription row
// itself is what a broader scope's query later finds and excludes, so it
// must exist for the mute to have anything to attach to.
func (s *Store) SetSubscriptionMuted(ctx context.Context, userID, targetType, targetID string, muted bool) (*Subscription, error) {
	return s.upsertSubscription(ctx, userID, targetType, targetID, &muted)
}

// upsertSubscription is the shared find-or-create behind Subscribe and
// SetSubscriptionMuted. If muted is nil (Subscribe's case), an existing row
// is returned unmodified; if non-nil (SetSubscriptionMuted's case), an
// existing row has its muted flag updated to *muted.
func (s *Store) upsertSubscription(ctx context.Context, userID, targetType, targetID string, muted *bool) (*Subscription, error) {
	var sub Subscription
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		findExisting := func() (*Subscription, error) {
			var existing Subscription
			err := tx.Where("user_id = ? AND target_type = ? AND target_id = ?", userID, targetType, targetID).
				First(&existing).Error
			if err != nil {
				return nil, err
			}
			return &existing, nil
		}

		existing, err := findExisting()
		if err == nil {
			if muted != nil && existing.Muted != *muted {
				if err := tx.Model(&Subscription{}).
					Where("user_id = ? AND target_type = ? AND target_id = ?", userID, targetType, targetID).
					Update("muted", *muted).Error; err != nil {
					return err
				}
				existing.Muted = *muted
			}
			sub = *existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		created := &Subscription{
			UserID:     userID,
			TargetType: targetType,
			TargetID:   targetID,
		}
		if muted != nil {
			created.Muted = *muted
		}
		// ON CONFLICT DO NOTHING guards the race between the findExisting
		// miss above and this insert (two concurrent subscribe calls for the
		// same target); on conflict, re-read the (now-existing) row so the
		// caller still gets a Subscription back rather than a partially
		// unpopulated one.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(created).Error; err != nil {
			return err
		}
		if created.ID == "" {
			existing, err := findExisting()
			if err != nil {
				return err
			}
			sub = *existing
			return nil
		}
		sub = *created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// Unsubscribe removes the caller's subscription to (targetType, targetID).
// Unsubscribing when no such subscription exists is a no-op, not an error —
// unsubscribe is idempotent both ways.
func (s *Store) Unsubscribe(ctx context.Context, userID, targetType, targetID string) error {
	return s.DB.WithContext(ctx).
		Where("user_id = ? AND target_type = ? AND target_id = ?", userID, targetType, targetID).
		Delete(&Subscription{}).Error
}

// ListSubscriptions returns every subscription belonging to userID, oldest
// first.
func (s *Store) ListSubscriptions(ctx context.Context, userID string) ([]Subscription, error) {
	var subs []Subscription
	err := s.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&subs).Error
	return subs, err
}
