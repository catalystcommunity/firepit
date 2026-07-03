package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// settingsService is the stub SettingsService implementation (task B1). B9
// replaces every method body below; see the package doc comment (doc.go)
// for the error-handling contract every replacement must follow.
type settingsService struct {
	store *store.Store
}

// NewSettingsService constructs the SettingsService implementation.
func NewSettingsService(st *store.Store) csil.SettingsService {
	return &settingsService{store: st}
}

func (s *settingsService) GetSettings(ctx context.Context, req csil.Empty) (csil.UserSettings, error) {
	return csil.UserSettings{}, Unimplemented("SettingsService.get-settings")
}

func (s *settingsService) UpdateSettings(ctx context.Context, req csil.UpdateSettingsRequest) (csil.UserSettings, error) {
	return csil.UserSettings{}, Unimplemented("SettingsService.update-settings")
}

func (s *settingsService) ListMentionGrants(ctx context.Context, req csil.Empty) (csil.MentionGrantList, error) {
	return csil.MentionGrantList{}, Unimplemented("SettingsService.list-mention-grants")
}

func (s *settingsService) GrantMention(ctx context.Context, req csil.UserID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SettingsService.grant-mention")
}

func (s *settingsService) RevokeMention(ctx context.Context, req csil.UserID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("SettingsService.revoke-mention")
}
