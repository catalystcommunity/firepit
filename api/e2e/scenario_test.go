//go:build integration

// Package e2e_test is task D2's end-to-end integration suite: it boots the
// REAL firepit-api HTTP handler (every real csilservices implementation,
// notify.NewDBPublisher — never notify.Noop{} — exactly what
// api/cmd/firepit-api/main.go wires) against a single testcontainers
// Postgres, with a mock linkkeys RP standing in for the sidecar (same
// pattern as api/internal/server/auth_integration_test.go), and drives the
// WHOLE product surface purely over HTTP in CBOR through
// api/internal/csil's codecs + api/internal/transport's envelope helpers —
// the same "two options task B1 allows" precedent server_integration_test.go
// documents, not the generated clients/go client (a separate Go module with
// its own Transport convention — see that file's doc comment for why
// pulling it in here would add a replace-directive dance for no extra
// coverage).
//
// One ordered scenario, one shared Postgres container, one shared HTTP
// server: state deliberately builds up across TestFirepitEndToEnd's
// subtests (two users, a board, a 3-deep comment thread, an edit, an
// endorsement, a mention, a GitHub webhook), exactly like a real forum
// session would. See the top-of-function comment for the scenario map.
package e2e_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/catalystcommunity/firepit/api/internal/github"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/server"
	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/api/internal/transport"
	"github.com/catalystcommunity/firepit/coredb"
)

// --- generic CSIL-RPC-over-HTTP call helpers ---
//
// Every op in csil/firepit.csil compiles down to one (service, op) pair
// dispatched by api/internal/server/dispatch.go's routing table, with a
// per-op Encode<Req>/Decode<Resp> pair in api/internal/csil/codec.gen.go.
// rpcCall below is the one seam this whole suite drives every op through —
// CBOR envelope out, CBOR envelope in, over a real net/http round trip —
// parameterized on the exact (Req, Resp) pair each call site already knows
// from csil/firepit.csil's declared signature, so every call site below
// reads as "service/op, request, encode fn, decode fn" and nothing more.

// rpcCall drives one CSIL-RPC op over HTTP: encodes req, wraps it in an
// envelope, posts it to /csil/v1/rpc (with sessionToken as the
// firepit_session cookie when non-empty), and decodes the reply. Returns
// (zero, non-nil) when the server replied with the op's declared
// ServiceError arm — the caller decides whether that's expected (svcErr's
// Code, e.g. csilservices.CodeForbidden) or a test failure. A transport-level
// failure (malformed envelope, unknown op, ...) always fails the test
// outright via require, since none of this suite's calls are expected to hit
// one.
func rpcCall[Req any, Resp any](t *testing.T, ts *httptest.Server, sessionToken, service, op string, req Req, encode func(Req) []byte, decode func([]byte) (Resp, error)) (Resp, *csil.ServiceError) {
	t.Helper()

	payload := encode(req)
	envelope, err := transport.NewRpcRequest(service, op, payload).Encode()
	require.NoError(t, err)

	httpReq, err := http.NewRequest(http.MethodPost, ts.URL+"/csil/v1/rpc", bytes.NewReader(envelope))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/cbor")
	if sessionToken != "" {
		httpReq.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: sessionToken})
	}

	httpResp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer httpResp.Body.Close()
	require.Equal(t, http.StatusOK, httpResp.StatusCode, "%s/%s", service, op)

	body, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)

	rpcResp, err := transport.DecodeRpcResponse(body)
	require.NoError(t, err)
	require.NoError(t, rpcResp.AsTransportError(), "%s/%s: unexpected transport failure", service, op)
	require.NotNil(t, rpcResp.Variant, "%s/%s", service, op)

	if *rpcResp.Variant == "ServiceError" {
		svcErr, err := csil.DecodeServiceError(rpcResp.Payload)
		require.NoError(t, err)
		var zero Resp
		return zero, &svcErr
	}

	resp, err := decode(rpcResp.Payload)
	require.NoError(t, err)
	return resp, nil
}

// mustRPC is rpcCall for the common case: the caller asserts success and
// only wants the decoded response.
func mustRPC[Req any, Resp any](t *testing.T, ts *httptest.Server, sessionToken, service, op string, req Req, encode func(Req) []byte, decode func([]byte) (Resp, error)) Resp {
	t.Helper()
	resp, svcErr := rpcCall(t, ts, sessionToken, service, op, req, encode, decode)
	require.Nil(t, svcErr, "%s/%s: unexpected ServiceError: %+v", service, op, svcErr)
	return resp
}

func strPtr(s string) *string { return &s }

// --- mock linkkeys RP (HTTP/JSON PKI shim) ---
//
// Duplicated from api/internal/server/auth_integration_test.go's mockRP
// rather than shared: that type is unexported (package server_test), and
// every other integration test file in this codebase (server, github,
// store, csilservices) already re-establishes its own testcontainers
// boilerplate per file rather than factoring out a shared test-helper
// package, so this follows the same convention. The wire shapes below
// (sign-request/decrypt-token/verify-assertion JSON) are exactly what
// api/internal/linkkeys/client.go speaks, byte-for-byte identical to the
// real sidecar's contract.
type mockAssertion struct {
	verified    bool
	userID      string
	domain      string
	audience    string
	nonce       string
	expiresAt   string
	displayName string
}

type mockRP struct {
	mu         sync.Mutex
	lastNonce  string
	assertions map[string]mockAssertion // keyed by encrypted_token
}

func newMockRP() *mockRP {
	return &mockRP{assertions: map[string]mockAssertion{}}
}

func (m *mockRP) setAssertion(encryptedToken string, a mockAssertion) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assertions[encryptedToken] = a
}

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

// signGithubPayload computes the X-Hub-Signature-256 header value GitHub
// itself would send for body signed under secret (HMAC-SHA256, hex-encoded,
// "sha256=" prefixed) — the exact scheme api/internal/github/verify.go's
// VerifySignature checks.
func signGithubPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestFirepitEndToEnd is task D2's full-product scenario. Scenario map
// (letters match PLANDOC.md §7 D2's SCOPE list):
//
//	a. Two users log in via the mock RP (begin-login -> callback -> session
//	   cookie); user A is bootstrapped to instance admin via a direct store
//	   update (admin bootstrap is a seeding concern per task D3, not a CSIL
//	   op — there is no "make me admin" endpoint).
//	b. Admin (A) creates a board; an anonymous caller gets Unauthenticated,
//	   a logged-in non-admin (B) gets Forbidden.
//	c. A posts; B replies 3 levels deep; get-thread returns the tree
//	   depth-first with author_handle populated on the post and every
//	   comment.
//	d. B edits the middle comment; list-revisions shows the pre-edit
//	   snapshot; the comment's edited_at is set on re-fetch.
//	e. B subscribes to the board; A endorses B's top-level comment -> B
//	   gets an `endorsed` notification with A's actor handle;
//	   list-endorsements renders A's handle.
//	f. A posts a new top-level comment mentioning "@"+B's handle (B's
//	   mention_policy defaults to 'subscribed', and B is subscribed to the
//	   board from (e), so the mention notifies); B's unread-summary shows
//	   the board unread; B marks the post read -> it clears; B marks the
//	   mention comment unread -> the override brings the board back.
//	g. Admin creates a GitHub mapping on the same board; a signed
//	   issues-opened webhook lands a system post with origin "github"; B
//	   (still a board subscriber) gets a new_post notification whose actor
//	   is the repo's system user.
//	h. resolve-user finds B by handle; whoami confirms A's session; logout
//	   ends it (a subsequent whoami is Unauthenticated).
//
// No step here needs require.Eventually/polling: PLANDOC.md §3 says
// notification fan-out "runs in-process on write" — every notify.Publisher
// call happens inside the same DB transaction as the content write it
// reacts to, which commits before the HTTP handler returns the response at
// all. There is no async boundary in this architecture for a poll to cross.
func TestFirepitEndToEnd(t *testing.T) {
	ctx := context.Background()

	// --- one shared Postgres, migrated once ---
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

	// --- mock linkkeys RP ---
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

	// notify.NewDBPublisher (the real fan-out — never notify.Noop{}) is the
	// whole point of this suite: it's what proves subscriptions, mentions,
	// and endorsements actually notify anyone, over the real wire.
	pub := notify.NewDBPublisher()
	svcs := server.Services{
		Auth:         csilservices.NewAuthService(st, cfg),
		Board:        csilservices.NewBoardService(st),
		Thread:       csilservices.NewThreadService(st, pub),
		Endorsement:  csilservices.NewEndorsementService(st, pub),
		Settings:     csilservices.NewSettingsService(st),
		Social:       csilservices.NewSocialService(st),
		Subscription: csilservices.NewSubscriptionService(st),
		Read:         csilservices.NewReadService(st),
		Notification: csilservices.NewNotificationService(st),
		Integration:  csilservices.NewIntegrationService(st),
	}
	srv := server.New(cfg, st, svcs, pub)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// noRedirectClient never follows GET /auth/callback's 302 — the test
	// needs to see the redirect response itself (status + Set-Cookie), not
	// whatever page it points at.
	noRedirectClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// login drives the full begin-login -> callback -> session-cookie flow
	// for one brand-new linkkeys identity and returns the raw session token
	// plus the resulting whoami profile.
	login := func(t *testing.T, encToken, linkkeysUserID, displayName string) (rawSessionToken string, profile csil.UserProfile) {
		t.Helper()

		beginResp := mustRPC(t, ts, "", "auth", "begin-login",
			csil.BeginLoginRequest{Domain: idpDomain},
			csil.EncodeAuthBeginLoginRequest, csil.DecodeAuthBeginLoginResponse)
		require.Contains(t, beginResp.RedirectUrl, "https://idp.example/auth/authorize?signed_request=")
		nonce := rp.takeLastNonce(t)

		rp.setAssertion(encToken, mockAssertion{
			verified:    true,
			userID:      linkkeysUserID,
			domain:      idpDomain,
			audience:    rpDomain,
			nonce:       nonce,
			expiresAt:   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			displayName: displayName,
		})

		resp, err := noRedirectClient.Get(ts.URL + "/auth/callback?" + url.Values{"encrypted_token": {encToken}}.Encode())
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusFound, resp.StatusCode, "expected a redirect on successful login")

		for _, c := range resp.Cookies() {
			if c.Name == server.SessionCookieName {
				rawSessionToken = c.Value
			}
		}
		require.NotEmpty(t, rawSessionToken, "expected a firepit_session cookie to be set")

		profile = mustRPC(t, ts, rawSessionToken, "auth", "whoami",
			csil.Empty{}, csil.EncodeAuthWhoamiRequest, csil.DecodeAuthWhoamiResponse)
		return rawSessionToken, profile
	}

	// --- shared scenario state, built up across subtests below ---
	var (
		aToken, bToken     string
		aProfile, bProfile csil.UserProfile
		board              csil.Board
		post               csil.Post
		c1, c2, c3         csil.Comment // a 3-deep reply chain: c1 -> c2 -> c3
		mentionComment     csil.Comment
	)

	t.Run("a_login_and_admin_bootstrap", func(t *testing.T) {
		aToken, aProfile = login(t, "enc-token-a", "linkkeys-user-a", "Alice Admin")
		bToken, bProfile = login(t, "enc-token-b", "linkkeys-user-b", "Bob Bystander")

		require.NotEqual(t, aProfile.Id, bProfile.Id)
		require.Equal(t, "alice-admin", aProfile.Handle)
		require.Equal(t, "bob-bystander", bProfile.Handle)
		require.Empty(t, aProfile.Roles, "brand new users start with no roles")

		// Admin bootstrap is a direct store write, not a CSIL op — there is
		// no "grant-admin" endpoint in csil/firepit.csil (PLANDOC.md §5).
		// Seeding a real deployment's first admin this way is task D3's
		// concern (documented there); this is the equivalent one-line
		// operator action for the test.
		require.NoError(t, gdb.Model(&store.User{}).
			Where("id = ?", string(aProfile.Id)).
			Update("roles", store.StringArray{"admin"}).Error)

		// Confirm the bootstrap actually took over the wire, not just in
		// the DB: a fresh whoami reflects the new role.
		aProfile = mustRPC(t, ts, aToken, "auth", "whoami",
			csil.Empty{}, csil.EncodeAuthWhoamiRequest, csil.DecodeAuthWhoamiResponse)
		require.Equal(t, []string{"admin"}, aProfile.Roles)
	})

	t.Run("b_create_board_authz", func(t *testing.T) {
		createReq := csil.CreateBoardRequest{Slug: "e2e-general", Title: "E2E General", Description: strPtr("the scenario's one board"), Kind: "discussion"}

		_, anonErr := rpcCall(t, ts, "", "board", "create-board", createReq,
			csil.EncodeBoardCreateBoardRequest, csil.DecodeBoardCreateBoardResponse)
		require.NotNil(t, anonErr, "anonymous create-board must fail")
		require.Equal(t, csilservices.CodeUnauthenticated, anonErr.Code)

		_, nonAdminErr := rpcCall(t, ts, bToken, "board", "create-board", createReq,
			csil.EncodeBoardCreateBoardRequest, csil.DecodeBoardCreateBoardResponse)
		require.NotNil(t, nonAdminErr, "non-admin create-board must fail")
		require.Equal(t, csilservices.CodeForbidden, nonAdminErr.Code)

		board = mustRPC(t, ts, aToken, "board", "create-board", createReq,
			csil.EncodeBoardCreateBoardRequest, csil.DecodeBoardCreateBoardResponse)
		require.Equal(t, "e2e-general", board.Slug)
		require.Equal(t, aProfile.Id, board.CreatedBy)
	})

	t.Run("c_post_and_deep_reply_thread", func(t *testing.T) {
		post = mustRPC(t, ts, aToken, "thread", "create-post",
			csil.CreatePostRequest{BoardId: board.Id, Title: "Welcome to the forum", BodyMd: "First post, let's see if threading works."},
			csil.EncodeThreadCreatePostRequest, csil.DecodeThreadCreatePostResponse)
		require.NotNil(t, post.AuthorHandle)
		require.Equal(t, aProfile.Handle, *post.AuthorHandle)

		c1 = mustRPC(t, ts, bToken, "thread", "create-comment",
			csil.CreateCommentRequest{PostId: post.Id, BodyMd: "Level 1 reply."},
			csil.EncodeThreadCreateCommentRequest, csil.DecodeThreadCreateCommentResponse)
		c2 = mustRPC(t, ts, bToken, "thread", "create-comment",
			csil.CreateCommentRequest{PostId: post.Id, ParentCommentId: &c1.Id, BodyMd: "Level 2 reply."},
			csil.EncodeThreadCreateCommentRequest, csil.DecodeThreadCreateCommentResponse)
		c3 = mustRPC(t, ts, bToken, "thread", "create-comment",
			csil.CreateCommentRequest{PostId: post.Id, ParentCommentId: &c2.Id, BodyMd: "Level 3 reply."},
			csil.EncodeThreadCreateCommentRequest, csil.DecodeThreadCreateCommentResponse)

		thread := mustRPC(t, ts, bToken, "thread", "get-thread",
			csil.GetThreadRequest{PostId: post.Id},
			csil.EncodeThreadGetThreadRequest, csil.DecodeThreadGetThreadResponse)

		require.Equal(t, post.Id, thread.Post.Id)
		require.NotNil(t, thread.Post.AuthorHandle)
		require.Equal(t, aProfile.Handle, *thread.Post.AuthorHandle)

		require.Len(t, thread.Comments, 3, "the 3-deep reply chain, depth-first")
		require.Equal(t, []csil.CommentID{c1.Id, c2.Id, c3.Id}, []csil.CommentID{thread.Comments[0].Id, thread.Comments[1].Id, thread.Comments[2].Id},
			"ltree path ordering must put each parent immediately before its child")
		for i, c := range thread.Comments {
			require.NotNilf(t, c.AuthorHandle, "comment %d", i)
			require.Equalf(t, bProfile.Handle, *c.AuthorHandle, "comment %d", i)
		}
	})

	t.Run("d_edit_and_revision_history", func(t *testing.T) {
		originalBody := c2.BodyMd

		edited := mustRPC(t, ts, bToken, "thread", "edit-comment",
			csil.EditCommentRequest{Id: c2.Id, BodyMd: "Level 2 reply, corrected."},
			csil.EncodeThreadEditCommentRequest, csil.DecodeThreadEditCommentResponse)
		require.NotNil(t, edited.EditedAt, "edited flag must be set on the response")
		c2 = edited

		revisions := mustRPC(t, ts, bToken, "thread", "list-revisions",
			csil.TargetRef{TargetType: "comment", TargetId: string(c2.Id)},
			csil.EncodeThreadListRevisionsRequest, csil.DecodeThreadListRevisionsResponse)
		require.Len(t, revisions.Revisions, 1)
		require.Equal(t, originalBody, revisions.Revisions[0].PrevBodyMd)
		require.Equal(t, csil.TargetType("comment"), revisions.Revisions[0].TargetType)
		require.Equal(t, string(c2.Id), revisions.Revisions[0].TargetId)

		// Re-fetching the thread confirms the edited flag persisted, not
		// just present on edit-comment's own response.
		thread := mustRPC(t, ts, bToken, "thread", "get-thread",
			csil.GetThreadRequest{PostId: post.Id},
			csil.EncodeThreadGetThreadRequest, csil.DecodeThreadGetThreadResponse)
		require.NotNil(t, thread.Comments[1].EditedAt)
		require.Equal(t, "Level 2 reply, corrected.", thread.Comments[1].BodyMd)
	})

	t.Run("e_subscribe_and_endorse", func(t *testing.T) {
		_ = mustRPC(t, ts, bToken, "subscription", "subscribe",
			csil.TargetRef{TargetType: "board", TargetId: string(board.Id)},
			csil.EncodeSubscriptionSubscribeRequest, csil.DecodeSubscriptionSubscribeResponse)

		endorsement := mustRPC(t, ts, aToken, "endorsement", "endorse",
			csil.EndorseRequest{TargetType: "comment", TargetId: string(c1.Id)},
			csil.EncodeEndorsementEndorseRequest, csil.DecodeEndorsementEndorseResponse)
		require.NotNil(t, endorsement.AuthorHandle)
		require.Equal(t, aProfile.Handle, *endorsement.AuthorHandle)

		list := mustRPC(t, ts, bToken, "endorsement", "list-endorsements",
			csil.TargetRef{TargetType: "comment", TargetId: string(c1.Id)},
			csil.EncodeEndorsementListEndorsementsRequest, csil.DecodeEndorsementListEndorsementsResponse)
		require.Len(t, list.Endorsements, 1, "server-ordered endorser list")
		require.NotNil(t, list.Endorsements[0].AuthorHandle)
		require.Equal(t, aProfile.Handle, *list.Endorsements[0].AuthorHandle)

		notifs := mustRPC(t, ts, bToken, "notification", "list-notifications",
			csil.ListNotificationsRequest{},
			csil.EncodeNotificationListNotificationsRequest, csil.DecodeNotificationListNotificationsResponse)
		found := findNotification(notifs.Notifications, "endorsed", string(c1.Id))
		require.NotNil(t, found, "B must be notified of A's endorsement")
		require.NotNil(t, found.ActorHandle)
		require.Equal(t, aProfile.Handle, *found.ActorHandle)
	})

	t.Run("f_mention_and_read_state", func(t *testing.T) {
		mentionComment = mustRPC(t, ts, aToken, "thread", "create-comment",
			csil.CreateCommentRequest{PostId: post.Id, BodyMd: "Hey @" + bProfile.Handle + ", take a look at this thread."},
			csil.EncodeThreadCreateCommentRequest, csil.DecodeThreadCreateCommentResponse)

		notifs := mustRPC(t, ts, bToken, "notification", "list-notifications",
			csil.ListNotificationsRequest{},
			csil.EncodeNotificationListNotificationsRequest, csil.DecodeNotificationListNotificationsResponse)
		mentionNotif := findNotification(notifs.Notifications, "mention", string(mentionComment.Id))
		require.NotNil(t, mentionNotif, "B's default 'subscribed' mention_policy + board subscription from (e) must notify")
		require.NotNil(t, mentionNotif.ActorHandle)
		require.Equal(t, aProfile.Handle, *mentionNotif.ActorHandle)

		summary := mustRPC(t, ts, bToken, "read", "unread-summary",
			csil.Empty{}, csil.EncodeReadUnreadSummaryRequest, csil.DecodeReadUnreadSummaryResponse)
		unread := findBoardUnread(summary.Boards, board.Id)
		require.NotNil(t, unread, "the new mention comment must show up as unread for B")
		require.Contains(t, unread.UnreadPostIds, post.Id)

		_ = mustRPC(t, ts, bToken, "read", "mark-read",
			csil.TargetRef{TargetType: "post", TargetId: string(post.Id)},
			csil.EncodeReadMarkReadRequest, csil.DecodeReadMarkReadResponse)

		summary = mustRPC(t, ts, bToken, "read", "unread-summary",
			csil.Empty{}, csil.EncodeReadUnreadSummaryRequest, csil.DecodeReadUnreadSummaryResponse)
		require.Nil(t, findBoardUnread(summary.Boards, board.Id), "mark-read on the post must clear the board's unread entry")

		_ = mustRPC(t, ts, bToken, "read", "mark-unread",
			csil.TargetRef{TargetType: "comment", TargetId: string(mentionComment.Id)},
			csil.EncodeReadMarkUnreadRequest, csil.DecodeReadMarkUnreadResponse)

		summary = mustRPC(t, ts, bToken, "read", "unread-summary",
			csil.Empty{}, csil.EncodeReadUnreadSummaryRequest, csil.DecodeReadUnreadSummaryResponse)
		unread = findBoardUnread(summary.Boards, board.Id)
		require.NotNil(t, unread, "an explicit mark-unread override must bring the board back")
		require.Contains(t, unread.UnreadPostIds, post.Id)
	})

	t.Run("g_github_webhook_ingestion", func(t *testing.T) {
		const repo = "catalystcommunity/firepit"
		const secretEnvVar = "FIREPIT_GH_SECRET_E2E_TEST"
		const secret = "e2e-s3kr1t-webhook-secret"
		t.Setenv(secretEnvVar, secret)

		mapping := mustRPC(t, ts, aToken, "integration", "create-github-mapping",
			csil.CreateMappingRequest{
				BoardId:    board.Id,
				Repo:       repo,
				Events:     []string{"issues", "pull_request", "release"},
				SecretRef:  secretEnvVar,
				ThreadMode: "post_per_issue",
			},
			csil.EncodeIntegrationCreateGithubMappingRequest, csil.DecodeIntegrationCreateGithubMappingResponse)
		require.Equal(t, board.Id, mapping.BoardId)

		body := github.FixtureIssuesOpened
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/github", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "issues")
		req.Header.Set("X-GitHub-Delivery", "delivery-e2e-issue-42-opened")
		req.Header.Set("X-Hub-Signature-256", signGithubPayload(secret, body))

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		posts := mustRPC(t, ts, bToken, "thread", "list-posts",
			csil.ListPostsRequest{BoardId: board.Id},
			csil.EncodeThreadListPostsRequest, csil.DecodeThreadListPostsResponse)
		var githubPost *csil.Post
		for i := range posts.Posts {
			if posts.Posts[i].Origin == "github" {
				githubPost = &posts.Posts[i]
				break
			}
		}
		require.NotNil(t, githubPost, "the webhook must land a system post on the board")
		require.Contains(t, githubPost.Title, "#42")

		notifs := mustRPC(t, ts, bToken, "notification", "list-notifications",
			csil.ListNotificationsRequest{},
			csil.EncodeNotificationListNotificationsRequest, csil.DecodeNotificationListNotificationsResponse)
		newPostNotif := findNotification(notifs.Notifications, "new_post", string(githubPost.Id))
		require.NotNil(t, newPostNotif, "B (a board subscriber) must be notified of the system post")
		require.NotNil(t, newPostNotif.ActorHandle)
		// The repo's system user handle is "github-" + the repo slug
		// (owner/name, slashes and underscores replaced with hyphens) —
		// see api/internal/github/contentwriter.go's systemUserIdentity,
		// asserted the same literal way api/internal/github's own
		// webhook_integration_test.go does.
		require.Equal(t, "github-catalystcommunity-firepit", *newPostNotif.ActorHandle)
	})

	t.Run("h_resolve_user_and_session_lifecycle", func(t *testing.T) {
		resolved := mustRPC(t, ts, aToken, "social", "resolve-user",
			csil.Handle(bProfile.Handle),
			csil.EncodeSocialResolveUserRequest, csil.DecodeSocialResolveUserResponse)
		require.Equal(t, bProfile.Id, resolved.Id)
		require.Equal(t, bProfile.Handle, resolved.Handle)

		who := mustRPC(t, ts, aToken, "auth", "whoami",
			csil.Empty{}, csil.EncodeAuthWhoamiRequest, csil.DecodeAuthWhoamiResponse)
		require.Equal(t, aProfile.Id, who.Id)

		_ = mustRPC(t, ts, aToken, "auth", "logout",
			csil.Empty{}, csil.EncodeAuthLogoutRequest, csil.DecodeAuthLogoutResponse)

		_, svcErr := rpcCall(t, ts, aToken, "auth", "whoami",
			csil.Empty{}, csil.EncodeAuthWhoamiRequest, csil.DecodeAuthWhoamiResponse)
		require.NotNil(t, svcErr, "the session must be gone after logout")
		require.Equal(t, csilservices.CodeUnauthenticated, svcErr.Code)
	})
}

func findNotification(notifs []csil.Notification, event, targetID string) *csil.Notification {
	for i := range notifs {
		if string(notifs[i].Event) == event && notifs[i].TargetId == targetID {
			return &notifs[i]
		}
	}
	return nil
}

func findBoardUnread(boards []csil.BoardUnread, boardID csil.BoardID) *csil.BoardUnread {
	for i := range boards {
		if boards[i].BoardId == boardID {
			return &boards[i]
		}
	}
	return nil
}
