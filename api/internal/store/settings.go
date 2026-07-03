package store

import (
	"context"
	"errors"

	"gorm.io/gorm/clause"
)

// ErrSelfMentionGrant is returned by GrantMention when userID and
// grantedUserID are the same: a user always has mention access to their own
// content by construction, so a self-grant is a meaningless row this method
// refuses to create. This is a defense-in-depth check — SettingsService
// (api/internal/csilservices/settings.go) rejects the same case earlier,
// with a proper Validation error naming the field — but keeping the guard
// here too means it holds even for calls that bypass the service layer.
var ErrSelfMentionGrant = errors.New("store: cannot grant mention access to yourself")

// GetOrCreateUserSettings returns userID's settings row, lazily creating one
// with the schema's own defaults (mention_policy 'subscribed',
// notify_on_endorse true — coredb's user_settings DEFAULTs, mirrored here)
// the first time it's requested, so a user who has never touched settings
// still gets a real row back instead of a not-found.
func (s *Store) GetOrCreateUserSettings(ctx context.Context, userID string) (*UserSettings, error) {
	var settings UserSettings
	err := s.DB.WithContext(ctx).First(&settings, "user_id = ?", userID).Error
	if err == nil {
		return &settings, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}

	settings = UserSettings{
		UserID:          userID,
		MentionPolicy:   "subscribed",
		NotifyOnEndorse: true,
	}
	// Two concurrent first-touches could both miss the First above; DoNothing
	// on conflict plus a re-fetch makes this safe either way, so callers of
	// what is conceptually a pure read never see a spurious duplicate-key
	// error.
	if err := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&settings).Error; err != nil {
		return nil, err
	}
	if err := s.DB.WithContext(ctx).First(&settings, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateUserSettings applies a partial update (nil fields left unchanged) to
// userID's settings, creating the row first via GetOrCreateUserSettings if
// it doesn't exist yet. Caller (SettingsService) is responsible for
// validating mentionPolicy against the mention_policy enum before calling.
func (s *Store) UpdateUserSettings(ctx context.Context, userID string, mentionPolicy *string, notifyOnEndorse *bool) (*UserSettings, error) {
	if _, err := s.GetOrCreateUserSettings(ctx, userID); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if mentionPolicy != nil {
		updates["mention_policy"] = *mentionPolicy
	}
	if notifyOnEndorse != nil {
		updates["notify_on_endorse"] = *notifyOnEndorse
	}
	if len(updates) > 0 {
		// updated_at is an AutoUpdateTime field (schema.go's convention for
		// a field literally named UpdatedAt) - gorm injects it into the SET
		// clause automatically even for a map-based Updates call, so it
		// isn't listed here.
		if err := s.DB.WithContext(ctx).Model(&UserSettings{}).Where("user_id = ?", userID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	var settings UserSettings
	if err := s.DB.WithContext(ctx).First(&settings, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

// ListMentionGrants returns every grant userID has extended — the users who
// may always @mention-notify userID regardless of subscribed-context
// overlap — newest first.
func (s *Store) ListMentionGrants(ctx context.Context, userID string) ([]MentionGrant, error) {
	var grants []MentionGrant
	err := s.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&grants).Error
	return grants, err
}

// GrantMention idempotently extends userID's mention-notify grant to
// grantedUserID: granting the same target twice is a no-op, not an error
// (see doc.go's error contract — this isn't a Conflict). Returns
// ErrSelfMentionGrant if userID == grantedUserID.
func (s *Store) GrantMention(ctx context.Context, userID, grantedUserID string) error {
	if userID == grantedUserID {
		return ErrSelfMentionGrant
	}
	grant := MentionGrant{UserID: userID, GrantedUserID: grantedUserID}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error
}

// RevokeMention idempotently removes a mention-notify grant; revoking one
// that doesn't exist (or never existed) is a no-op, not an error.
func (s *Store) RevokeMention(ctx context.Context, userID, grantedUserID string) error {
	return s.DB.WithContext(ctx).
		Where("user_id = ? AND granted_user_id = ?", userID, grantedUserID).
		Delete(&MentionGrant{}).Error
}
