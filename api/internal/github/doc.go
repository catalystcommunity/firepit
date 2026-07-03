// Package github implements task B8: the GitHub webhook receiver
// (POST /webhooks/github, wired in by api/internal/server/webhooks.go) and
// its event -> firepit content rules (PLANDOC.md §4 github_mappings design
// notes, §5 IntegrationService, §7 B8, §9 decision 6).
//
// # Request flow (see Handler.ServeHTTP)
//
//  1. Read the whole body (capped at maxBodyBytes) and the X-GitHub-Event /
//     X-GitHub-Delivery / X-Hub-Signature-256 headers.
//  2. Peek repository.full_name out of the JSON body (peekRepoFullName) —
//     every repo-scoped GitHub webhook payload carries this, and it's needed
//     BEFORE signature verification to know which mapping's secret to
//     verify against.
//  3. Look up the store.GithubMapping for that repo (repo has a UNIQUE
//     constraint in the schema, so at most one). No mapping -> HTTP 200,
//     drop (an unmapped repo is not this instance's problem, and 200 keeps
//     GitHub from retrying/disabling the hook).
//  4. Resolve the mapping's secret (see "Secret resolution" below) and
//     verify the HMAC-SHA256 signature with a constant-time comparison
//     (VerifySignature). Missing/invalid signature, or a secret_ref that
//     doesn't resolve to anything -> HTTP 401. This is a hard fail-closed:
//     an unresolvable secret is treated the same as a bad signature, never
//     as "skip verification."
//  5. "ping" events (GitHub's webhook-configured connectivity check) get a
//     200 once signature-verified, with no further processing.
//  6. If the event type isn't in mapping.Events, or isn't one of the event
//     types this package understands at all (see "Event rules" below) ->
//     HTTP 200, drop.
//  7. Idempotency: if a post or comment already exists whose origin_ref
//     carries this delivery's X-GitHub-Delivery id, the event has already
//     been applied (a GitHub redelivery) -> HTTP 200, no-op.
//  8. Otherwise, apply the event rule (contentWriter.HandleEvent) and
//     answer HTTP 200.
//
// Every branch above that isn't a signature failure answers HTTP 200 —
// GitHub webhook delivery retries/disables a hook on repeated non-2xx
// responses, and "we understood you but chose not to act" (unmapped repo,
// unsubscribed event type, already-processed redelivery) is not a delivery
// failure.
//
// # Secret resolution (secret_ref scheme)
//
// github_mappings.secret_ref is NOT the HMAC secret itself — it is the name
// of an environment variable firepit-api reads at verification time
// (ResolveSecret does exactly this: os.Getenv(mapping.SecretRef)). The
// convention operators are expected to follow, and that
// IntegrationService.CreateGithubMapping documents to admins, is:
//
//	FIREPIT_GH_SECRET_<MAPPING_ID_OR_NAME>
//
// e.g. FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT for a mapping covering
// catalystcommunity/firepit, or FIREPIT_GH_SECRET_01HXYZ... keyed on the
// mapping's own ULID once it's known. The admin picks the exact env var
// name (any non-empty string is accepted as secret_ref — see
// CreateGithubMapping's validation), sets it in the deployment's env (a k8s
// Secret mounted as env vars, in the real deployment — see PLANDOC.md's
// decision 6: "plain per-repo webhooks with HMAC secrets in v1"), and
// configures the SAME value as the GitHub repo's webhook secret. Storing
// only the env var *name* in the database — never the secret value itself —
// means a database dump/backup never leaks webhook secrets.
//
// # origin_ref shape
//
// Every post/comment this package creates carries origin='github' and an
// origin_ref jsonb payload shaped as:
//
//	{
//	  "repo":        "owner/name",
//	  "kind":        "issue" | "pull_request" | "release",
//	  "number":      123,                 // issue/PR number, or the release's numeric id
//	  "url":         "https://github.com/...",   // the GitHub html_url for the source object
//	  "delivery_id": "<X-GitHub-Delivery header>"
//	}
//
// See OriginRef. "number" + "kind" + "repo" is how contentWriter finds the
// post an issue/PR event should comment on (findPostByOriginRef, a jsonb
// ->> text-extraction query); "delivery_id" is the idempotency key
// (deliveryProcessed).
//
// # Event rules v1
//
// Only three GitHub event types are understood; anything else is dropped
// (step 6 above) even if a mapping subscribes to it:
//
//   - release/published: always creates a new post. The release's numeric
//     id is used as origin_ref.number (releases don't have an issue/PR-style
//     sequence number).
//   - issues/opened: creates a new post keyed on the issue number.
//   - issues/closed: finds the post created for that issue number and adds
//     a top-level comment. If none exists (e.g. the mapping was added after
//     the issue was opened, so no "opened" event was ever seen) it falls
//     back to creating the post instead of dropping the event, so a closed
//     issue's content is never silently lost.
//   - pull_request/closed with merged=true (anything else on pull_request
//     is dropped — v1 does not track "opened"): behavior depends on
//     mapping.ThreadMode.
//   - "post_per_pull_request": comment on the PR's post if one already
//     exists (future-proofing for when an "opened" rule lands), else
//     create one.
//   - any other thread_mode ("post_per_issue" or "post_per_release", the
//     schema's defaults): the "comment on an existing thread" behavior is
//     identical, since v1 never pre-creates a PR post at "opened" time —
//     this case exists so the distinction is meaningful once a later
//     iteration adds pull_request/opened handling.
//
// # contentWriter — TEMPORARY, unify with ThreadService post-merge
//
// contentWriter (contentwriter.go) writes posts and TOP-LEVEL comments
// directly via *store.Store (raw gorm calls: Create, a two-step
// insert-then-set-ltree-path for a comment's materialized path, and a
// comment_count/last_activity_at bump on the parent post) rather than going
// through ThreadService.CreatePost/CreateComment.
//
// This is deliberate, not an oversight: ThreadService (task B4) is being
// implemented concurrently in a sibling worktree and could not be called
// from here without depending on unmerged, unstable code. Once both land,
// TODO(post-merge): replace contentWriter's direct store writes with calls
// to ThreadService (or whatever internal repository it settles on for
// post/comment creation + ltree path maintenance), so there is exactly one
// code path that knows how to create a post or comment. Until then, the
// ltree path convention here (a top-level comment's own id, unqualified —
// see createTopLevelComment) is chosen to match the "materialized path"
// design PLANDOC.md §4 describes, but is NOT guaranteed to be byte-for-byte
// what ThreadService's eventual implementation produces for nested replies;
// only top-level comments are ever created here, which sidesteps the
// nested-path question entirely.
//
// # System user per mapping
//
// Each github_mappings row's repo gets its own users row (kind='system'),
// created lazily on first event (getOrCreateSystemUser): linkkeys_domain is
// the constant "github.firepit.system", linkkeys_user_id is the repo string
// itself ("owner/name" — combined with the constant domain, this satisfies
// the users table's UNIQUE(linkkeys_domain, linkkeys_user_id) constraint,
// giving at most one system user per repo), and handle is derived as
// "github-<owner>-<name>" (repo lowercased, "/" and "_" replaced with "-").
package github
