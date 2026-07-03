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

// newSubscriptionTestStore boots a fresh testcontainers Postgres, runs
// coredb's migrations, and returns a ready *store.Store. Deliberately not
// shared with store_integration_test.go (which several other Wave B tasks
// also touch for their own round-trip coverage) — a private helper here
// avoids adding contention on that file while every B-task's store tests
// land in parallel.
func newSubscriptionTestStore(t *testing.T) *store.Store {
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
	t.Cleanup(func() { require.NoError(t, pgContainer.Terminate(ctx)) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, coredb.Up(dsn))

	gdb, err := store.Open(dsn)
	require.NoError(t, err)
	return store.New(gdb)
}

// seedUserAndBoard creates one user and one board (owned by that user) for
// tests that just need a valid subscription target.
func seedUserAndBoard(t *testing.T, st *store.Store) (userID, boardID string) {
	t.Helper()
	ctx := context.Background()

	user := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "u1", Handle: "alice", Kind: "human"}
	require.NoError(t, st.DB.WithContext(ctx).Create(user).Error)

	board := &store.Board{Slug: "general", Title: "General", Kind: "discussion", CreatedBy: user.ID}
	require.NoError(t, st.DB.WithContext(ctx).Create(board).Error)

	return user.ID, board.ID
}

func TestSubscribeIsIdempotent(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	userID, boardID := seedUserAndBoard(t, st)

	first, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)
	require.False(t, first.Muted)

	second, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "subscribing twice must return the same row, not a duplicate")

	var count int64
	require.NoError(t, st.DB.WithContext(ctx).Model(&store.Subscription{}).
		Where("user_id = ? AND target_type = 'board' AND target_id = ?", userID, boardID).
		Count(&count).Error)
	require.Equal(t, int64(1), count, "exactly one subscription row must exist")
}

func TestSubscribeThenMuteDoesNotDuplicateOrLoseFlag(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	userID, boardID := seedUserAndBoard(t, st)

	sub, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)
	require.False(t, sub.Muted)

	muted, err := st.SetSubscriptionMuted(ctx, userID, "board", boardID, true)
	require.NoError(t, err)
	require.Equal(t, sub.ID, muted.ID)
	require.True(t, muted.Muted)

	// Subscribing again afterwards must not clobber the mute (idempotent
	// subscribe never resets an existing row).
	again, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)
	require.True(t, again.Muted)
}

func TestSetSubscriptionMutedCreatesRowIfMissing(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	userID, boardID := seedUserAndBoard(t, st)

	// Muting a post the caller never explicitly subscribed to is exactly
	// the "carve a hole out of a board subscription" flow (PLANDOC.md §4):
	// set-muted must create the subscription row rather than requiring a
	// prior subscribe call.
	post := &store.Post{BoardID: boardID, AuthorID: userID, Title: "t", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(post).Error)

	sub, err := st.SetSubscriptionMuted(ctx, userID, "post", post.ID, true)
	require.NoError(t, err)
	require.True(t, sub.Muted)
	require.Equal(t, post.ID, sub.TargetID)
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	userID, boardID := seedUserAndBoard(t, st)

	// Unsubscribing when never subscribed must succeed silently.
	require.NoError(t, st.Unsubscribe(ctx, userID, "board", boardID))

	_, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)

	require.NoError(t, st.Unsubscribe(ctx, userID, "board", boardID))
	// Second unsubscribe: still a no-op, not an error.
	require.NoError(t, st.Unsubscribe(ctx, userID, "board", boardID))

	_, err = st.FindSubscription(ctx, userID, "board", boardID)
	require.True(t, store.IsNotFound(err))
}

func TestListSubscriptionsReturnsAllOfCallers(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	userID, boardID := seedUserAndBoard(t, st)

	other := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "u2", Handle: "bob", Kind: "human"}
	require.NoError(t, st.DB.WithContext(ctx).Create(other).Error)

	post := &store.Post{BoardID: boardID, AuthorID: userID, Title: "t", LastActivityAt: time.Now().UTC()}
	require.NoError(t, st.DB.WithContext(ctx).Create(post).Error)

	_, err := st.Subscribe(ctx, userID, "board", boardID)
	require.NoError(t, err)
	_, err = st.Subscribe(ctx, userID, "post", post.ID)
	require.NoError(t, err)
	// A different user's subscription must not leak into userID's list.
	_, err = st.Subscribe(ctx, other.ID, "board", boardID)
	require.NoError(t, err)

	subs, err := st.ListSubscriptions(ctx, userID)
	require.NoError(t, err)
	require.Len(t, subs, 2)
}

func TestTargetExists(t *testing.T) {
	st := newSubscriptionTestStore(t)
	ctx := context.Background()
	_, boardID := seedUserAndBoard(t, st)

	ok, err := st.TargetExists(ctx, "board", boardID)
	require.NoError(t, err)
	require.True(t, ok)

	// A well-formed but never-assigned id (generate_ulid() never produces
	// all-zeros) is a safe stand-in for "a real-looking id that doesn't
	// exist" (see csilservices' board_integration_test.go's nilUUID for the
	// same constant elsewhere).
	ok, err = st.TargetExists(ctx, "board", "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = st.TargetExists(ctx, "bogus-type", boardID)
	require.NoError(t, err)
	require.False(t, ok)

	// A malformed id must report "not found", not surface a raw Postgres
	// "invalid input syntax for type uuid" error.
	ok, err = st.TargetExists(ctx, "board", "not-a-uuid")
	require.NoError(t, err)
	require.False(t, ok)
}
