package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// UserReputation mirrors the `user_reputation` table (coredb/migrations/
// 000002_reputation.sql): a cached, incrementally-maintained count of
// trusted-domain endorsements a user's content has received. It exists
// only to order the endorser list per-viewer (PLANDOC.md §4 "Endorser
// ordering") — it never ranks content, and it is not the same thing as a
// board role, which is looked up live from board_members.
//
// Maintained transactionally by csilservices.EndorsementService: bumped up
// on endorse, down on retract, but ONLY when the endorsing user's
// linkkeys_domain is present in trusted_domains at the time of the
// endorse/retract (the endorsed author is the one whose count changes).
type UserReputation struct {
	UserID                  string    `gorm:"column:user_id;type:uuid;primaryKey"`
	TrustedEndorsementCount int       `gorm:"column:trusted_endorsement_count;not null;default:0"`
	UpdatedAt               time.Time `gorm:"column:updated_at;not null"`
}

func (UserReputation) TableName() string { return "user_reputation" }

// BumpReputation adds delta (+1 on a trusted-domain endorse, -1 on
// retracting one) to authorID's cached trusted-endorsement count, within
// db (expected to be an open transaction shared with the endorsement
// write itself), upserting the row if authorID has never had one. The
// count is clamped at zero: retracting an endorsement whose trusted-domain
// status the cache never actually counted (or double-retracting under a
// race) must never drive it negative.
func (s *Store) BumpReputation(ctx context.Context, db *gorm.DB, authorID string, delta int) error {
	now := time.Now().UTC()
	return db.WithContext(ctx).Exec(`
		INSERT INTO user_reputation (user_id, trusted_endorsement_count, updated_at)
		VALUES (?, GREATEST(?, 0), ?)
		ON CONFLICT (user_id) DO UPDATE SET
			trusted_endorsement_count = GREATEST(user_reputation.trusted_endorsement_count + ?, 0),
			updated_at = ?
	`, authorID, delta, now, delta, now).Error
}

// ReputationCount returns userID's cached trusted-endorsement count, or 0
// if no row exists yet (nobody from a trusted domain has ever endorsed
// their content).
func (s *Store) ReputationCount(ctx context.Context, db *gorm.DB, userID string) (int, error) {
	var rep UserReputation
	err := db.WithContext(ctx).First(&rep, "user_id = ?", userID).Error
	if err != nil {
		if IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	return rep.TrustedEndorsementCount, nil
}
