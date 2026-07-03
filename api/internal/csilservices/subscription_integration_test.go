//go:build integration

package csilservices_test

// NOTE ON COVERAGE: ./tools.sh test-integration only runs
// api/internal/store/... and api/internal/server/... under -tags=integration
// (see tools.sh's cmd_test_integration) — this file's tests are exercised by
// running `go test -tags=integration ./internal/csilservices/...` directly.
// The store-level tests (api/internal/store/subscription_test.go,
// read_test.go) are what ./tools.sh test-integration actually gates on, and
// carry the bulk of the behavioral coverage (idempotency, mute carve-outs,
// override-beats-watermark, summary correctness); the tests here focus on
// what only exists at this layer: current-user resolution and AppError
// translation (NotFound/Validation/Unauthenticated).

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// b6TestEnv boots real Subscription/Read services (task B6) against a
// testcontainers Postgres. Named distinctly from setupBoardTestEnv (B3)
// since both live in the same csilservices_test package.
type b6TestEnv struct {
	subs  csil.SubscriptionService
	reads csil.ReadService
	store *store.Store
}

func setupB6Env(t *testing.T) *b6TestEnv {
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
	st := store.New(gdb)

	return &b6TestEnv{
		subs:  csilservices.NewSubscriptionService(st),
		reads: csilservices.NewReadService(st),
		store: st,
	}
}

func (env *b6TestEnv) createUser(t *testing.T, handle string) *store.User {
	t.Helper()
	u := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: handle, Handle: handle, Kind: "human"}
	require.NoError(t, env.store.DB.Create(u).Error)
	return u
}

func (env *b6TestEnv) createBoard(t *testing.T, slug string, createdBy string) *store.Board {
	t.Helper()
	b := &store.Board{Slug: slug, Title: slug, Kind: "discussion", CreatedBy: createdBy}
	require.NoError(t, env.store.DB.Create(b).Error)
	return b
}

func (env *b6TestEnv) createPost(t *testing.T, boardID, authorID string) *store.Post {
	t.Helper()
	p := &store.Post{BoardID: boardID, AuthorID: authorID, Title: "post", LastActivityAt: time.Now().UTC()}
	require.NoError(t, env.store.DB.Create(p).Error)
	return p
}

func TestSubscriptionServiceSubscribe(t *testing.T) {
	env := setupB6Env(t)
	u := env.createUser(t, "alice")
	board := env.createBoard(t, "general", u.ID)

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		_, err := env.subs.Subscribe(anonymous(), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
	})

	t.Run("invalid target_type is a validation error", func(t *testing.T) {
		_, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "bogus", TargetId: board.ID})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeValidation, appErrorCode(t, err))
	})

	t.Run("nonexistent target is not found", func(t *testing.T) {
		_, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: nilUUID})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})

	t.Run("subscribing is idempotent", func(t *testing.T) {
		first, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)
		require.False(t, first.Muted)

		second, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)
		require.Equal(t, first.Id, second.Id)
	})
}

func TestSubscriptionServiceUnsubscribe(t *testing.T) {
	env := setupB6Env(t)
	u := env.createUser(t, "bob")
	board := env.createBoard(t, "general", u.ID)

	t.Run("unsubscribing when never subscribed succeeds", func(t *testing.T) {
		_, err := env.subs.Unsubscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)
	})

	t.Run("unsubscribe removes an existing subscription and is idempotent", func(t *testing.T) {
		_, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)

		_, err = env.subs.Unsubscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)
		_, err = env.subs.Unsubscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.NoError(t, err)

		list, err := env.subs.ListSubscriptions(asUser(u), csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, list.Subscriptions)
	})

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		_, err := env.subs.Unsubscribe(anonymous(), csil.TargetRef{TargetType: "board", TargetId: board.ID})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
	})
}

func TestSubscriptionServiceSetMuted(t *testing.T) {
	env := setupB6Env(t)
	u := env.createUser(t, "carol")
	board := env.createBoard(t, "general", u.ID)
	post := env.createPost(t, board.ID, u.ID)

	t.Run("mutes a post the caller never explicitly subscribed to", func(t *testing.T) {
		sub, err := env.subs.SetMuted(asUser(u), csil.SetMutedRequest{TargetType: "post", TargetId: post.ID, Muted: true})
		require.NoError(t, err)
		require.True(t, sub.Muted)
		require.Equal(t, csil.TargetType("post"), sub.TargetType)
	})

	t.Run("nonexistent target is not found", func(t *testing.T) {
		_, err := env.subs.SetMuted(asUser(u), csil.SetMutedRequest{TargetType: "post", TargetId: nilUUID, Muted: true})
		require.Error(t, err)
		require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
	})
}

func TestSubscriptionServiceListSubscriptions(t *testing.T) {
	env := setupB6Env(t)
	u := env.createUser(t, "dave")
	other := env.createUser(t, "erin")
	board := env.createBoard(t, "general", u.ID)

	t.Run("anonymous sees an empty list, not an error", func(t *testing.T) {
		list, err := env.subs.ListSubscriptions(anonymous(), csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, list.Subscriptions)
	})

	_, err := env.subs.Subscribe(asUser(u), csil.TargetRef{TargetType: "board", TargetId: board.ID})
	require.NoError(t, err)
	_, err = env.subs.Subscribe(asUser(other), csil.TargetRef{TargetType: "board", TargetId: board.ID})
	require.NoError(t, err)

	t.Run("only the caller's own subscriptions come back", func(t *testing.T) {
		list, err := env.subs.ListSubscriptions(asUser(u), csil.Empty{})
		require.NoError(t, err)
		require.Len(t, list.Subscriptions, 1)
		require.Equal(t, csil.TargetType("board"), list.Subscriptions[0].TargetType)
	})
}
