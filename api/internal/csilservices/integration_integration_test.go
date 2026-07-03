//go:build integration

package csilservices_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestIntegrationServiceAuthz drives csilservices.IntegrationService (task
// B8) directly against a real Postgres, asserting the instance-admin gate
// (users.roles contains "admin") on every mutating op, plus the
// create/list/delete-github-mapping and add/remove/list-trusted-domain
// behaviors themselves.
//
// Run via `go test -tags=integration ./internal/csilservices/...` — this is
// NOT yet wired into tools.sh's test-integration verb (which today only
// covers internal/store and internal/server); that gap is being closed
// centrally, tracked outside this task.
func TestIntegrationServiceAuthz(t *testing.T) {
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
	member := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "member-1", Handle: "member", Kind: "human", Roles: store.StringArray{"member"}}
	require.NoError(t, gdb.Create(member).Error)

	board := &store.Board{Slug: "firepit-dev", Title: "Firepit Dev", Kind: "discussion", CreatedBy: admin.ID}
	require.NoError(t, gdb.Create(board).Error)

	svc := csilservices.NewIntegrationService(st)

	adminCtx := reqctx.WithUser(context.Background(), admin)
	memberCtx := reqctx.WithUser(context.Background(), member)
	anonCtx := context.Background()

	createReq := csil.CreateMappingRequest{
		BoardId:    csil.BoardID(board.ID),
		Repo:       "catalystcommunity/firepit",
		Events:     []string{"issues", "release", "pull_request"},
		SecretRef:  "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT",
		ThreadMode: "post_per_issue",
	}

	t.Run("create-github-mapping rejects anonymous", func(t *testing.T) {
		_, err := svc.CreateGithubMapping(anonCtx, createReq)
		requireAppErrorCode(t, err, csilservices.CodeUnauthenticated)
	})

	t.Run("create-github-mapping rejects non-admin", func(t *testing.T) {
		_, err := svc.CreateGithubMapping(memberCtx, createReq)
		requireAppErrorCode(t, err, csilservices.CodeForbidden)
	})

	var created csil.GithubMapping
	t.Run("create-github-mapping succeeds for admin", func(t *testing.T) {
		var err error
		created, err = svc.CreateGithubMapping(adminCtx, createReq)
		require.NoError(t, err)
		require.NotEmpty(t, created.Id)
		require.Equal(t, "catalystcommunity/firepit", created.Repo)
		require.Equal(t, csil.BoardID(board.ID), created.BoardId)
		require.ElementsMatch(t, []string{"issues", "release", "pull_request"}, created.Events)
		require.Equal(t, csil.UserID(admin.ID), created.CreatedBy)
	})

	t.Run("create-github-mapping rejects a duplicate repo", func(t *testing.T) {
		_, err := svc.CreateGithubMapping(adminCtx, createReq)
		requireAppErrorCode(t, err, csilservices.CodeConflict)
	})

	t.Run("create-github-mapping validates required fields", func(t *testing.T) {
		bad := createReq
		bad.Repo = "not-owner-slash-name"
		_, err := svc.CreateGithubMapping(adminCtx, bad)
		requireAppErrorCode(t, err, csilservices.CodeValidation)
	})

	t.Run("list-github-mappings: anonymous gets an empty list, not an error", func(t *testing.T) {
		list, err := svc.ListGithubMappings(anonCtx, csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, list.Mappings)
	})

	t.Run("list-github-mappings: admin sees the mapping", func(t *testing.T) {
		list, err := svc.ListGithubMappings(adminCtx, csil.Empty{})
		require.NoError(t, err)
		require.Len(t, list.Mappings, 1)
		require.Equal(t, created.Id, list.Mappings[0].Id)
	})

	t.Run("delete-github-mapping rejects non-admin", func(t *testing.T) {
		_, err := svc.DeleteGithubMapping(memberCtx, created.Id)
		requireAppErrorCode(t, err, csilservices.CodeForbidden)
	})

	t.Run("delete-github-mapping 404s on an unknown id", func(t *testing.T) {
		_, err := svc.DeleteGithubMapping(adminCtx, csil.MappingID("nonexistent"))
		requireAppErrorCode(t, err, csilservices.CodeNotFound)
	})

	t.Run("delete-github-mapping succeeds for admin", func(t *testing.T) {
		_, err := svc.DeleteGithubMapping(adminCtx, created.Id)
		require.NoError(t, err)

		list, err := svc.ListGithubMappings(adminCtx, csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, list.Mappings)
	})

	t.Run("trusted domains: authz + idempotency", func(t *testing.T) {
		_, err := svc.AddTrustedDomain(memberCtx, csil.Domain("trusted.example.com"))
		requireAppErrorCode(t, err, csilservices.CodeForbidden)

		_, err = svc.AddTrustedDomain(adminCtx, csil.Domain("Trusted.Example.com"))
		require.NoError(t, err)

		// Adding it again (any case) is a no-op, not a conflict.
		_, err = svc.AddTrustedDomain(adminCtx, csil.Domain("trusted.example.com"))
		require.NoError(t, err)

		listAnon, err := svc.ListTrustedDomains(anonCtx, csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, listAnon.Domains)

		listAdmin, err := svc.ListTrustedDomains(adminCtx, csil.Empty{})
		require.NoError(t, err)
		require.Len(t, listAdmin.Domains, 1)
		require.Equal(t, "trusted.example.com", listAdmin.Domains[0].Domain)

		_, err = svc.RemoveTrustedDomain(memberCtx, csil.Domain("trusted.example.com"))
		requireAppErrorCode(t, err, csilservices.CodeForbidden)

		_, err = svc.RemoveTrustedDomain(adminCtx, csil.Domain("trusted.example.com"))
		require.NoError(t, err)

		// Removing an already-absent domain is a no-op, not an error.
		_, err = svc.RemoveTrustedDomain(adminCtx, csil.Domain("trusted.example.com"))
		require.NoError(t, err)

		listAfter, err := svc.ListTrustedDomains(adminCtx, csil.Empty{})
		require.NoError(t, err)
		require.Empty(t, listAfter.Domains)
	})
}

func requireAppErrorCode(t *testing.T, err error, code uint64) {
	t.Helper()
	require.Error(t, err)
	var appErr *csilservices.AppError
	require.True(t, errors.As(err, &appErr), "expected an *csilservices.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
