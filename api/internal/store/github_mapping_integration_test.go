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

// TestGithubMappingAndTrustedDomainStore exercises every store method task
// B8 owns (api/internal/store/github_mapping.go): github_mappings CRUD +
// repo lookup, and trusted_domains CRUD — against a real Postgres, same
// testcontainers pattern as store_integration_test.go.
func TestGithubMappingAndTrustedDomainStore(t *testing.T) {
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

	admin := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "admin-1", Handle: "admin", Kind: "human", Roles: store.StringArray{"admin"}}
	require.NoError(t, gdb.Create(admin).Error)

	board := &store.Board{Slug: "firepit-dev", Title: "Firepit Dev", Kind: "discussion", CreatedBy: admin.ID}
	require.NoError(t, gdb.Create(board).Error)

	t.Run("github mapping CRUD", func(t *testing.T) {
		m := &store.GithubMapping{
			BoardID:    board.ID,
			Repo:       "catalystcommunity/firepit",
			Events:     store.StringArray{"issues", "release", "pull_request"},
			SecretRef:  "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT",
			ThreadMode: "post_per_issue",
			CreatedBy:  admin.ID,
		}
		require.NoError(t, st.CreateGithubMapping(ctx, m))
		require.NotEmpty(t, m.ID)
		require.NotZero(t, m.CreatedAt)

		byID, err := st.GetGithubMapping(ctx, m.ID)
		require.NoError(t, err)
		require.Equal(t, "catalystcommunity/firepit", byID.Repo)

		byRepo, err := st.GetGithubMappingByRepo(ctx, "catalystcommunity/firepit")
		require.NoError(t, err)
		require.Equal(t, m.ID, byRepo.ID)

		_, err = st.GetGithubMappingByRepo(ctx, "nobody/nothing")
		require.Error(t, err)
		require.True(t, store.IsNotFound(err))

		all, err := st.ListGithubMappings(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)

		require.NoError(t, st.DeleteGithubMapping(ctx, m.ID))
		_, err = st.GetGithubMapping(ctx, m.ID)
		require.True(t, store.IsNotFound(err))
	})

	t.Run("trusted domain CRUD", func(t *testing.T) {
		require.NoError(t, st.CreateTrustedDomain(ctx, &store.TrustedDomain{Domain: "trusted.example.com", AddedBy: admin.ID}))

		got, err := st.GetTrustedDomain(ctx, "trusted.example.com")
		require.NoError(t, err)
		require.Equal(t, admin.ID, got.AddedBy)

		_, err = st.GetTrustedDomain(ctx, "untrusted.example.com")
		require.True(t, store.IsNotFound(err))

		all, err := st.ListTrustedDomains(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)

		require.NoError(t, st.DeleteTrustedDomain(ctx, "trusted.example.com"))
		_, err = st.GetTrustedDomain(ctx, "trusted.example.com")
		require.True(t, store.IsNotFound(err))

		// Deleting something already absent is a no-op, not an error.
		require.NoError(t, st.DeleteTrustedDomain(ctx, "trusted.example.com"))
	})
}
