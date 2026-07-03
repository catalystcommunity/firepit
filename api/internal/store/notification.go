package store

import "time"

// Notification mirrors the `notifications` table — a fan-out target.
// SubscriptionID/PostID are convenience denormalizations for the inbox
// query; TargetType/TargetID is the thing that actually triggered it.
type Notification struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	UserID         string     `gorm:"column:user_id;type:uuid;not null"`
	Event          string     `gorm:"column:event;type:notification_event;not null"`
	SubscriptionID *string    `gorm:"column:subscription_id;type:uuid"`
	ActorID        *string    `gorm:"column:actor_id;type:uuid"`
	TargetType     string     `gorm:"column:target_type;not null"`
	TargetID       string     `gorm:"column:target_id;type:uuid;not null"`
	PostID         *string    `gorm:"column:post_id;type:uuid"`
	ReadAt         *time.Time `gorm:"column:read_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
}

func (Notification) TableName() string { return "notifications" }
