//go:build integration

// Service-layer integration coverage for SocialService.resolve-user (added
// alongside the CSIL schema change that introduced it — see
// social_test.go's unit-level authz/validation coverage for the
// nil-store-safe cases; this file covers the real store lookup).
package csilservices_test

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

// socialTestEnv boots a real SocialService (task B9, plus resolve-user)
// against a testcontainers Postgres — the same pattern every other
// *_integration_test.go in this package uses.
type socialTestEnv struct {
	svc   csil.SocialService
	store *store.Store
}

func setupSocialTestEnv(t *testing.T) *socialTestEnv {
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

	return &socialTestEnv{svc: csilservices.NewSocialService(st), store: st}
}

func (env *socialTestEnv) createUser(t *testing.T, handle, displayName string) *store.User {
	t.Helper()
	u := &store.User{
		LinkkeysDomain: "example.com",
		LinkkeysUserID: handle,
		Handle:         handle,
		DisplayName:    displayName,
		Kind:           "human",
		Roles:          store.StringArray{},
	}
	require.NoError(t, env.store.DB.Create(u).Error)
	return u
}

// TestResolveUser_HappyPath covers an exact handle match returning the
// target's public profile — the lookup grant-mention/add-friend's forms use
// to turn a typed handle into a UserID.
func TestResolveUser_HappyPath(t *testing.T) {
	env := setupSocialTestEnv(t)
	caller := env.createUser(t, "alice", "Alice Anders")
	target := env.createUser(t, "bob", "Bob Baker")

	got, err := env.svc.ResolveUser(asUser(caller), csil.Handle("bob"))
	require.NoError(t, err)
	require.Equal(t, csil.UserID(target.ID), got.Id)
	require.Equal(t, "bob", got.Handle)
	require.Equal(t, "Bob Baker", got.DisplayName)
}

// TestResolveUser_NotFound covers a handle with no matching user.
func TestResolveUser_NotFound(t *testing.T) {
	env := setupSocialTestEnv(t)
	caller := env.createUser(t, "alice", "Alice Anders")

	_, err := env.svc.ResolveUser(asUser(caller), csil.Handle("nobody-by-this-handle"))
	require.Equal(t, csilservices.CodeNotFound, appErrorCode(t, err))
}

// TestResolveUser_RequiresSession covers the anonymous-caller rejection —
// available to any AUTHENTICATED user, not the general public.
func TestResolveUser_RequiresSession(t *testing.T) {
	env := setupSocialTestEnv(t)
	env.createUser(t, "bob", "Bob Baker")

	_, err := env.svc.ResolveUser(anonymous(), csil.Handle("bob"))
	require.Equal(t, csilservices.CodeUnauthenticated, appErrorCode(t, err))
}
