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

// newSocialTestStore mirrors newSettingsTestStore (settings_integration_test.go)
// exactly, duplicated rather than shared for the same reason: this task
// only owns the files it's asked to touch.
func newSocialTestStore(t *testing.T) *store.Store {
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

func newSocialTestUser(t *testing.T, st *store.Store, handle string) *store.User {
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

// TestFriendGroupCRUDAndPrivacy covers PLANDOC.md §7 B9's group CRUD,
// per-owner unique name (-> Conflict at the service layer, ErrFriendGroupNameTaken
// here), self-add rejection, member idempotency, and — the acceptance
// criterion this task calls out explicitly — that a group is invisible and
// unmodifiable to anyone but its owner.
func TestFriendGroupCRUDAndPrivacy(t *testing.T) {
	st := newSocialTestStore(t)
	ctx := context.Background()
	alice := newSocialTestUser(t, st, "alice")
	bob := newSocialTestUser(t, st, "bob")
	carol := newSocialTestUser(t, st, "carol")

	group, err := st.CreateFriendGroup(ctx, alice.ID, "inner circle")
	require.NoError(t, err)
	require.Equal(t, "inner circle", group.Name)

	_, err = st.CreateFriendGroup(ctx, alice.ID, "inner circle")
	require.ErrorIs(t, err, store.ErrFriendGroupNameTaken, "per-owner name uniqueness")

	// Same name, different owner: names are unique per-owner, not globally.
	_, err = st.CreateFriendGroup(ctx, bob.ID, "inner circle")
	require.NoError(t, err)

	err = st.AddFriendGroupMember(ctx, alice.ID, group.ID, alice.ID)
	require.ErrorIs(t, err, store.ErrSelfFriend)

	require.NoError(t, st.AddFriendGroupMember(ctx, alice.ID, group.ID, bob.ID))
	require.NoError(t, st.AddFriendGroupMember(ctx, alice.ID, group.ID, bob.ID), "adding the same member twice must be a no-op")

	groups, err := st.ListFriendGroupsByOwner(ctx, alice.ID)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.ElementsMatch(t, []string{bob.ID}, groups[0].MemberUserIDs)

	// Privacy: bob is not alice's group's owner, so every op bob attempts
	// against it reads as not-found — never a distinct "forbidden" that
	// would confirm the group exists.
	_, err = st.GetOwnedFriendGroup(ctx, bob.ID, group.ID)
	require.True(t, store.IsNotFound(err))

	err = st.AddFriendGroupMember(ctx, bob.ID, group.ID, carol.ID)
	require.True(t, store.IsNotFound(err))

	err = st.RemoveFriendGroupMember(ctx, bob.ID, group.ID, bob.ID)
	require.True(t, store.IsNotFound(err))

	err = st.DeleteFriendGroup(ctx, bob.ID, group.ID)
	require.True(t, store.IsNotFound(err))

	// bob's own listing contains only his own "inner circle", never alice's.
	bobGroups, err := st.ListFriendGroupsByOwner(ctx, bob.ID)
	require.NoError(t, err)
	require.Len(t, bobGroups, 1)
	require.Equal(t, "inner circle", bobGroups[0].Name)
	require.NotEqual(t, group.ID, bobGroups[0].ID)

	require.NoError(t, st.RemoveFriendGroupMember(ctx, alice.ID, group.ID, bob.ID))
	require.NoError(t, st.RemoveFriendGroupMember(ctx, alice.ID, group.ID, bob.ID), "removing an absent member must be a no-op")

	groups, err = st.ListFriendGroupsByOwner(ctx, alice.ID)
	require.NoError(t, err)
	require.Empty(t, groups[0].MemberUserIDs)
}

// TestFriendGroupCascadeDelete covers PLANDOC.md §7 B9's "cascade delete"
// acceptance criterion: deleting a group removes its membership rows too
// (coredb's ON DELETE CASCADE FK), and the group itself is then gone.
func TestFriendGroupCascadeDelete(t *testing.T) {
	st := newSocialTestStore(t)
	ctx := context.Background()
	alice := newSocialTestUser(t, st, "alice")
	bob := newSocialTestUser(t, st, "bob")

	group, err := st.CreateFriendGroup(ctx, alice.ID, "circle")
	require.NoError(t, err)
	require.NoError(t, st.AddFriendGroupMember(ctx, alice.ID, group.ID, bob.ID))

	var memberCountBefore int64
	require.NoError(t, st.DB.Model(&store.FriendGroupMember{}).Where("group_id = ?", group.ID).Count(&memberCountBefore).Error)
	require.Equal(t, int64(1), memberCountBefore)

	require.NoError(t, st.DeleteFriendGroup(ctx, alice.ID, group.ID))

	var memberCountAfter int64
	require.NoError(t, st.DB.Model(&store.FriendGroupMember{}).Where("group_id = ?", group.ID).Count(&memberCountAfter).Error)
	require.Zero(t, memberCountAfter, "membership rows should cascade-delete with the group")

	err = st.DeleteFriendGroup(ctx, alice.ID, group.ID)
	require.True(t, store.IsNotFound(err), "the group is gone; a second delete is not-found, not a silent success")
}
