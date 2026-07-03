package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// User mirrors the `users` table.
type User struct {
	ID             string      `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	LinkkeysDomain string      `gorm:"column:linkkeys_domain;not null"`
	LinkkeysUserID string      `gorm:"column:linkkeys_user_id;not null"`
	Handle         string      `gorm:"column:handle;not null"`
	DisplayName    string      `gorm:"column:display_name;not null;default:''"`
	Kind           string      `gorm:"column:kind;type:user_kind;not null;default:'human'"`
	Roles          StringArray `gorm:"column:roles;type:text[];not null"`
	CreatedAt      time.Time   `gorm:"column:created_at;not null"`
}

func (User) TableName() string { return "users" }

// GetUser looks up a user by id, returning gorm.ErrRecordNotFound (see
// IsNotFound in session.go) if there is none.
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	var user User
	if err := s.DB.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByHandle looks up a user by their exact handle (case-sensitive,
// matching the `users_handle_idx` unique index this exact-matches against),
// returning gorm.ErrRecordNotFound (see IsNotFound) if there is none. Used by
// SocialService.resolve-user (csil/types/social.csil's Handle) so the UI can
// turn a typed "@handle" into the UserID that grant-mention/add-friend
// actually take.
func (s *Store) GetUserByHandle(ctx context.Context, handle string) (*User, error) {
	var user User
	if err := s.DB.WithContext(ctx).First(&user, "handle = ?", handle).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// maxHandleAttempts bounds the deterministic "-2", "-3", ... suffix retry
// loop in UpsertUser before falling back to a random suffix. 20 is generous
// headroom for any realistic handle collision run (see UpsertUser's doc
// comment for the full strategy).
const maxHandleAttempts = 20

// UpsertUser resolves a `users` row for a verified linkkeys identity
// (task B2's GET /auth/callback: PLANDOC.md §3), inserting one on first
// login and returning the existing row on every login after that.
//
// # Identity vs. handle
//
// The lookup key is always (linkkeysDomain, linkkeysUserID) — the schema's
// own UNIQUE constraint (coredb/migrations/000001_baseline.sql) and the only
// thing that identifies "the same person" across logins. When a row already
// exists, it is returned completely unchanged: an existing user's handle and
// display_name are sticky across logins, never silently overwritten by
// whatever the IDP happens to release this time (a user may have since
// customized either — there is no such customization op in v1, but nothing
// here should assume there never will be one).
//
// # Handle-collision strategy
//
// A brand new row needs a `handle` value, globally unique
// (`users_handle_idx`) so @mention parsing (PLANDOC.md §4) is unambiguous.
// baseHandle is the caller's best derived candidate (see
// csilservices.DeriveHandleCandidate — a claimed handle, else a slugified
// display name, else the raw linkkeys user id). This method does NOT
// pre-check availability with a SELECT (a check-then-insert race under
// concurrent first logins from two different identities that happen to slug
// to the same candidate); instead it optimistically INSERTs baseHandle, and
// on a unique-violation naming users_handle_idx specifically, retries with
// baseHandle + "-2", "-3", ... up to maxHandleAttempts. If every deterministic
// suffix is somehow also taken (astronomically unlikely outside adversarial
// input), one final attempt appends a short random hex suffix so this call
// always terminates rather than erroring out a real login. A unique
// violation on the DIFFERENT (linkkeys_domain, linkkeys_user_id) constraint
// instead means a concurrent login for the SAME identity won the race; that
// row is looked up and returned rather than treated as an error.
func (s *Store) UpsertUser(ctx context.Context, linkkeysDomain, linkkeysUserID, baseHandle, displayName string) (*User, error) {
	existing, err := s.findUserByIdentity(ctx, linkkeysDomain, linkkeysUserID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	candidate := baseHandle
	for attempt := 2; attempt <= maxHandleAttempts+1; attempt++ {
		user := &User{
			LinkkeysDomain: linkkeysDomain,
			LinkkeysUserID: linkkeysUserID,
			Handle:         candidate,
			DisplayName:    displayName,
			Kind:           "human",
			Roles:          StringArray{},
		}
		err := s.DB.WithContext(ctx).Create(user).Error
		if err == nil {
			return user, nil
		}
		switch constraintOf(err) {
		case "users_linkkeys_domain_linkkeys_user_id_key":
			// Lost a race with a concurrent login for the same identity.
			winner, lookupErr := s.findUserByIdentity(ctx, linkkeysDomain, linkkeysUserID)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if winner != nil {
				return winner, nil
			}
			return nil, err
		case "users_handle_idx":
			candidate = fmt.Sprintf("%s-%d", baseHandle, attempt)
			continue
		default:
			return nil, err
		}
	}

	// Exhausted every deterministic suffix (effectively unreachable in
	// practice): fall back to a random suffix so this call always
	// terminates instead of failing a real login over handle contention.
	candidate = baseHandle + "-" + randomHexSuffix()
	user := &User{
		LinkkeysDomain: linkkeysDomain,
		LinkkeysUserID: linkkeysUserID,
		Handle:         candidate,
		DisplayName:    displayName,
		Kind:           "human",
		Roles:          StringArray{},
	}
	if err := s.DB.WithContext(ctx).Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// GetUsersByIDs batch-loads every user in ids in one query, returned as a
// map keyed by id — the shape every list-response handle-denormalization
// caller (ThreadService.ListPosts/GetThread, NotificationService.
// ListNotifications, EndorsementService.ListEndorsements) needs: look up
// once per page/response, not once per row (avoiding the N+1 a per-row
// GetUser call would cause). A ULID missing from the returned map (deleted
// user, or a bad id) is simply absent — callers treat that the same as "no
// handle to denormalize" rather than an error, mirroring GetUser's
// not-found-as-error convention being deliberately NOT used here (a partial
// miss shouldn't fail the whole page). An empty/nil ids returns an empty map
// without querying.
func (s *Store) GetUsersByIDs(ctx context.Context, ids []string) (map[string]User, error) {
	out := make(map[string]User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// De-duplicate before querying — the same author/actor id commonly
	// repeats across a page of posts/comments/notifications, and there's no
	// reason to ask Postgres to match the same value in an IN-list more than
	// once.
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return out, nil
	}

	var users []User
	if err := s.DB.WithContext(ctx).Where("id IN ?", unique).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = u
	}
	return out, nil
}

// findUserByIdentity returns the users row for (linkkeysDomain,
// linkkeysUserID), or (nil, nil) if there is none — distinct from GetUser's
// error-returning not-found convention because UpsertUser treats "no row
// yet" as a normal branch, not a failure.
func (s *Store) findUserByIdentity(ctx context.Context, linkkeysDomain, linkkeysUserID string) (*User, error) {
	var user User
	err := s.DB.WithContext(ctx).
		Where("linkkeys_domain = ? AND linkkeys_user_id = ?", linkkeysDomain, linkkeysUserID).
		First(&user).Error
	if err == nil {
		return &user, nil
	}
	if IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

// constraintOf extracts the violated Postgres constraint/index name from err,
// or "" if err isn't a unique-violation *pgconn.PgError. UpsertUser uses this
// to tell a handle collision (retry with a new candidate) apart from an
// identity-race (look up and return the winner) apart from any other error
// (propagate as-is).
func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return pgErr.ConstraintName
	}
	return ""
}

// randomHexSuffix returns a short random hex string for UpsertUser's
// last-resort handle disambiguation.
func randomHexSuffix() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// UserSettings mirrors the `user_settings` table (1:1 with User).
type UserSettings struct {
	UserID          string    `gorm:"column:user_id;type:uuid;primaryKey"`
	MentionPolicy   string    `gorm:"column:mention_policy;type:mention_policy;not null;default:'subscribed'"`
	NotifyOnEndorse bool      `gorm:"column:notify_on_endorse;not null;default:true"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null"`
}

func (UserSettings) TableName() string { return "user_settings" }

// MentionGrant mirrors the `mention_grants` table: granted_user_id may
// always @mention-notify user_id.
type MentionGrant struct {
	UserID        string    `gorm:"column:user_id;type:uuid;primaryKey"`
	GrantedUserID string    `gorm:"column:granted_user_id;type:uuid;primaryKey"`
	CreatedAt     time.Time `gorm:"column:created_at;not null"`
}

func (MentionGrant) TableName() string { return "mention_grants" }
