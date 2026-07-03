package store

import "time"

// Board mirrors the `boards` table.
type Board struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	Slug        string     `gorm:"column:slug;not null"`
	Title       string     `gorm:"column:title;not null"`
	Description string     `gorm:"column:description;not null;default:''"`
	Kind        string     `gorm:"column:kind;type:board_kind;not null;default:'discussion'"`
	CreatedBy   string     `gorm:"column:created_by;type:uuid;not null"`
	ArchivedAt  *time.Time `gorm:"column:archived_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
}

func (Board) TableName() string { return "boards" }

// BoardMember mirrors the `board_members` table: per-board maintainer /
// moderator role assignment.
type BoardMember struct {
	BoardID   string    `gorm:"column:board_id;type:uuid;primaryKey"`
	UserID    string    `gorm:"column:user_id;type:uuid;primaryKey"`
	Role      string    `gorm:"column:role;type:board_member_role;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (BoardMember) TableName() string { return "board_members" }

// TrustedDomain mirrors the `trusted_domains` table: instance-level,
// admin-managed linkkeys domains whose endorsements earn reputation.
type TrustedDomain struct {
	Domain    string    `gorm:"column:domain;primaryKey"`
	AddedBy   string    `gorm:"column:added_by;type:uuid;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (TrustedDomain) TableName() string { return "trusted_domains" }
