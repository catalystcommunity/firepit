//go:build integration

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// TestAuthLoginFlow drives task B2's entire linkkeys login flow end-to-end
// over real HTTP against a testcontainers Postgres, with a MOCK linkkeys RP
// (the HTTP/JSON PKI shim's three endpoints — sign-request, decrypt-token,
// verify-assertion — see api/internal/linkkeys/client.go) standing in for
// the real sidecar:
//
//   - happy path: AuthService.begin-login -> GET /auth/callback -> session
//     cookie -> AuthService.whoami round-trip reflects the logged-in user.
//   - bad nonce: an assertion whose nonce doesn't match the one begin-login
//     minted is rejected.
//   - expired assertion: an assertion whose own expires_at is in the past is
//     rejected even though its nonce is fresh.
//
// The mock RP never performs real linkkeys crypto — decrypt-token and
// verify-assertion are keyed by whatever string relationship each subtest
// sets up (see mockRP below) — but the CALLER-side contract they exercise
// (client.go's JSON shapes, csilservices.CompleteLogin's verification
// sequence, session.go's cookie handling) is exactly what the real sidecar
// would be driven through.
func TestAuthLoginFlow(t *testing.T) {
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

	rp := newMockRP()
	rpSrv := httptest.NewServer(rp)
	t.Cleanup(rpSrv.Close)

	const (
		idpDomain = "idp.example"
		rpDomain  = "firepit.example"
	)

	cfg := config.Config{
		DBURI:                dsn,
		CORSOrigins:          []string{"*"},
		LinkkeysDomain:       rpDomain,
		LinkkeysPKIURL:       rpSrv.URL,
		LinkkeysPKIAPIKey:    "test-api-key",
		LinkkeysTransport:    "http",
		LinkkeysIDPDomain:    idpDomain,
		LinkkeysIDPURL:       "https://idp.example",
		AppCallbackURL:       "https://firepit.example/auth/callback",
		SessionNonceSecret:   "test-nonce-secret",
		PostLoginRedirectURL: "/",
	}
	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st, cfg),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st, notify.Noop{}),
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

	// beginLogin drives AuthService.begin-login over the real CSIL-RPC HTTP
	// carrier and returns the nonce the mock RP's sign-request endpoint
	// captured for this call (see mockRP.SignRequest).
	beginLogin := func(t *testing.T) string {
		t.Helper()
		payload := csil.EncodeAuthBeginLoginRequest(csil.BeginLoginRequest{Domain: idpDomain})
		envelope, err := transport.NewRpcRequest("auth", "begin-login", payload).Encode()
		require.NoError(t, err)

		resp, err := http.Post(ts.URL+"/csil/v1/rpc", "application/cbor", bytes.NewReader(envelope))
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		rpcResp, err := transport.DecodeRpcResponse(body)
		require.NoError(t, err)
		require.NoError(t, rpcResp.AsTransportError())
		require.Equal(t, "BeginLoginResponse", *rpcResp.Variant)

		beginResp, err := csil.DecodeAuthBeginLoginResponse(rpcResp.Payload)
		require.NoError(t, err)
		require.Contains(t, beginResp.RedirectUrl, "https://idp.example/auth/authorize?signed_request=")

		return rp.takeLastNonce(t)
	}

	// noRedirectClient never follows a 302 — callback (below) needs to see
	// the redirect response itself (status + Set-Cookie), not whatever page
	// it points at (which doesn't exist in this test server anyway).
	noRedirectClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// callback issues the raw GET /auth/callback request (no automatic
	// cookie jar — the response's Set-Cookie header, if any, is returned
	// directly so each subtest controls exactly how it's reused).
	callback := func(t *testing.T, encryptedToken string) *http.Response {
		t.Helper()
		resp, err := noRedirectClient.Get(ts.URL + "/auth/callback?" + url.Values{"encrypted_token": {encryptedToken}}.Encode())
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	// whoami drives AuthService.whoami, optionally presenting a raw session
	// token as the firepit_session cookie.
	whoami := func(t *testing.T, rawSessionToken string) transport.RpcResponse {
		t.Helper()
		payload := csil.EncodeAuthWhoamiRequest(csil.Empty{})
		envelope, err := transport.NewRpcRequest("auth", "whoami", payload).Encode()
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/csil/v1/rpc", bytes.NewReader(envelope))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/cbor")
		if rawSessionToken != "" {
			req.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: rawSessionToken})
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		rpcResp, err := transport.DecodeRpcResponse(body)
		require.NoError(t, err)
		return rpcResp
	}

	t.Run("happy path: begin-login -> callback -> session cookie -> whoami round trip", func(t *testing.T) {
		nonce := beginLogin(t)

		rp.setAssertion("enc-token-happy", mockAssertion{
			verified:    true,
			userID:      "u-happy",
			domain:      idpDomain,
			audience:    rpDomain,
			nonce:       nonce,
			expiresAt:   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			displayName: "Ada Example",
		})

		resp := callback(t, "enc-token-happy")
		require.Equal(t, http.StatusFound, resp.StatusCode, "expected a redirect on successful login")

		var rawToken string
		for _, c := range resp.Cookies() {
			if c.Name == server.SessionCookieName {
				rawToken = c.Value
			}
		}
		require.NotEmpty(t, rawToken, "expected a firepit_session cookie to be set")

		rpcResp := whoami(t, rawToken)
		require.NoError(t, rpcResp.AsTransportError())
		require.Equal(t, "UserProfile", *rpcResp.Variant)
		profile, err := csil.DecodeAuthWhoamiResponse(rpcResp.Payload)
		require.NoError(t, err)
		require.Equal(t, idpDomain, profile.LinkkeysDomain)
		require.Equal(t, "Ada Example", profile.DisplayName)
		require.Equal(t, "ada-example", profile.Handle)
		require.Equal(t, csil.UserKind("human"), profile.Kind)

		// Anonymous whoami (no cookie) is a distinct ServiceError, not a
		// transport failure.
		anonResp := whoami(t, "")
		require.NoError(t, anonResp.AsTransportError())
		require.Equal(t, "ServiceError", *anonResp.Variant)
		svcErr, err := csil.DecodeServiceError(anonResp.Payload)
		require.NoError(t, err)
		require.Equal(t, csilservices.CodeUnauthenticated, svcErr.Code)
	})

	t.Run("bad nonce is rejected", func(t *testing.T) {
		_ = beginLogin(t) // mints and captures a real nonce, but we deliberately don't use it below

		rp.setAssertion("enc-token-badnonce", mockAssertion{
			verified:    true,
			userID:      "u-badnonce",
			domain:      idpDomain,
			audience:    rpDomain,
			nonce:       "this-nonce-was-never-minted-by-begin-login",
			expiresAt:   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			displayName: "Bad Nonce User",
		})

		resp := callback(t, "enc-token-badnonce")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		for _, c := range resp.Cookies() {
			require.NotEqual(t, server.SessionCookieName, c.Name, "no session should be minted on a rejected login")
		}
	})

	t.Run("expired assertion is rejected", func(t *testing.T) {
		nonce := beginLogin(t)

		rp.setAssertion("enc-token-expired", mockAssertion{
			verified:    true,
			userID:      "u-expired",
			domain:      idpDomain,
			audience:    rpDomain,
			nonce:       nonce,                                                 // fresh, valid nonce ...
			expiresAt:   time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), // ... but the assertion itself already expired
			displayName: "Expired User",
		})

		resp := callback(t, "enc-token-expired")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("unverified assertion is rejected", func(t *testing.T) {
		rp.setAssertion("enc-token-unverified", mockAssertion{verified: false})

		resp := callback(t, "enc-token-unverified")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("missing encrypted_token is a bad request", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/auth/callback")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// --- mock linkkeys RP (HTTP/JSON PKI shim) ---

type mockAssertion struct {
	verified    bool
	userID      string
	domain      string
	audience    string
	nonce       string
	expiresAt   string
	displayName string
}

// mockRP implements the three endpoints api/internal/linkkeys/client.go
// (Client) calls: sign-request, decrypt-token, verify-assertion. It is not
// cryptographically meaningful — decrypt-token maps an encrypted_token to a
// deterministic "signed assertion" string 1:1, and verify-assertion looks
// that string up in a test-populated table — but every JSON shape on the
// wire is exactly what the real sidecar and api/internal/linkkeys/client.go
// agree on.
type mockRP struct {
	mu         sync.Mutex
	lastNonce  string
	assertions map[string]mockAssertion // keyed by encrypted_token
}

func newMockRP() *mockRP {
	return &mockRP{assertions: map[string]mockAssertion{}}
}

// setAssertion registers what verify-assertion should return for the signed
// assertion decrypt-token derives from encryptedToken.
func (m *mockRP) setAssertion(encryptedToken string, a mockAssertion) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assertions[encryptedToken] = a
}

// takeLastNonce returns (and clears) the nonce most recently seen by
// sign-request, failing the test if none has arrived yet.
func (m *mockRP) takeLastNonce(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.NotEmpty(t, m.lastNonce, "sign-request was never called")
	n := m.lastNonce
	m.lastNonce = ""
	return n
}

func (m *mockRP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/sign-request.json"):
		m.handleSignRequest(w, r)
	case strings.HasSuffix(r.URL.Path, "/decrypt-token.json"):
		m.handleDecryptToken(w, r)
	case strings.HasSuffix(r.URL.Path, "/verify-assertion.json"):
		m.handleVerifyAssertion(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *mockRP) handleSignRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CallbackURL string `json:"callback_url"`
		Nonce       string `json:"nonce"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	m.mu.Lock()
	m.lastNonce = in.Nonce
	m.mu.Unlock()
	writeJSON(w, map[string]string{"signed_request": "signed-request-blob"})
}

func (m *mockRP) handleDecryptToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EncryptedToken string `json:"encrypted_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	// 1:1, reversible-by-lookup mapping: verify-assertion below looks the
	// encrypted_token back out of this exact string.
	writeJSON(w, map[string]string{"signed_assertion": "signed-assertion-for:" + in.EncryptedToken})
}

func (m *mockRP) handleVerifyAssertion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SignedAssertion string `json:"signed_assertion"`
		ExpectedDomain  string `json:"expected_domain"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	encryptedToken := strings.TrimPrefix(in.SignedAssertion, "signed-assertion-for:")
	m.mu.Lock()
	a, ok := m.assertions[encryptedToken]
	m.mu.Unlock()
	if !ok {
		writeJSON(w, map[string]any{"verified": false, "assertion": map[string]any{}})
		return
	}
	writeJSON(w, map[string]any{
		"verified": a.verified,
		"assertion": map[string]any{
			"user_id":      a.userID,
			"domain":       a.domain,
			"audience":     a.audience,
			"nonce":        a.nonce,
			"issued_at":    time.Now().UTC().Format(time.RFC3339),
			"expires_at":   a.expiresAt,
			"display_name": a.displayName,
		},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
