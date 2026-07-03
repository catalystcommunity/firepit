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

// TestUpsertUserAndSessions exercises task B2's store additions
// (UpsertUser/CreateSession/DeleteSession) against a real Postgres: the
// identity-key upsert semantics, the handle-collision retry strategy
// (store.UpsertUser's doc comment), and the session mint/hash/delete
// lifecycle session.go's HashSessionToken and LookupSession depend on.
func TestUpsertUserAndSessions(t *testing.T) {
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
	st := store.New(gdb)

	t.Run("first login inserts a new user with the requested handle", func(t *testing.T) {
		user, err := st.UpsertUser(ctx, "example.com", "user-1", "ada", "Ada Lovelace")
		require.NoError(t, err)
		require.NotEmpty(t, user.ID)
		require.Equal(t, "ada", user.Handle)
		require.Equal(t, "Ada Lovelace", user.DisplayName)
		require.Equal(t, "human", user.Kind)
	})

	t.Run("second login for the same identity returns the existing row unchanged", func(t *testing.T) {
		first, err := st.UpsertUser(ctx, "example.com", "user-2", "grace", "Grace Hopper")
		require.NoError(t, err)

		// A different candidate handle / display name on a later login must
		// NOT overwrite the sticky existing row.
		second, err := st.UpsertUser(ctx, "example.com", "user-2", "totally-different-handle", "New Name")
		require.NoError(t, err)

		require.Equal(t, first.ID, second.ID)
		require.Equal(t, "grace", second.Handle)
		require.Equal(t, "Grace Hopper", second.DisplayName)
	})

	t.Run("a handle collision between two different identities is resolved with a numeric suffix", func(t *testing.T) {
		first, err := st.UpsertUser(ctx, "example.com", "user-3", "same-handle", "First")
		require.NoError(t, err)
		require.Equal(t, "same-handle", first.Handle)

		second, err := st.UpsertUser(ctx, "example.com", "user-4", "same-handle", "Second")
		require.NoError(t, err)
		require.NotEqual(t, first.ID, second.ID)
		require.Equal(t, "same-handle-2", second.Handle)

		third, err := st.UpsertUser(ctx, "example.com", "user-5", "same-handle", "Third")
		require.NoError(t, err)
		require.Equal(t, "same-handle-3", third.Handle)
	})

	t.Run("different linkkeys domains with the same user id are different identities", func(t *testing.T) {
		a, err := st.UpsertUser(ctx, "domain-a.example", "shared-id", "shared-id-handle-a", "A")
		require.NoError(t, err)
		b, err := st.UpsertUser(ctx, "domain-b.example", "shared-id", "shared-id-handle-b", "B")
		require.NoError(t, err)
		require.NotEqual(t, a.ID, b.ID)
	})

	t.Run("session mint, lookup, and delete round-trip", func(t *testing.T) {
		user, err := st.UpsertUser(ctx, "example.com", "session-user", "sessionuser", "Session User")
		require.NoError(t, err)

		sess, rawToken, err := st.CreateSession(ctx, user.ID, time.Hour)
		require.NoError(t, err)
		require.NotEmpty(t, rawToken)
		require.Equal(t, store.HashSessionToken(rawToken), sess.TokenHash)

		got, err := st.LookupSession(ctx, store.HashSessionToken(rawToken))
		require.NoError(t, err)
		require.Equal(t, sess.ID, got.ID)
		require.Equal(t, user.ID, got.UserID)

		require.NoError(t, st.DeleteSession(ctx, sess.TokenHash))

		_, err = st.LookupSession(ctx, store.HashSessionToken(rawToken))
		require.True(t, store.IsNotFound(err), "expected session to be gone after delete")

		// Deleting an already-deleted (or never-existing) session is not an
		// error — Logout's contract never errors even when already logged out.
		require.NoError(t, st.DeleteSession(ctx, sess.TokenHash))
	})

	t.Run("an expired session is treated as not-found by LookupSession", func(t *testing.T) {
		user, err := st.UpsertUser(ctx, "example.com", "expired-user", "expireduser", "Expired User")
		require.NoError(t, err)

		sess, _, err := st.CreateSession(ctx, user.ID, -time.Minute) // already expired
		require.NoError(t, err)

		_, err = st.LookupSession(ctx, sess.TokenHash)
		require.True(t, store.IsNotFound(err))
	})
}
