package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

// DefaultSessionTTL is how long a freshly minted session is valid for absent
// an explicit ttl argument to CreateSession. firepit has no "remember me" /
// short-session distinction in v1 — one TTL for every login.
const DefaultSessionTTL = 30 * 24 * time.Hour

// rawSessionTokenBytes is the amount of crypto/rand entropy behind a raw
// session token (256 bits) — comfortably unguessable; only its SHA-256 hash
// (HashSessionToken) is ever persisted.
const rawSessionTokenBytes = 32

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

// CreateSession mints a brand new session for userID: a random raw token
// (never persisted) plus its sha256 hash (persisted, see HashSessionToken),
// expiring ttl from now — pass exactly 0 to use DefaultSessionTTL. A
// negative ttl is honored as-is (mints an already-expired session); tests
// use that to exercise LookupSession's expiry handling without waiting.
// Returns the inserted row AND the raw token; the row's own TokenHash field
// is the only place the raw token can no longer be recovered from, so the
// caller (GET /auth/callback) must capture the returned raw token
// immediately to place in the session cookie.
func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (sess *Session, rawToken string, err error) {
	if ttl == 0 {
		ttl = DefaultSessionTTL
	}
	rawToken, err = generateRawSessionToken()
	if err != nil {
		return nil, "", err
	}
	row := &Session{
		UserID:    userID,
		TokenHash: HashSessionToken(rawToken),
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	if err := s.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, "", err
	}
	return row, rawToken, nil
}

// DeleteSession removes the session row matching tokenHash (see
// HashSessionToken) — AuthService.Logout's job. Deleting a hash that matches
// no row (already logged out, already expired and reaped, or never existed)
// is not an error: gorm's Delete doesn't fail on "0 rows affected", and
// Logout's CSIL contract explicitly never errors even when already logged
// out (csilservices/auth.go).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.DB.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&Session{}).Error
}

// generateRawSessionToken returns a fresh crypto/rand-backed session token,
// base64url-encoded (cookie-safe: no padding, no reserved characters).
func generateRawSessionToken() (string, error) {
	buf := make([]byte, rawSessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
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
