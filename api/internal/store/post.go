package store

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Post mirrors the `posts` table — thread roots.
type Post struct {
	ID             string         `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	BoardID        string         `gorm:"column:board_id;type:uuid;not null"`
	AuthorID       string         `gorm:"column:author_id;type:uuid;not null"`
	Title          string         `gorm:"column:title;not null"`
	BodyMD         string         `gorm:"column:body_md;not null;default:''"`
	Origin         string         `gorm:"column:origin;type:content_origin;not null;default:'user'"`
	OriginRef      datatypes.JSON `gorm:"column:origin_ref;type:jsonb"`
	CommentCount   int            `gorm:"column:comment_count;not null;default:0"`
	LastActivityAt time.Time      `gorm:"column:last_activity_at;not null"`
	EditedAt       *time.Time     `gorm:"column:edited_at"`
	DeletedAt      *time.Time     `gorm:"column:deleted_at"`
	CreatedAt      time.Time      `gorm:"column:created_at;not null"`
}

func (Post) TableName() string { return "posts" }

// PostListCursor identifies the boundary row of a previously-fetched
// list-posts page: keyset pagination over posts ordered by
// (last_activity_at DESC, id DESC). Encoding/decoding this to/from the
// wire's opaque PageCursor is csilservices.ThreadService's job (this type
// only exists so ListPostsByBoard doesn't need to import csil).
type PostListCursor struct {
	LastActivityAt time.Time
	ID             string
}

// CreatePost inserts p via db.
func (s *Store) CreatePost(ctx context.Context, db *gorm.DB, p *Post) error {
	return db.WithContext(ctx).Create(p).Error
}

// GetPost looks up a post by id via db, returning gorm.ErrRecordNotFound
// (see IsNotFound) if there is none. Soft-deleted posts are still returned
// (get-thread still needs to render a tombstoned original post whose
// replies survive it) — only ListPostsByBoard excludes them.
func (s *Store) GetPost(ctx context.Context, db *gorm.DB, id string) (*Post, error) {
	var post Post
	if err := db.WithContext(ctx).First(&post, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

// ListPostsByBoard returns up to limit posts on boardID, newest activity
// first (last_activity_at DESC, id DESC as a stable tiebreak for
// same-instant activity), optionally starting strictly after a previous
// page's boundary row (after). Soft-deleted posts are excluded — unlike a
// permalinked thread (GetPost), a deleted post has no reason to keep
// occupying a slot in the activity feed.
//
// Callers typically pass limit+1 to detect whether another page follows
// without a second COUNT query; ListPostsByBoard itself just executes the
// keyset predicate, it doesn't special-case that.
func (s *Store) ListPostsByBoard(ctx context.Context, db *gorm.DB, boardID string, after *PostListCursor, limit int) ([]Post, error) {
	q := db.WithContext(ctx).Where("board_id = ? AND deleted_at IS NULL", boardID)
	if after != nil {
		// Row-value comparison: strictly "before" the cursor in the same
		// (last_activity_at DESC, id DESC) order used below, so a post
		// inserted/bumped after the cursor was taken never retroactively
		// appears on a later page and never shifts rows already handed out
		// — the keyset property that makes this stable under concurrent
		// inserts (as opposed to OFFSET-based paging).
		q = q.Where("(last_activity_at, id) < (?, ?)", after.LastActivityAt, after.ID)
	}
	var posts []Post
	err := q.Order("last_activity_at DESC, id DESC").Limit(limit).Find(&posts).Error
	return posts, err
}

// UpdatePost persists a post's mutated fields (edit-post loads a Post,
// mutates Title/BodyMD/EditedAt, then calls this inside the same
// transaction as the revision snapshot).
func (s *Store) UpdatePost(ctx context.Context, db *gorm.DB, p *Post) error {
	return db.WithContext(ctx).Save(p).Error
}

// SoftDeletePost tombstones a post: sets deleted_at only. The row and its
// comments remain exactly as they were (PLANDOC.md §4) — redaction for
// display is a csilservices-layer concern.
func (s *Store) SoftDeletePost(ctx context.Context, db *gorm.DB, id string, deletedAt time.Time) error {
	return db.WithContext(ctx).Model(&Post{}).Where("id = ?", id).
		Update("deleted_at", deletedAt).Error
}

// BumpPostActivity advances a post's last_activity_at and adjusts
// comment_count by commentDelta in one UPDATE, called inside the same
// transaction as a new comment's INSERT so the two never drift apart.
func (s *Store) BumpPostActivity(ctx context.Context, db *gorm.DB, postID string, at time.Time, commentDelta int) error {
	return db.WithContext(ctx).Model(&Post{}).Where("id = ?", postID).
		Updates(map[string]any{
			"last_activity_at": at,
			"comment_count":    gorm.Expr("comment_count + ?", commentDelta),
		}).Error
}
