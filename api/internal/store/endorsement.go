package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
)

// Endorsement mirrors the `endorsements` table. Retraction is a row
// delete, not a soft-delete flag.
type Endorsement struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	UserID     string    `gorm:"column:user_id;type:uuid;not null"`
	TargetType string    `gorm:"column:target_type;not null"`
	TargetID   string    `gorm:"column:target_id;type:uuid;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (Endorsement) TableName() string { return "endorsements" }

// ErrInvalidTargetType is returned by ResolveEndorsementTarget for a
// target_type other than "post"/"comment" — endorsing a board is
// meaningless (csil/types/endorsements.csil's doc comment).
var ErrInvalidTargetType = errors.New("store: invalid endorsement target_type")

// EndorsementTarget is what EndorsementService needs to know about a post
// or comment to validate and rank an endorsement of it: who authored it,
// which board it lives under (for the maintainer/moderator reputation
// tier and role badge), its post id (comments carry the same post id their
// notify.Event needs), and whether it's been tombstoned.
type EndorsementTarget struct {
	AuthorID  string
	BoardID   string
	PostID    string
	DeletedAt *time.Time
}

// ResolveEndorsementTarget looks up a post or comment by id, returning
// gorm.ErrRecordNotFound if it doesn't exist, or ErrInvalidTargetType if
// targetType is neither "post" nor "comment".
func (s *Store) ResolveEndorsementTarget(ctx context.Context, db *gorm.DB, targetType, targetID string) (*EndorsementTarget, error) {
	switch targetType {
	case "post":
		var p Post
		if err := db.WithContext(ctx).Select("id", "author_id", "board_id", "deleted_at").First(&p, "id = ?", targetID).Error; err != nil {
			return nil, err
		}
		return &EndorsementTarget{AuthorID: p.AuthorID, BoardID: p.BoardID, PostID: p.ID, DeletedAt: p.DeletedAt}, nil
	case "comment":
		var c Comment
		if err := db.WithContext(ctx).Select("id", "author_id", "post_id", "deleted_at").First(&c, "id = ?", targetID).Error; err != nil {
			return nil, err
		}
		var p Post
		if err := db.WithContext(ctx).Select("id", "board_id").First(&p, "id = ?", c.PostID).Error; err != nil {
			return nil, err
		}
		return &EndorsementTarget{AuthorID: c.AuthorID, BoardID: p.BoardID, PostID: p.ID, DeletedAt: c.DeletedAt}, nil
	default:
		return nil, ErrInvalidTargetType
	}
}

// FindEndorsement looks up userID's own endorsement of a target, or
// gorm.ErrRecordNotFound if none exists.
func (s *Store) FindEndorsement(ctx context.Context, db *gorm.DB, userID, targetType, targetID string) (*Endorsement, error) {
	var e Endorsement
	err := db.WithContext(ctx).First(&e, "user_id = ? AND target_type = ? AND target_id = ?", userID, targetType, targetID).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// InsertEndorsement creates userID's endorsement of a target, or — if one
// already exists (a concurrent double-endorse, or a plain re-endorse) —
// returns the existing row instead of erroring. This is what makes endorse
// idempotent even under a race: the uniqueness check happens in the
// database (endorsements' UNIQUE (user_id, target_type, target_id)
// constraint) via ON CONFLICT DO NOTHING, not via a separate
// check-then-insert in application code. The returned bool is true only
// when this call actually created the row — callers use it to decide
// whether to bump reputation / publish a notification, which must not
// happen again on a no-op re-endorse.
func (s *Store) InsertEndorsement(ctx context.Context, db *gorm.DB, userID, targetType, targetID string) (*Endorsement, bool, error) {
	var e Endorsement
	err := db.WithContext(ctx).Raw(`
		INSERT INTO endorsements (user_id, target_type, target_id)
		VALUES (?, ?, ?)
		ON CONFLICT (user_id, target_type, target_id) DO NOTHING
		RETURNING id, user_id, target_type, target_id, created_at
	`, userID, targetType, targetID).Scan(&e).Error
	if err != nil {
		return nil, false, err
	}
	if e.ID == "" {
		existing, ferr := s.FindEndorsement(ctx, db, userID, targetType, targetID)
		if ferr != nil {
			return nil, false, ferr
		}
		return existing, false, nil
	}
	return &e, true, nil
}

// DeleteEndorsement removes an endorsement row by id (retraction).
func (s *Store) DeleteEndorsement(ctx context.Context, db *gorm.DB, id string) error {
	return db.WithContext(ctx).Delete(&Endorsement{}, "id = ?", id).Error
}

// BoardRoleForUser returns userID's board_members.role on boardID
// ("maintainer"/"moderator"), or "" if they hold neither. This is always a
// live lookup against the current board_members state — the "snapshot"
// language in csil/types/endorsements.csil's Endorsement.role_badge doc
// comment means "computed once per response," not "persisted forever";
// there is no role_badge column in the endorsements table (PLANDOC.md §4).
func (s *Store) BoardRoleForUser(ctx context.Context, db *gorm.DB, boardID, userID string) (string, error) {
	var bm BoardMember
	err := db.WithContext(ctx).First(&bm, "board_id = ? AND user_id = ?", boardID, userID).Error
	if err != nil {
		if IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return bm.Role, nil
}

// IsTrustedDomain reports whether domain is present in trusted_domains.
func (s *Store) IsTrustedDomain(ctx context.Context, db *gorm.DB, domain string) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).Model(&TrustedDomain{}).Where("domain = ?", domain).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// EndorsementView is one row of a per-viewer ordered endorser list: the
// endorsement itself plus the ranking inputs sortEndorsementViews needs.
// RoleBadge is the endorsing user's CURRENT maintainer/moderator role on
// the target's board (empty if they hold neither).
type EndorsementView struct {
	Endorsement  Endorsement
	RoleBadge    string
	IsFriend     bool
	TrustedCount int
}

// ListEndorsementsForViewer returns every endorsement of (targetType,
// targetID), ordered per PLANDOC.md §4's "Endorser ordering" design note:
//
//  1. endorsers in any of viewerID's friend groups (nil viewerID —
//     anonymous — never has a friend tier, so this falls through to
//     reputation ordering only);
//  2. then by reputation: a maintainer/moderator role on the target's
//     board ranks above any endorser without one, and among those without
//     a role, a higher cached trusted-domain endorsement count ranks
//     higher;
//  3. stable tiebreak by the endorsement's own created_at (earliest
//     first), so equally-ranked endorsers keep a deterministic order.
//
// list-endorsements has no declared ServiceError arm (csil/firepit.csil):
// an unresolvable target reads as an empty list, not a failure, so this
// returns (nil, nil) rather than an error for a missing/invalid target.
func (s *Store) ListEndorsementsForViewer(ctx context.Context, viewerID *string, targetType, targetID string) ([]EndorsementView, error) {
	target, err := s.ResolveEndorsementTarget(ctx, s.DB, targetType, targetID)
	if err != nil {
		if IsNotFound(err) || errors.Is(err, ErrInvalidTargetType) {
			return nil, nil
		}
		return nil, err
	}

	var endorsements []Endorsement
	if err := s.DB.WithContext(ctx).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("created_at asc").
		Find(&endorsements).Error; err != nil {
		return nil, err
	}
	if len(endorsements) == 0 {
		return nil, nil
	}

	userIDs := make([]string, len(endorsements))
	for i, e := range endorsements {
		userIDs[i] = e.UserID
	}

	roles := map[string]string{}
	var boardMembers []BoardMember
	if err := s.DB.WithContext(ctx).
		Where("board_id = ? AND user_id IN ?", target.BoardID, userIDs).
		Find(&boardMembers).Error; err != nil {
		return nil, err
	}
	for _, bm := range boardMembers {
		roles[bm.UserID] = bm.Role
	}

	reps := map[string]int{}
	var repRows []UserReputation
	if err := s.DB.WithContext(ctx).Where("user_id IN ?", userIDs).Find(&repRows).Error; err != nil {
		return nil, err
	}
	for _, r := range repRows {
		reps[r.UserID] = r.TrustedEndorsementCount
	}

	friends := map[string]bool{}
	if viewerID != nil {
		var memberIDs []string
		if err := s.DB.WithContext(ctx).
			Model(&FriendGroupMember{}).
			Joins("JOIN friend_groups ON friend_groups.id = friend_group_members.group_id").
			Where("friend_groups.owner_id = ? AND friend_group_members.member_user_id IN ?", *viewerID, userIDs).
			Pluck("friend_group_members.member_user_id", &memberIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range memberIDs {
			friends[id] = true
		}
	}

	views := make([]EndorsementView, len(endorsements))
	for i, e := range endorsements {
		views[i] = EndorsementView{
			Endorsement:  e,
			RoleBadge:    roles[e.UserID],
			IsFriend:     friends[e.UserID],
			TrustedCount: reps[e.UserID],
		}
	}
	sortEndorsementViews(views)
	return views, nil
}

// sortEndorsementViews orders views in place per PLANDOC.md §4 "Endorser
// ordering" (see ListEndorsementsForViewer's doc comment for the exact
// tiers). It's a pure function over already-fetched data — no database
// access — specifically so the comparator itself is unit-testable without
// a container.
func sortEndorsementViews(views []EndorsementView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.IsFriend != b.IsFriend {
			return a.IsFriend
		}
		aRole, bRole := a.RoleBadge != "", b.RoleBadge != ""
		if aRole != bRole {
			return aRole
		}
		if a.TrustedCount != b.TrustedCount {
			return a.TrustedCount > b.TrustedCount
		}
		return a.Endorsement.CreatedAt.Before(b.Endorsement.CreatedAt)
	})
}
