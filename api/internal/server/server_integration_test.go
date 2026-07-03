//go:build integration

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/config"
	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/server"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/api/internal/transport"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestServerEndToEnd boots the real firepit-api HTTP handler (task B1:
// config -> migrations -> store -> stub services -> server.New, exactly what
// api/cmd/firepit-api/main.go wires) against a testcontainers Postgres, then
// drives it purely over HTTP in CBOR:
//
//   - GET /healthz reports 200 once the database is reachable.
//   - POST /csil/v1/rpc for a stubbed op (AuthService.begin-login) round-trips
//     the declared ServiceError arm with the Unimplemented code, proving the
//     dispatcher, the session middleware, and the stub wiring all work
//     end-to-end over the wire — not just via direct Go calls.
//
// This uses api/internal/csil's codec + api/internal/transport's envelope
// helpers directly (one of the two options task B1 allows) rather than the
// generated clients/go client: that package lives in its own Go module with
// its own (service, PascalCase-method) Transport convention that a real
// caller's carrier must kebab-case before it hits the wire (see dispatch.go's
// doc comment) — exercising that translation isn't this test's job, and
// pulling in a second module here would only add a replace-directive dance
// for no extra coverage. The wire bytes this test sends are byte-identical
// to what a correctly-written clients/go carrier would produce.
func TestServerEndToEnd(t *testing.T) {
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

	cfg := config.Config{DBURI: dsn, CORSOrigins: []string{"*"}}
	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st),
		Endorsement:  csilservices.NewEndorsementService(st, notify.Noop{}),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}
	srv := server.New(cfg, st, svcs)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Equal(t, "ok", body["status"])
	})

	t.Run("stubbed op round-trips ServiceError over HTTP", func(t *testing.T) {
		payload := csil.EncodeAuthBeginLoginRequest(csil.BeginLoginRequest{Domain: "example.com"})
		envelope, err := transport.NewRpcRequest("auth", "begin-login", payload).Encode()
		require.NoError(t, err)

		httpResp, err := http.Post(ts.URL+"/csil/v1/rpc", "application/cbor", bytes.NewReader(envelope))
		require.NoError(t, err)
		defer httpResp.Body.Close()
		// Per csil-rpc-transport.md/serving-csil-rpc-http-and-tcp.md: HTTP
		// status is 200 whenever an envelope comes back, even one carrying a
		// non-zero transport status or a declared application error.
		require.Equal(t, http.StatusOK, httpResp.StatusCode)
		require.Equal(t, "application/cbor", httpResp.Header.Get("Content-Type"))

		body, err := io.ReadAll(httpResp.Body)
		require.NoError(t, err)

		rpcResp, err := transport.DecodeRpcResponse(body)
		require.NoError(t, err)
		require.NoError(t, rpcResp.AsTransportError(), "expected status 0 (ok) — a typed reply, not a transport failure")
		require.NotNil(t, rpcResp.Variant)
		require.Equal(t, "ServiceError", *rpcResp.Variant)

		svcErr, err := csil.DecodeServiceError(rpcResp.Payload)
		require.NoError(t, err)
		require.Equal(t, csilservices.CodeUnimplemented, svcErr.Code)
		require.Contains(t, svcErr.Message, "begin-login")
	})
}
