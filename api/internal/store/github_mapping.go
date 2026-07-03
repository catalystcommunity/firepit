package store

import (
	"time"
)

// GithubMapping mirrors the `github_mappings` table: per-repo webhook
// routing into a board.
type GithubMapping struct {
	ID         string      `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	BoardID    string      `gorm:"column:board_id;type:uuid;not null"`
	Repo       string      `gorm:"column:repo;not null"`
	Events     StringArray `gorm:"column:events;type:text[];not null"`
	SecretRef  string      `gorm:"column:secret_ref;not null"`
	ThreadMode string      `gorm:"column:thread_mode;type:github_thread_mode;not null;default:'post_per_issue'"`
	CreatedBy  string      `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt  time.Time   `gorm:"column:created_at;not null"`
}

func (GithubMapping) TableName() string { return "github_mappings" }
