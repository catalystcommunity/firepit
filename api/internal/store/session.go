package store

import "time"

// Session mirrors the `sessions` table: firepit's own session, minted
// after a successful linkkeys RP handshake.
type Session struct {
	ID        string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	UserID    string    `gorm:"column:user_id;type:uuid;not null"`
	TokenHash string    `gorm:"column:token_hash;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (Session) TableName() string { return "sessions" }
