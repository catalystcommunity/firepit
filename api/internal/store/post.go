package store

import (
	"time"

	"gorm.io/datatypes"
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
