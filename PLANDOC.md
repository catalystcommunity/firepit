# Firepit — Plan

A dev coordination forum for open source projects. Threaded discussion in the spirit of
old mailing lists and reddit threads, built for async communication across an open source
community. No upvotes or karma — instead, **endorsements**: putting your name to a post or
comment, publicly. Machine actors (GitHub events, CI, releases) post into the same threads
humans use, so the forum replaces the social layer of GitHub: discussion of projects,
issues, releases, and operating/supporting those projects.

First deployment: `firepit.catalystsquad.com`.

**Status: planning. Nothing here is implemented yet.**

---

## 1. Goals and non-goals

### Goals (v1)

- Boards (per project or topic) containing **posts** (thread roots) with **threaded
  comments** (arbitrary depth, mailing-list style).
- Login with **linkkeys** (`user@domain` identity). Anonymous read access to public boards.
- **Endorsements** on posts and comments — a public, named "I stand behind this." No
  counts-as-ranking, no sorting of *content* by endorsement. Endorsements are
  retractable, always shown inline by name, badge maintainer/mod endorsements
  distinctly, and notify the author (opt-out). The endorser *list itself* is ordered
  per-viewer: your friends first, then by endorser reputation (maintainer status on the
  related project, or endorsements received from trusted linkkeys domains).
- **Subscriptions** to a board, a post, or a specific comment subtree. Notifications only
  for things you subscribed to — never firehose.
- **Read/unread tracking** per user, with manual mark-read/mark-unread.
- **@mentions** that notify the mentioned user — gated by per-user authorization. The
  default is **subscribed-contexts only**: a mention notifies you only in boards/threads
  you already subscribe to; beyond that, only people you've explicitly authorized can
  mention-notify you (with `everyone` and `nobody` also available as policies). Tagging
  never becomes a harassment channel.
- **Edit with full revision history**: content is editable, every edit is preserved and
  inspectable — transparency in place of immutability.
- **GitHub event ingestion**: webhook → system-authored posts/comments in mapped boards,
  so project updates flow into discussion without a human typing them.
- **CSIL-RPC** API (CBOR over `POST /csil/v1/rpc`) as the only client contract, so
  additional frontends (TUI, mobile, bots) are generated clients, not new API surfaces.
- Web frontend: **SolidJS SPA** (Vite + TypeScript).
- CI/CD on **reactorcide**, deployed via Helm + Gateway API `HTTPRoute`.

### Non-goals (v1)

- No email/push digest notifications (in-app only; poll unread counts; live push via
  CSIL-Events is a later milestone).
- No federation between firepit instances (linkkeys already gives cross-domain *identity*;
  cross-instance *content* is out of scope).
- No full-text search (schema leaves room; ship later).
- No private/DM messaging.
- No image/file uploads (markdown + links only in v1).
- No voting, ranking, or algorithmic feeds — ever. Chronological threading only.

---

## 2. Prior art in this org (what we reuse)

| Repo | What we take |
|---|---|
| `longhouse` | The template. Repo layout (`api/`, `coredb/`, `webapp/`, `clients/`, `helm_chart/`), the `api/internal/linkkeys/` RP client package (port near-verbatim), session/cookie handling, SolidJS + CBOR transport patterns (`webapp/src/pages/AuthCallback.tsx`), goose migration style (ULID PKs via `generate_ulid()`, `timestamptz`, soft-delete, audit log). |
| `csilgen` | CSIL IDL + codegen. `csilgen generate --target go` / `--target typescript-client`; reference transports in `transports/go` and `transports/typescript`. |
| `linkkeys` | Auth. We run a linkkeys server in RP mode (`ENABLE_RP_ENDPOINTS=true`) as a sidecar; IDP at `linkkeys.todandlorna.com` for the first deployment. |
| `reactorcide` | CI/CD. `.reactorcide/jobs/*.yaml`, BuildKit builds pushed to `containers.catalystsquad.com`, deploy job runs `helm upgrade`. |
| `app-utils-go` | `config`, `logging` (logrus), `errorutils`, `ulids`. |
| `zest` | SolidJS component/template conventions where useful. |

Org conventions we follow: module path `github.com/catalystcommunity/firepit/<module>`,
conventional commits + semantic-release, `tools.sh` task runner (not Make), `CLAUDE.md` +
`AGENTS.md`, Apache-2.0, Postgres + goose, gorm-on-pgx, testify + testcontainers-go.

---

## 3. Architecture

```
 browser (SolidJS SPA)          other clients (later: TUI, bots, mobile)
        │ CBOR                            │ CBOR
        ▼                                 ▼
  POST /csil/v1/rpc  ─────────  firepit-api (Go)
  GET  /auth/callback                │  ├── csilservices/*  (generated interfaces, hand impl)
  POST /webhooks/github              │  ├── internal/linkkeys (RP client, ported from longhouse)
  GET  /healthz                      │  ├── internal/notify   (fan-out on write)
                                     │  └── internal/github   (webhook → system posts)
                                     │
             linkkeys sidecar ◄──────┤ Bearer api-key (sign-request / decrypt-token /
             (RP mode)               │                 verify-assertion / userinfo)
                                     ▼
                                 Postgres  (coredb: goose migrations)
```

- **One Go binary** (`firepit-api`). Notification fan-out runs in-process on write (a
  forum's write rate doesn't justify a queue; if it ever does, corndogs is the org answer).
- **Three repo modules**: `api/` (service), `coredb/` (migrations, embedded goose), plus
  `webapp/` (npm). `csil/` holds the schema; `clients/<lang>` holds generated clients.
- **The CSIL schema is the contract** — it's what makes the parallel task breakdown below
  safe, and what makes "multiple frontends" cheap later.
- Auth flow (longhouse pattern): SPA → `AuthService.begin-login` → backend `sign-request`
  via RP sidecar, sets nonce cookie → 302 to user's IDP → IDP → `GET /auth/callback` →
  `decrypt-token` → `verify-assertion` → `userinfo-fetch` → upsert `users` row → mint our
  own session cookie. Sessions are firepit's; linkkeys only verifies identity.

---

## 4. Data model (coredb baseline)

Conventions: ULID-as-uuid PKs (`generate_ulid()`), `created_at/updated_at timestamptz`,
soft-delete (`deleted_at`) on user content.

```
users            id, linkkeys_domain, linkkeys_user_id, handle, display_name,
                 kind ('human'|'system'), roles text[], created_at
                 UNIQUE (linkkeys_domain, linkkeys_user_id)

boards           id, slug UNIQUE, title, description, kind ('discussion'|'announce'),
                 created_by, archived_at, created_at

user_settings    user_id PK,
                 mention_policy ('subscribed'|'everyone'|'authorized'|'nobody')
                 DEFAULT 'subscribed',                   -- 'subscribed': mentions notify
                                                         -- only in contexts I subscribe to
                 notify_on_endorse bool DEFAULT true, updated_at
mention_grants   user_id, granted_user_id, created_at    -- who may ALWAYS mention-notify
                 PRIMARY KEY (user_id, granted_user_id)  -- me (extends 'subscribed' and
                                                         -- powers 'authorized')

friend_groups    id, owner_id FK, name, created_at        -- private to the owner
friend_group_members  group_id FK, member_user_id FK, created_at
                 PRIMARY KEY (group_id, member_user_id)

board_members    board_id FK, user_id FK, role ('maintainer'|'moderator'), created_at
                 PRIMARY KEY (board_id, user_id)          -- maintainer = project owner rep

trusted_domains  domain PK, added_by, created_at          -- instance-level, admin-managed;
                                                          -- endorsements from these
                                                          -- linkkeys domains earn reputation

posts            id, board_id FK, author_id FK, title, body_md,
                 origin ('user'|'github'|'system'), origin_ref jsonb,
                 comment_count int, last_activity_at, edited_at, deleted_at, created_at

comments         id, post_id FK, parent_comment_id FK NULL, author_id FK,
                 path ltree,            -- materialized path for one-query subtree fetch
                 body_md, origin, origin_ref jsonb, edited_at, deleted_at, created_at
                 INDEX GIST (path), INDEX (post_id, created_at)

revisions        id, target_type ('post'|'comment'), target_id, editor_id,
                 prev_title, prev_body_md, created_at         -- snapshot BEFORE each edit
                 INDEX (target_type, target_id, created_at)

endorsements     id, user_id, target_type ('post'|'comment'), target_id, created_at
                 UNIQUE (user_id, target_type, target_id)     -- retract = delete row

subscriptions    id, user_id, target_type ('board'|'post'|'comment'), target_id,
                 muted bool DEFAULT false, created_at
                 UNIQUE (user_id, target_type, target_id)
                 -- 'comment' = that comment's subtree; muted lets you carve holes
                 -- out of a broader subscription (sub board, mute one post)

read_marks       user_id, post_id, last_read_at,             -- watermark: auto-read on view
                 PRIMARY KEY (user_id, post_id)
unread_overrides user_id, target_type ('post'|'comment'), target_id, created_at
                 -- explicit "keep this unread" pins, beats the watermark

notifications    id, user_id, event ('new_post'|'new_comment'|'mention'|'github_event'),
                 subscription_id FK, actor_id, target_type, target_id, post_id,
                 read_at NULL, created_at
                 INDEX (user_id, read_at, created_at DESC)

github_mappings  id, board_id FK, repo ('owner/name'), events text[],
                 secret_ref, thread_mode ('post_per_issue'|'post_per_release'|...),
                 created_by, created_at

sessions         id, user_id, token_hash, expires_at, created_at
```

Design notes:

- **Threading**: `parent_comment_id` for structure + `ltree` path for retrieval. Whole
  thread = one indexed query, ordered depth-first — renders both reddit-style trees and
  flat mailing-list order.
- **Read model is hybrid**: a per-post watermark (`read_marks.last_read_at`, advanced when
  you view a thread) computes unread cheaply, and `unread_overrides` implements manual
  "mark as unread" on specific items. Manual "mark read" without viewing = advance the
  watermark / clear the override.
- **Notification fan-out** on write: collect subscribers of the board + the post + every
  ancestor comment (one `ltree` ancestor query), subtract mutes, dedupe per user, never
  notify the actor, insert `notifications` rows in the same transaction.
- **Mentions**: `@handle` parsed server-side on create/edit; each resolved user is
  notified only if their `mention_policy` allows it — default `subscribed` (the mention
  is inside a board/post/comment subtree they subscribe to, OR the author holds a
  `mention_grants` row), `everyone`, `authorized` (grants only), or `nobody`. Denied
  mentions still render as text — they just don't notify.
- **Endorser ordering** is computed per-viewer at read time: (1) endorsers in any of the
  viewer's friend groups, (2) then by reputation = maintainer/moderator on the post's
  board (highest), then count of endorsements received from users on `trusted_domains`.
  Reputation is a cached per-user score (recomputed on endorsement writes, or a
  materialized view refreshed periodically) — never used to rank content, only to order
  the names on an endorsement list. Maintainer/mod endorsements carry a role badge.
- **Endorsement notifications**: endorsing notifies the content author unless they've set
  `notify_on_endorse=false`.
- **Revisions**: `edit-post`/`edit-comment` snapshot the prior title/body into
  `revisions` in the same transaction. History is readable by anyone who can read the
  item (tombstoned content hides its history from non-mods).
- **GitHub content is first-class**: system posts/comments carry `origin='github'` and
  `origin_ref` (event payload pointer), authored by a per-integration system user. Humans
  reply to them like anything else — that's the point.

---

## 5. API surface (CSIL, `csil/firepit.csil` + `csil/types/*.csil`)

All ops CBOR over `POST /csil/v1/rpc` (HTTP carrier; WS carrier + CSIL-Events push is
milestone M4). Every mutating op has a declared error arm (`-> Result / FirepitError`).

```
service AuthService {
    begin-login:        BeginLoginRequest -> BeginLoginResponse / AuthError,
    logout:             Empty -> Empty,
    whoami:             Empty -> UserProfile / AuthError,
}
service BoardService {
    list-boards:        ListBoardsRequest -> BoardPage,
    get-board:          BoardSlug -> Board / NotFoundError,
    create-board:       CreateBoardRequest -> Board / FirepitError,       ;; admin-only
    update-board:       UpdateBoardRequest -> Board / FirepitError,       ;; admin
    archive-board:      BoardID -> Empty / FirepitError,                  ;; admin
    set-board-member:   SetBoardMemberRequest -> Empty / FirepitError,    ;; admin: assign
    remove-board-member: RemoveBoardMemberRequest -> Empty / FirepitError,;; maintainer/mod
}
service ThreadService {
    list-posts:         ListPostsRequest -> PostPage,          ;; board + cursor, by activity
    get-thread:         GetThreadRequest -> Thread,            ;; post + full comment tree
    create-post:        CreatePostRequest -> Post / FirepitError,
    create-comment:     CreateCommentRequest -> Comment / FirepitError,
    edit-post:          EditPostRequest -> Post / FirepitError,   ;; snapshots a revision
    edit-comment:       EditCommentRequest -> Comment / FirepitError,
    list-revisions:     TargetRef -> RevisionList,
    delete-post:        PostID -> Empty / FirepitError,        ;; soft; author or mod
    delete-comment:     CommentID -> Empty / FirepitError,
}
service EndorsementService {
    endorse:            EndorseRequest -> Endorsement / FirepitError,
    retract:            EndorseRequest -> Empty / FirepitError,
    list-endorsements:  TargetRef -> EndorsementList,   ;; names, not numbers; ordered
}                                                       ;; per-viewer (friends, then rep)
service SettingsService {
    get-settings:       Empty -> UserSettings,
    update-settings:    UpdateSettingsRequest -> UserSettings / FirepitError,
    list-mention-grants: Empty -> MentionGrantList,
    grant-mention:      UserID -> Empty / FirepitError,
    revoke-mention:     UserID -> Empty / FirepitError,
}
service SocialService {
    list-friend-groups:  Empty -> FriendGroupList,
    create-friend-group: CreateFriendGroupRequest -> FriendGroup / FirepitError,
    delete-friend-group: GroupID -> Empty / FirepitError,
    add-friend:          AddFriendRequest -> Empty / FirepitError,    ;; group + user
    remove-friend:       RemoveFriendRequest -> Empty / FirepitError,
}
service SubscriptionService {
    subscribe:          TargetRef -> Subscription / FirepitError,
    unsubscribe:        TargetRef -> Empty / FirepitError,
    set-muted:          SetMutedRequest -> Subscription / FirepitError,
    list-subscriptions: Empty -> SubscriptionList,
}
service ReadService {
    mark-read:          TargetRef -> Empty,                    ;; item or whole post
    mark-unread:        TargetRef -> Empty,
    unread-summary:     Empty -> UnreadSummary,                ;; per-board/post counts (poll)
}
service NotificationService {
    list-notifications: ListNotificationsRequest -> NotificationPage,
    mark-notification-read: NotificationIDs -> Empty,
    mark-all-read:      Empty -> Empty,
}
service IntegrationService {                                   ;; admin
    create-github-mapping: CreateMappingRequest -> GithubMapping / FirepitError,
    list-github-mappings:  Empty -> MappingList,
    delete-github-mapping: MappingID -> Empty / FirepitError,
    add-trusted-domain:    Domain -> Empty / FirepitError,
    remove-trusted-domain: Domain -> Empty / FirepitError,
    list-trusted-domains:  Empty -> DomainList,
}
```

HTTP-native (outside CSIL): `GET /auth/callback`, `POST /webhooks/github` (HMAC-SHA256,
reactorcide's verification pattern), `GET /healthz`.

---

## 6. Repo layout

```
firepit/
├── PLANDOC.md  CLAUDE.md  AGENTS.md  LICENSE (Apache-2.0)  .releaserc.json  tools.sh
├── csil/                    # firepit.csil + types/*.csil — THE contract
├── api/                     # go module github.com/catalystcommunity/firepit/api
│   ├── cmd/firepit-api/
│   └── internal/{config,transport,csil,csilservices,linkkeys,notify,github,store}
├── coredb/                  # go module …/firepit/coredb — goose embedded migrations
│   └── migrations/000001_baseline.sql …
├── webapp/                  # SolidJS + Vite + TS, npm, vitest
│   └── src/{gen,lib,components,pages}
├── clients/                 # generated: go/, typescript/ (more languages later)
├── helm_chart/              # api + linkkeys-rp sidecar + HTTPRoute
└── .reactorcide/jobs/       # test-go.yaml test-web.yaml release.yaml deploy.yaml
```

`tools.sh` verbs: `gen` (csilgen → api/internal/csil, webapp/src/gen, clients/), `test`,
`test-integration` (testcontainers Postgres), `lint`, `migrate`, `dev` (compose: postgres +
linkkeys-rp + api + vite), `build-images`.

---

## 7. Task breakdown (Sonnet-ready)

Four waves. Within a wave, every task is independently assignable and parallel — the CSIL
schema and the baseline migration are the shared contracts that make that true. Each task
lands as one PR with tests; conventional commits.

### Wave A — contracts and scaffolding (serialize A1 → A2/A3; A2 ∥ A3)

**A1. Repo scaffold** *(no deps)*
`tools.sh`, module init for `api/` + `coredb/`, webapp Vite scaffold, LICENSE, CLAUDE.md /
AGENTS.md (architecture-first, longhouse style), `.releaserc.json`, lint config,
`.reactorcide/jobs/test-go.yaml` + `test-web.yaml` stubs, docker-compose for `dev`.
*Accept:* `./tools.sh test` green on empty modules; CI stubs valid YAML.

**A2. CSIL schema v1 + codegen wiring** *(deps: A1)*
Write `csil/` per §5 with full request/response/error types, `@wire-id` ordinals,
`options { go_package, … }`. Wire `tools.sh gen`; check in generated Go server/types, TS
client, `clients/{go,typescript}`. Vendor/pin the csilgen invocation.
*Accept:* `csilgen validate` + `lint` clean; generated code compiles both sides; a
round-trip encode/decode test per service using `transports/go` + `transports/typescript`.

**A3. coredb baseline migration** *(deps: A1)*
`000001_baseline.sql` per §4 (goose, `generate_ulid()`, ltree extension, indexes,
constraints). Store package skeleton in `api/internal/store` (gorm models mirroring the
schema, no business logic).
*Accept:* goose up/down clean on testcontainers Postgres; model↔schema round-trip test.

### Wave B — backend (B1 first; then B2–B9 fully parallel)

**B1. Server skeleton + RPC dispatch** *(deps: A2, A3)*
`cmd/firepit-api`, config (`FIREPIT_*` env, app-utils-go), logrus, `/csil/v1/rpc`
envelope loop (`transports/go`: decode → route via generated `Route<Service>Channel` →
encode), session middleware (cookie → `users` row in ctx), `/healthz`, CORS, graceful
shutdown. Stub every service with `unimplemented`.
*Accept:* integration test drives a stubbed op end-to-end over HTTP in CBOR.

**B2. Auth + linkkeys RP** *(deps: B1)*
Port `longhouse/api/internal/linkkeys/` (HTTP + TCP transports, fingerprint pinning).
Implement `AuthService`, `/auth/callback`, user upsert, session mint/expiry, logout.
Config: `FIREPIT_LINKKEYS_*` mirroring longhouse's names.
*Accept:* integration test with a mocked RP sidecar covering happy path, bad nonce,
expired assertion.

**B3. BoardService** *(deps: B1)* CRUD + archive (creation admin-only), slug rules,
`board_members` maintainer/moderator assignment, role checks, pagination.
*Accept:* service tests incl. authz failures.

**B4. ThreadService** *(deps: B1)*
Posts + comments, ltree path maintenance, one-query tree fetch ordered depth-first,
cursor pagination by `last_activity_at`, edits with **revision snapshots** +
`list-revisions`, soft-delete (tombstone keeps thread shape),
`comment_count`/`last_activity_at` maintenance, markdown stored raw (render client-side).
Emits internal `ContentCreated`/`ContentEdited` events consumed by B7.
*Accept:* tests for deep trees (≥6 levels), tombstoned mid-tree replies, cursor
stability, revision snapshots on every edit path.

**B5. EndorsementService + reputation** *(deps: B1)* Endorse/retract/list; idempotent;
can't endorse own or deleted content. Per-viewer list ordering (viewer's friends first,
then reputation: board maintainer/mod > trusted-domain endorsements received), role
badges in the response, cached reputation score maintenance. Emits `Endorsed` event
consumed by B7. *Accept:* service tests incl. ordering matrix (friend vs maintainer vs
trusted-domain rep vs nobody) and reputation cache invalidation.

**B6. SubscriptionService + ReadService** *(deps: B1)*
Subscribe/unsubscribe/mute on board/post/comment; watermark + override read model per §4;
`unread-summary` aggregation query (needs to be one efficient query — this is the poll
endpoint). *Accept:* tests for override-beats-watermark, mute carve-outs, summary counts.

**B7. Notification fan-out + NotificationService** *(deps: B1; interfaces of B4/B5/B6)*
Fan-out per §4: subscriber resolution incl. ltree ancestors, dedupe, no self-notify,
mute-aware; **mention resolution** (`@handle` parse, `mention_policy` gate per §4);
**endorsement notifications** (`notify_on_endorse` gate); list/mark-read RPCs.
*Accept:* tests: board-sub gets new-post, comment-sub gets deep descendant reply, muted
post silences a board sub, actor never notified; mention-policy matrix (subscribed /
granted / authorized / nobody); endorse notify + opt-out.

**B9. SettingsService + SocialService** *(deps: B1)*
User settings (mention policy, notify-on-endorse), mention grants, friend groups CRUD +
membership, trusted-domains admin ops (IntegrationService side). Exposes the read
interfaces B5/B7 consume (friend membership, grants, trusted domains).
*Accept:* service tests incl. authz (groups are private to their owner).

**B8. GitHub ingestion** *(deps: B1; B4 store interfaces)*
`/webhooks/github` with per-mapping HMAC verification, `IntegrationService`, event →
content rules (v1: `release` → new post; `issues` opened → new post, closed → comment on
its post; `pull_request` merged → comment or post per mapping; `origin_ref` links back).
System user per mapping. Unknown/unmapped events: 200 and drop.
*Accept:* golden-payload tests from real webhook fixtures; bad-signature rejection.

### Wave C — webapp (C1 first; then C2–C4 parallel)

**C1. SPA foundation** *(deps: A2)*
Solid + `@solidjs/router`, CBOR transport wrapper around generated TS client
(longhouse pattern), session/auth pages (`/login`, `/auth/callback`), app shell/layout,
error + loading conventions, vitest setup. Mock-server mode so C2–C4 develop without B.
*Accept:* login flow against mock; typed client calls compile from generated code.

**C2. Boards + post lists** *(deps: C1)* Board index, board page with post list
(activity order, unread badges from `unread-summary` polling), new-post composer
(markdown editor + marked/dompurify preview), subscribe toggle on boards.
*Accept:* vitest component tests against mock client.

**C3. Thread view** *(deps: C1)* The core screen. Comment tree with collapse, flat
"mailing-list" toggle, reply-at-any-depth composer with `@handle` autocomplete,
endorsements (inline named list in server order, role badges, endorse/retract),
edit/delete own content with revision-history view, tombstones, per-item mark-unread,
auto-watermark on view, GitHub-origin styling with backlinks, permalinks to comments.
*Accept:* component tests: deep tree render, collapse state, endorsement flow, revision
history view.

**C4. Notifications, subscriptions + settings UI** *(deps: C1)* Bell + unread count
(poll), notification inbox (click-through marks read), mark-all-read, subscriptions
management page (incl. mutes), settings page (mention policy, mention grants,
notify-on-endorse), friend-groups management, read/unread affordances consistent
with C3. *Accept:* component tests against mock client.

### Wave D — ship it (parallel with late B/C)

**D1. Helm chart + deploy** *(deps: A1; finalize after B1)*
Chart modeled on longhouse/reactorcide: api Deployment, linkkeys-rp sidecar
(`ENABLE_RP_ENDPOINTS=true`), `HTTPRoute` for `firepit.catalystsquad.com`. **Postgres is
a Zalando postgres-operator CR** (created alongside, not owned by this chart); the chart
consumes a `DB_URI` secret. Build/deploy secrets via reactorcide vault (`${secret:…}`).
`.reactorcide/jobs/release.yaml` (BuildKit → `containers.catalystsquad.com`) +
`deploy.yaml` (`helm upgrade` on tag). *Accept:* `helm template` + kind smoke test via
`action-kind-test` conventions; dry-run deploy job local via `reactorcide run-local`.

**D2. End-to-end integration suite** *(deps: B2–B9, C1)*
testcontainers Postgres + real api binary + generated Go client driving full scenarios:
login → create board → post → deep reply → edit + revision history → endorsement (with
ordering + author notification) → mention across policies → subscription → notification
→ read marks → GitHub webhook → system post appears. *Accept:* suite green in
`.reactorcide/jobs/test-go.yaml`.

**D3. Seed data, docs, dogfood setup** *(deps: D1, D2)*
Seed script (boards for existing catalystcommunity projects), GitHub mappings for 2–3
real repos, `docs/OPERATING.md`, README. *Accept:* fresh `./tools.sh dev` gives a
browsable seeded forum.

**Critical path:** A1 → A2 → B1 → B4 → B7 → D2. Peak parallelism: 8 agents (B2–B9), then
3 (C2–C4). Everything else hangs off the CSIL contract, so schema changes after Wave A
are PRs against `csil/` first, regen, then implementations.

---

## 8. Milestones

- **M1 — Skeleton talks** (A + B1 + C1 + D1): deployed at firepit.catalystsquad.com,
  login works, stub API answers.
- **M2 — Forum** (B3–B5, C2–C3): boards, threaded posts, revisions, endorsements
  (ordering degrades gracefully until B9 lands). Usable read/write.
- **M3 — Signal** (B6–B7, B8–B9, C4, D2–D3): subscriptions, read state, notifications,
  mentions, settings/friends, GitHub events flowing in. **This is "replace GitHub
  Discussions" day.**
- **M4 — Later**: CSIL-Events/WebSocket live push (schema already carries `<-` ops),
  email digests, search, more frontends from `clients/<lang>`, moderation tooling,
  more integrations (reactorcide job results, release announcements from semantic-release).

---

## 9. Decisions (settled 2026-07-03)

1. **Vocabulary** — plain `boards/posts/comments` in schema, API, and UI.
2. **Board creation** — admin-only in v1; maintainer/moderator roles assigned per board.
3. **Endorsements** — retractable; endorsing your own content blocked; endorser names
   always inline; maintainer/mod endorsements badged; author notified (opt-out via
   `notify_on_endorse`); endorser list ordered per-viewer (friends first, then
   reputation from board maintainer status / endorsements received from trusted
   linkkeys domains). Never ranks content.
4. **Mentions** — in v1. Default policy `subscribed` (mentions notify only in contexts
   you subscribe to); `mention_grants` extends that per-person; `everyone`/`authorized`/
   `nobody` policies available.
5. **Edit history** — editable with full revision history (`revisions` table,
   `list-revisions` op) from day one.
6. **GitHub ingestion** — plain per-repo webhooks with HMAC secrets in v1; GitHub App
   deferred until we want write-back.
7. **Postgres** — provisioned as a Zalando postgres-operator CR; the chart consumes a
   `DB_URI` secret and never owns the database.
