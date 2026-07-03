package store

import "time"

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

// ReadMark mirrors the `read_marks` table: a per-post read watermark,
// advanced when the user views the thread.
type ReadMark struct {
	UserID     string    `gorm:"column:user_id;type:uuid;primaryKey"`
	PostID     string    `gorm:"column:post_id;type:uuid;primaryKey"`
	LastReadAt time.Time `gorm:"column:last_read_at;not null"`
}

func (ReadMark) TableName() string { return "read_marks" }

// UnreadOverride mirrors the `unread_overrides` table: an explicit "keep
// this unread" pin that beats the watermark until cleared.
type UnreadOverride struct {
	UserID     string    `gorm:"column:user_id;type:uuid;primaryKey"`
	TargetType string    `gorm:"column:target_type;primaryKey"`
	TargetID   string    `gorm:"column:target_id;type:uuid;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (UnreadOverride) TableName() string { return "unread_overrides" }
