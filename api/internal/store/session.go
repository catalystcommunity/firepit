package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

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

// HashSessionToken derives the value stored in sessions.token_hash from the
// raw session token a caller presents (session cookie value, bearer token,
// etc). The raw token itself is never persisted — only this hash — so a
// leaked database row can't be replayed as a session. Task B2 (session
// minting) and the session middleware (api/internal/server) both call this
// so the two sides always agree on one hashing scheme.
func HashSessionToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// LookupSession returns the still-valid session matching tokenHash (see
// HashSessionToken), or gorm.ErrRecordNotFound if there is none — including
// when a session row exists but has expired, which intentionally reads as
// not-found rather than a distinct "expired" error: callers (the session
// middleware) treat both identically as "anonymous," never as a failure.
func (s *Store) LookupSession(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	err := s.DB.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", tokenHash, time.Now()).
		First(&sess).Error
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// ErrSessionNotFound is a convenience alias for the not-found sentinel
// LookupSession returns, so callers outside this package don't need to
// import gorm just to compare errors.
var ErrSessionNotFound = gorm.ErrRecordNotFound

// IsNotFound reports whether err is the "no matching row" sentinel gorm
// queries in this package return (record not found, including an
// expired/absent session or a missing user).
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
