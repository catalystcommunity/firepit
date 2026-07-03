package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Comment mirrors the `comments` table. Path is the ltree materialized
// path enabling a one-query, depth-first subtree fetch (see PLANDOC.md §4
// design notes); ParentCommentID carries the actual tree structure.
type Comment struct {
	ID              string         `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	PostID          string         `gorm:"column:post_id;type:uuid;not null"`
	ParentCommentID *string        `gorm:"column:parent_comment_id;type:uuid"`
	AuthorID        string         `gorm:"column:author_id;type:uuid;not null"`
	Path            Ltree          `gorm:"column:path;type:ltree;not null"`
	BodyMD          string         `gorm:"column:body_md;not null;default:''"`
	Origin          string         `gorm:"column:origin;type:content_origin;not null;default:'user'"`
	OriginRef       datatypes.JSON `gorm:"column:origin_ref;type:jsonb"`
	EditedAt        *time.Time     `gorm:"column:edited_at"`
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`
	CreatedAt       time.Time      `gorm:"column:created_at;not null"`
}

func (Comment) TableName() string { return "comments" }

// NewCommentID mints a comment id using the exact same scheme as coredb's
// generate_ulid() Postgres function (000001_baseline.sql): a 12-hex-digit
// millisecond-epoch timestamp prefix, left-padded with zeros, followed by
// 20 hex digits of crypto-random suffix, formatted as a canonical
// (dashed) uuid.
//
// ThreadService needs a comment's id *before* the INSERT, because the row's
// own `path` column is "parent's path + this comment's own label" — unlike
// every other primary key in this schema, path can't be filled in from a
// RETURNING clause after the fact. Every other table lets Postgres's
// generate_ulid() DEFAULT assign the id and reads it back via gorm's
// RETURNING support; comments are the one table that must mint client-side.
//
// # ltree label scheme
//
// A materialized path is a dot-separated sequence of labels, and ltree
// restricts label characters. It's tempting to assume (as the org's other
// ULID usage might suggest) that a dashed uuid string needs its dashes
// stripped to become a valid label — but verified directly against a live
// Postgres 17 + contrib/ltree ('SELECT 'abc-def'::ltree' succeeds; only
// whitespace/other punctuation like ' ' or '@' is rejected), '-' IS a legal
// ltree label character in this Postgres version. So a comment's path
// label is simply its dashed uuid id, used verbatim — no
// stripping/round-tripping needed. See ChildPath.
//
// Keeping the SAME format as generate_ulid() (rather than, say, a random
// v4 uuid) matters beyond consistency: labels sharing one fixed-width,
// same-charset, time-prefixed format sort byte-for-byte in creation order,
// which is what makes "ORDER BY path" produce a chronological depth-first
// traversal (see ListCommentsByPost) instead of a random one.
func NewCommentID() (string, error) {
	millis := time.Now().UnixMilli()
	if millis < 0 {
		return "", fmt.Errorf("store: comment id timestamp is negative")
	}
	ts := fmt.Sprintf("%012x", millis)
	if len(ts) > 12 {
		// Past year ~10889; generate_ulid()'s lpad(...,12,'0') would grow
		// past 12 chars too rather than truncate, so this keeps parity
		// instead of silently corrupting the timestamp.
		return "", fmt.Errorf("store: comment id timestamp overflowed 12 hex digits: %s", ts)
	}
	randBytes := make([]byte, 10)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("store: generating comment id randomness: %w", err)
	}
	raw := ts + hex.EncodeToString(randBytes)
	return fmt.Sprintf("%s-%s-%s-%s-%s", raw[0:8], raw[8:12], raw[12:16], raw[16:20], raw[20:32]), nil
}

// ChildPath returns the ltree path for a comment identified by selfID:
// parentPath's labels followed by selfID's own label, or just selfID's
// label when parentPath is empty (a top-level reply directly on the post,
// which has no comment ancestor to prefix).
func ChildPath(parentPath Ltree, selfID string) Ltree {
	if parentPath == "" {
		return Ltree(selfID)
	}
	return Ltree(string(parentPath) + "." + selfID)
}

// CreateComment inserts c via db (either the Store's own *gorm.DB for a
// standalone write, or an open *gorm.DB transaction so the insert commits
// atomically with the post's comment_count/last_activity_at bump and the
// notify.Publisher call ThreadService.CreateComment makes in the same tx).
func (s *Store) CreateComment(ctx context.Context, db *gorm.DB, c *Comment) error {
	return db.WithContext(ctx).Create(c).Error
}

// GetComment looks up a comment by id via db, returning
// gorm.ErrRecordNotFound (see IsNotFound) if there is none.
func (s *Store) GetComment(ctx context.Context, db *gorm.DB, id string) (*Comment, error) {
	var c Comment
	if err := db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCommentsByPost fetches every comment on postID in one query, ordered
// by path — the single indexed-WHERE (post_id) + sort-by-path query that
// gives get-thread a depth-first-ordered comment list (see the doc comment
// on NewCommentID for why '.'-joined dashed-uuid labels sort chronologically
// within siblings, and PLANDOC.md §4's threading design note for why this is
// one query rather than a per-level walk). Includes soft-deleted
// (tombstoned) rows; redacting a tombstoned comment's body/author for
// display is a csilservices-layer concern (ThreadService), not this layer's
// — the row itself keeps its real content so a moderator/undelete path
// always has it.
func (s *Store) ListCommentsByPost(ctx context.Context, postID string) ([]Comment, error) {
	var comments []Comment
	err := s.DB.WithContext(ctx).
		Where("post_id = ?", postID).
		Order("path").
		Find(&comments).Error
	return comments, err
}

// UpdateCommentBody applies an edit's new body and edited_at via db (called
// inside the same transaction as the revision snapshot).
func (s *Store) UpdateCommentBody(ctx context.Context, db *gorm.DB, id, body string, editedAt time.Time) error {
	return db.WithContext(ctx).Model(&Comment{}).Where("id = ?", id).
		Updates(map[string]any{"body_md": body, "edited_at": editedAt}).Error
}

// SoftDeleteComment tombstones a comment: sets deleted_at, leaves every
// other column (including body_md/author_id and the row's descendants)
// untouched so the thread's shape survives (PLANDOC.md §4).
func (s *Store) SoftDeleteComment(ctx context.Context, db *gorm.DB, id string, deletedAt time.Time) error {
	return db.WithContext(ctx).Model(&Comment{}).Where("id = ?", id).
		Update("deleted_at", deletedAt).Error
}
