package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

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

// CreateRevision inserts r via db — called inside the same transaction as
// the edit it snapshots (r holds the content as it was BEFORE that edit),
// so an edit and its history entry always commit or roll back together.
func (s *Store) CreateRevision(ctx context.Context, db *gorm.DB, r *Revision) error {
	return db.WithContext(ctx).Create(r).Error
}

// ListRevisions returns every revision for (targetType, targetID), most
// recent edit first. An unrecognized/empty targetType or a target with no
// edits both just come back as an empty slice — list-revisions has no
// declared ServiceError arm, so "nothing to show" is the normal case here,
// not an error (see csilservices' package doc comment).
func (s *Store) ListRevisions(ctx context.Context, db *gorm.DB, targetType, targetID string) ([]Revision, error) {
	var revisions []Revision
	err := db.WithContext(ctx).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("created_at DESC").
		Find(&revisions).Error
	return revisions, err
}
