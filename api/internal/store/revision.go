package store

import "time"

// Revision mirrors the `revisions` table: a snapshot of a post/comment's
// title+body taken BEFORE each edit. TargetType is a plain
// CHECK-constrained column (not a Postgres enum) because the same
// target_type/target_id pairing recurs, with different allowed value
// sets, across revisions/endorsements/subscriptions/unread_overrides/
// notifications — matching longhouse's polymorphic-reference convention.
type Revision struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	TargetType string    `gorm:"column:target_type;not null"`
	TargetID   string    `gorm:"column:target_id;type:uuid;not null"`
	EditorID   string    `gorm:"column:editor_id;type:uuid;not null"`
	PrevTitle  *string   `gorm:"column:prev_title"`
	PrevBodyMD string    `gorm:"column:prev_body_md;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (Revision) TableName() string { return "revisions" }
