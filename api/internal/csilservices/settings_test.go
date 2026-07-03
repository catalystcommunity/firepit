package csilservices

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// These are pure validation/authz-gate unit tests: every case here returns
// before the settingsService touches s.store, so a nil *store.Store is
// safe (same pattern api/internal/server/dispatch_test.go's stubServices
// uses for the stub constructors). DB-backed behavior (lazy defaults,
// policy round-trip, grant idempotency) is covered by the testcontainers
// integration suite in api/internal/store/settings_integration_test.go.

func newSettingsServiceForTest() *settingsService {
	return &settingsService{store: nil}
}

func TestUpdateSettingsRejectsUnknownMentionPolicy(t *testing.T) {
	svc := newSettingsServiceForTest()
	ctx := reqctx.WithUser(context.Background(), &store.User{ID: "user-1"})

	bogus := csil.MentionPolicy("bogus")
	_, err := svc.UpdateSettings(ctx, csil.UpdateSettingsRequest{MentionPolicy: &bogus})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeValidation, appErr.Code)
	require.Equal(t, "mention_policy", appErr.Field)
}

func TestUpdateSettingsAcceptsEveryDeclaredMentionPolicy(t *testing.T) {
	for policy := range validMentionPolicies {
		t.Run(string(policy), func(t *testing.T) {
			require.True(t, validMentionPolicies[policy])
		})
	}
	require.Len(t, validMentionPolicies, 4, "subscribed/everyone/authorized/nobody per PLANDOC.md §4")
	require.False(t, validMentionPolicies[csil.MentionPolicy("not-a-policy")])
}

func TestUpdateSettingsRequiresSession(t *testing.T) {
	svc := newSettingsServiceForTest()
	notify := true

	_, err := svc.UpdateSettings(context.Background(), csil.UpdateSettingsRequest{NotifyOnEndorse: &notify})

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestGetSettingsRequiresSession(t *testing.T) {
	svc := newSettingsServiceForTest()

	_, err := svc.GetSettings(context.Background(), csil.Empty{})

	require.Error(t, err)
	var appErr *AppError
	require.NotErrorAs(t, err, &appErr, "get-settings has no ServiceError arm; an unauthenticated call must not look like a typed AppError, it's an opaque transport failure per doc.go")
}

func TestListMentionGrantsRequiresSession(t *testing.T) {
	svc := newSettingsServiceForTest()

	_, err := svc.ListMentionGrants(context.Background(), csil.Empty{})

	require.Error(t, err)
}

func TestGrantMentionRejectsSelfGrant(t *testing.T) {
	svc := newSettingsServiceForTest()
	ctx := reqctx.WithUser(context.Background(), &store.User{ID: "user-1"})

	_, err := svc.GrantMention(ctx, csil.UserID("user-1"))

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeValidation, appErr.Code)
	require.Equal(t, "user_id", appErr.Field)
}

func TestGrantMentionRequiresSession(t *testing.T) {
	svc := newSettingsServiceForTest()

	_, err := svc.GrantMention(context.Background(), csil.UserID("someone-else"))

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestRevokeMentionRequiresSession(t *testing.T) {
	svc := newSettingsServiceForTest()

	_, err := svc.RevokeMention(context.Background(), csil.UserID("someone-else"))

	var appErr *AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, CodeUnauthenticated, appErr.Code)
}

func TestToCSILUserSettings(t *testing.T) {
	settings := &store.UserSettings{
		UserID:          "user-1",
		MentionPolicy:   "everyone",
		NotifyOnEndorse: false,
	}

	got := toCSILUserSettings(settings)

	require.Equal(t, csil.MentionPolicy("everyone"), got.MentionPolicy)
	require.False(t, got.NotifyOnEndorse)
	require.NotNil(t, got.UpdatedAt)
}
