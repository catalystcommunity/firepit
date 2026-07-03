//go:build integration

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/catalystcommunity/firepit/api/internal/store"
	"github.com/catalystcommunity/firepit/coredb"
)

// TestWebhookGoldenPayloads drives Handler.ServeHTTP directly (no real HTTP
// listener needed — httptest.NewRecorder is enough) against a real
// Postgres, using the golden GitHub webhook fixtures in testdata/, and
// asserts the resulting posts/comments/system-user rows per task B8's
// acceptance criteria: "golden-payload tests ... for each event type incl.
// signature verification (valid, invalid, missing), unmapped drop,
// idempotent redelivery; integration tests ... asserting posts/comments
// land with correct origin/origin_ref/system-user."
//
// Run via `go test -tags=integration ./internal/github/...` — not yet
// wired into tools.sh's test-integration verb (today: internal/store,
// internal/server only); that gap is being closed centrally.
func TestWebhookGoldenPayloads(t *testing.T) {
	ctx := context.Background()
	const repo = "catalystcommunity/firepit"
	const secretEnvVar = "FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT_TEST"
	const secret = "s3kr1t-webhook-secret"

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

	t.Setenv(secretEnvVar, secret)

	admin := &store.User{LinkkeysDomain: "example.com", LinkkeysUserID: "admin-1", Handle: "admin", Kind: "human", Roles: store.StringArray{"admin"}}
	require.NoError(t, gdb.Create(admin).Error)
	board := &store.Board{Slug: "firepit-dev", Title: "Firepit Dev", Kind: "discussion", CreatedBy: admin.ID}
	require.NoError(t, gdb.Create(board).Error)

	mapping := &store.GithubMapping{
		BoardID:    board.ID,
		Repo:       repo,
		Events:     store.StringArray{"issues", "pull_request", "release"},
		SecretRef:  secretEnvVar,
		ThreadMode: "post_per_issue",
		CreatedBy:  admin.ID,
	}
	require.NoError(t, st.CreateGithubMapping(ctx, mapping))

	handler := NewWebhookHandler(st)

	post := func(t *testing.T, body []byte, event, deliveryID, signature string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if event != "" {
			req.Header.Set("X-GitHub-Event", event)
		}
		if deliveryID != "" {
			req.Header.Set("X-GitHub-Delivery", deliveryID)
		}
		if signature != "" {
			req.Header.Set("X-Hub-Signature-256", signature)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	countPosts := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		require.NoError(t, gdb.Model(&store.Post{}).Count(&n).Error)
		return n
	}
	countComments := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		require.NoError(t, gdb.Model(&store.Comment{}).Count(&n).Error)
		return n
	}
	findPostByNumber := func(t *testing.T, kind string, number int) *store.Post {
		t.Helper()
		var p store.Post
		err := gdb.Where("origin = ? AND origin_ref ->> 'repo' = ? AND origin_ref ->> 'kind' = ? AND origin_ref ->> 'number' = ?",
			"github", repo, kind, strconv.Itoa(number)).First(&p).Error
		if store.IsNotFound(err) {
			return nil
		}
		require.NoError(t, err)
		return &p
	}
	systemUser := func(t *testing.T) *store.User {
		t.Helper()
		var u store.User
		require.NoError(t, gdb.Where("linkkeys_domain = ? AND linkkeys_user_id = ?", systemUserLinkkeysDomain, repo).First(&u).Error)
		return &u
	}

	t.Run("missing signature is rejected 401", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixtureIssuesOpened, "issues", "delivery-missing-sig", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, before, countPosts(t))
	})

	t.Run("invalid signature is rejected 401", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixtureIssuesOpened, "issues", "delivery-bad-sig", "sha256="+"0000000000000000000000000000000000000000000000000000000000000000")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, before, countPosts(t))
	})

	t.Run("unmapped repo is dropped 200", func(t *testing.T) {
		before := countPosts(t)
		// No mapping exists for this repo, so no secret to sign against —
		// an unsigned request is still expected to 200-and-drop, never a
		// signature failure, since the repo lookup happens first.
		rec := post(t, FixtureIssuesOpenedUnmappedRepo, "issues", "delivery-unmapped", "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, before, countPosts(t))
	})

	t.Run("ping is verified and answered without processing", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixturePing, "ping", "delivery-ping", sign(secret, FixturePing))
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "pong", body["status"])
		require.Equal(t, before, countPosts(t))
	})

	const issueOpenedDeliveryID = "delivery-issue-42-opened"
	t.Run("issues opened creates a post authored by the repo's system user", func(t *testing.T) {
		rec := post(t, FixtureIssuesOpened, "issues", issueOpenedDeliveryID, sign(secret, FixtureIssuesOpened))
		require.Equal(t, http.StatusOK, rec.Code)

		p := findPostByNumber(t, "issue", 42)
		require.NotNil(t, p, "expected a post for issue #42")
		require.Equal(t, board.ID, p.BoardID)
		require.Equal(t, "github", p.Origin)
		require.Contains(t, p.Title, "#42")

		var ref OriginRef
		require.NoError(t, json.Unmarshal(p.OriginRef, &ref))
		require.Equal(t, repo, ref.Repo)
		require.Equal(t, "issue", ref.Kind)
		require.Equal(t, 42, ref.Number)
		require.Equal(t, issueOpenedDeliveryID, ref.DeliveryID)
		require.Equal(t, "https://github.com/catalystcommunity/firepit/issues/42", ref.URL)

		sysUser := systemUser(t)
		require.Equal(t, "system", sysUser.Kind)
		require.Equal(t, "github-catalystcommunity-firepit", sysUser.Handle)
		require.Equal(t, p.AuthorID, sysUser.ID)
	})

	t.Run("issues closed adds a top-level comment on the existing post", func(t *testing.T) {
		p := findPostByNumber(t, "issue", 42)
		require.NotNil(t, p)
		commentsBefore := countComments(t)

		rec := post(t, FixtureIssuesClosed, "issues", "delivery-issue-42-closed", sign(secret, FixtureIssuesClosed))
		require.Equal(t, http.StatusOK, rec.Code)

		require.Equal(t, commentsBefore+1, countComments(t))

		var comment store.Comment
		require.NoError(t, gdb.Where("post_id = ?", p.ID).First(&comment).Error)
		require.Equal(t, "github", comment.Origin)
		require.Nil(t, comment.ParentCommentID)
		require.Equal(t, store.Ltree(comment.ID), comment.Path, "a top-level comment's path is its own id")

		var updatedPost store.Post
		require.NoError(t, gdb.First(&updatedPost, "id = ?", p.ID).Error)
		require.Equal(t, 1, updatedPost.CommentCount)
	})

	t.Run("idempotent redelivery of the same delivery id is a no-op", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixtureIssuesOpened, "issues", issueOpenedDeliveryID, sign(secret, FixtureIssuesOpened))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, before, countPosts(t), "redelivering the same X-GitHub-Delivery must not create a second post")
	})

	t.Run("release published creates a new post", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixtureReleasePublished, "release", "delivery-release-v0.3.0", sign(secret, FixtureReleasePublished))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, before+1, countPosts(t))

		p := findPostByNumber(t, "release", 190837465)
		require.NotNil(t, p)
		require.Contains(t, p.Title, "Release")
	})

	t.Run("pull_request closed unmerged is dropped", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixturePullRequestClosedUnmerged, "pull_request", "delivery-pr-8-closed", sign(secret, FixturePullRequestClosedUnmerged))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, before, countPosts(t))
	})

	t.Run("pull_request merged creates a post (no prior opened-event thread)", func(t *testing.T) {
		before := countPosts(t)
		rec := post(t, FixturePullRequestMerged, "pull_request", "delivery-pr-7-merged", sign(secret, FixturePullRequestMerged))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, before+1, countPosts(t))

		p := findPostByNumber(t, "pull_request", 7)
		require.NotNil(t, p)

		var ref OriginRef
		require.NoError(t, json.Unmarshal(p.OriginRef, &ref))
		require.Equal(t, "pull_request", ref.Kind)
		require.Equal(t, 7, ref.Number)
	})

	t.Run("a second merge delivery for the same PR comments instead of posting again", func(t *testing.T) {
		beforePosts := countPosts(t)
		beforeComments := countComments(t)
		rec := post(t, FixturePullRequestMerged, "pull_request", "delivery-pr-7-merged-retry", sign(secret, FixturePullRequestMerged))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, beforePosts, countPosts(t), "should not create a second post for the same PR")
		require.Equal(t, beforeComments+1, countComments(t), "should comment on the existing PR post instead")
	})
}
