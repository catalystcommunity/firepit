//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// newSettingsTestStore spins a fresh testcontainers Postgres migrated to
// coredb's baseline schema and returns a *store.Store against it. This
// duplicates store_integration_test.go's container boilerplate rather than
// factoring out a shared helper: that file belongs to task A3, and this
// task (B9) only owns the tables/files it's asked to touch.
func newSettingsTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:17",
		tcpostgres.WithDatabase("firepit_test"),
		tcpostgres.WithUsername("firepit"),
		tcpostgres.WithPassword("devpass123"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(ctx))
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, coredb.Up(dsn))

	gdb, err := store.Open(dsn)
	require.NoError(t, err)
	return store.New(gdb)
}

func newSettingsTestUser(t *testing.T, st *store.Store, handle string) *store.User {
	t.Helper()
	u := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: handle,
		Handle:         handle,
		Kind:           "human",
	}
	require.NoError(t, st.DB.Create(u).Error)
	return u
}

// TestUserSettingsLazyDefault covers PLANDOC.md §7 B9's "lazy-default row
// if absent" requirement: a user who has never touched settings gets the
// schema's defaults back, and exactly one row is created for them even
// across repeated calls.
func TestUserSettingsLazyDefault(t *testing.T) {
	st := newSettingsTestStore(t)
	ctx := context.Background()
	user := newSettingsTestUser(t, st, "alice")

	settings, err := st.GetOrCreateUserSettings(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "subscribed", settings.MentionPolicy)
	require.True(t, settings.NotifyOnEndorse)

	again, err := st.GetOrCreateUserSettings(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, settings.UpdatedAt, again.UpdatedAt, "second call should read back the same row, not create another")

	var count int64
	require.NoError(t, st.DB.Model(&store.UserSettings{}).Where("user_id = ?", user.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// TestUserSettingsPolicyRoundTrip covers the update-settings partial-update
// contract: only fields present in the update change, everything else is
// left as-is.
func TestUserSettingsPolicyRoundTrip(t *testing.T) {
	st := newSettingsTestStore(t)
	ctx := context.Background()
	user := newSettingsTestUser(t, st, "bob")

	policy := "everyone"
	notify := false
	updated, err := st.UpdateUserSettings(ctx, user.ID, &policy, &notify)
	require.NoError(t, err)
	require.Equal(t, "everyone", updated.MentionPolicy)
	require.False(t, updated.NotifyOnEndorse)

	reread, err := st.GetOrCreateUserSettings(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "everyone", reread.MentionPolicy)
	require.False(t, reread.NotifyOnEndorse)

	policy2 := "nobody"
	updated2, err := st.UpdateUserSettings(ctx, user.ID, &policy2, nil)
	require.NoError(t, err)
	require.Equal(t, "nobody", updated2.MentionPolicy, "mention_policy should change")
	require.False(t, updated2.NotifyOnEndorse, "notify_on_endorse should be left alone by a nil pointer")
}

// TestMentionGrantIdempotencyAndSelfGrant covers grant/revoke idempotency
// and the self-grant rejection from PLANDOC.md §7 B9.
func TestMentionGrantIdempotencyAndSelfGrant(t *testing.T) {
	st := newSettingsTestStore(t)
	ctx := context.Background()
	user := newSettingsTestUser(t, st, "carol")
	other := newSettingsTestUser(t, st, "dave")

	require.NoError(t, st.GrantMention(ctx, user.ID, other.ID))
	require.NoError(t, st.GrantMention(ctx, user.ID, other.ID), "granting the same target twice must be a no-op, not an error")

	grants, err := st.ListMentionGrants(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, other.ID, grants[0].GrantedUserID)

	require.NoError(t, st.RevokeMention(ctx, user.ID, other.ID))
	require.NoError(t, st.RevokeMention(ctx, user.ID, other.ID), "revoking an absent grant must be a no-op, not an error")

	grants, err = st.ListMentionGrants(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, grants)

	err = st.GrantMention(ctx, user.ID, user.ID)
	require.ErrorIs(t, err, store.ErrSelfMentionGrant)
}
