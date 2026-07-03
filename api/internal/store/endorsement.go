package store

import "time"

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
