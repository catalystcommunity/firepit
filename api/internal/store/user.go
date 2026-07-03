package store

import (
	"time"
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
