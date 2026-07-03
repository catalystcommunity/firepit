package store

import (
	"time"

	"gorm.io/datatypes"
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
