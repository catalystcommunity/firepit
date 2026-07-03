# Operating firepit

This is the operator-facing guide to running a firepit deployment: required
configuration, boot/migration behavior, seeding + admin bootstrap, linkkeys
auth setup, GitHub webhook wiring, the notification model, and backup
scope. For *what firepit is* and local dev quickstart, see the repo
[README](../README.md). For the product spec, data model, and API surface,
see [PLANDOC.md](../PLANDOC.md). For the Kubernetes deploy runbook (Helm
chart, secrets, Gateway routes), see
[helm_chart/README.md](../helm_chart/README.md) — this document doesn't
repeat that content, only links to it where it's relevant.

Postgres is the **only** stateful component firepit owns; everything below
follows from that.

---

## 1. Required configuration (`FIREPIT_*` env vars)

firepit-api reads its entire configuration from environment variables at
boot (`api/internal/config/config.go`'s `Load`) — there is no config file.
Every var has a dev-friendly default; a real deployment should set at least
the ones marked **required**.

| Var | Default | Notes |
|---|---|---|
| `FIREPIT_PORT` | `8080` | HTTP port the api listens on. |
| `FIREPIT_DB_URI` | `postgresql://firepit:devpass123@localhost:5432/firepit_db?sslmode=disable` | **Required** in any real deployment. `postgresql://user:pass@host:5432/dbname` — see helm_chart/README.md's "Database" section for how the chart wires this from a Zalando postgres-operator credentials Secret. |
| `FIREPIT_CORS_ORIGINS` | `*` | Comma-separated allow-list. Narrow this to the webapp's real origin(s) in any real deployment — `*` is a dev convenience. |
| `FIREPIT_MIGRATE_ON_BOOT` | `true` | See §2 below. |
| `FIREPIT_LINKKEYS_DOMAIN` | `""` | firepit's own relying-party identity. Falls back to `FIREPIT_LINKKEYS_IDP_DOMAIN` if unset (`config.Config.LinkkeysRPDomain()`). |
| `FIREPIT_LINKKEYS_URL` | `""` | Reserved for future use alongside the transport-specific vars below. |
| `FIREPIT_LINKKEYS_TRANSPORT` | `http` | `http` (legacy JSON PKI sidecar) or `tcp` (canonical CSIL-RPC over TCP, additionally gets richer `userinfo-fetch`). |
| `FIREPIT_LINKKEYS_PKI_URL` | `""` | **Required** for the `http` transport — the RP sidecar's base URL. |
| `FIREPIT_LINKKEYS_PKI_API_KEY` | `""` | **Required** — see §4 below for how this is minted. |
| `FIREPIT_LINKKEYS_PKI_ALLOW_INVALID_CERTS` | `false` | Dev-only escape hatch for a self-signed RP. |
| `FIREPIT_LINKKEYS_TCP_ADDR` | `""` | **Required** for the `tcp` transport. |
| `FIREPIT_LINKKEYS_TCP_FINGERPRINTS` | `""` | Comma-separated pinned cert fingerprints for the `tcp` transport. |
| `FIREPIT_LINKKEYS_TCP_ALLOW_INSECURE` | `false` | Dev-only escape hatch for the `tcp` transport. |
| `FIREPIT_LINKKEYS_IDP_DOMAIN` | `""` | **Required** — the single linkkeys identity domain this instance's login flow accepts (v1 is single-IDP). First deployment: `linkkeys.todandlorna.com` (PLANDOC.md §1). |
| `FIREPIT_LINKKEYS_IDP_URL` | `""` | **Required** — that IDP's browser-facing URL (`begin-login` redirects here). |
| `FIREPIT_APP_CALLBACK_URL` | `""` | **Required** — the absolute URL the IDP redirects back to: `https://<your-host>/auth/callback`. NOT a SPA route. |
| `FIREPIT_SESSION_NONCE_SECRET` | *(random per-process)* | HMACs the login nonce. **Set this explicitly** for any deployment with more than one replica or that restarts often — an unset value works for a single dev process but a value that changes between two replicas (or across a restart) makes in-flight logins fail nonce verification. Not a secret an attacker can use to forge sessions on its own (see `api/internal/config/config.go`'s doc comment), but treat it as sensitive anyway. |
| `FIREPIT_POST_LOGIN_REDIRECT_URL` | `/` | Where `GET /auth/callback` 302s the browser after minting a session cookie. |

GitHub webhook secrets (`FIREPIT_GH_SECRET_*`) are per-mapping, not listed
here — see §5.

---

## 2. Migrate-on-boot semantics

`FIREPIT_MIGRATE_ON_BOOT` (default `true`) controls whether firepit-api
runs coredb's goose migrations itself, in-process, before it starts serving
traffic (`api/cmd/firepit-api/main.go`'s `run`):

```go
if cfg.MigrateOnBoot {
    coredb.Up(cfg.DBURI)   // blocking; boot fails if this fails
}
```

This is convenient for a single-replica dev/small deployment (one moving
part: start the binary, get a migrated database) but has the usual
multi-replica caveat: if you ever run more than one firepit-api replica,
every replica attempts the migration on every boot. goose's migrations are
individually transactional and idempotent-by-version (a migration that's
already been applied is a no-op), so concurrent replicas racing to migrate
is safe, but it's still wasted work and briefly widens the window where a
schema change is "in progress" during a rolling restart. For anything
beyond a single replica, prefer running migrations as an explicit release
step (`./tools.sh migrate up` against the production `DB_URI`, or the
equivalent in your deploy pipeline) and set `FIREPIT_MIGRATE_ON_BOOT=false`
so the api process itself never touches schema.

`./tools.sh migrate [up|down|status]` (backed by `coredb/cmd/migrate`) is
the same goose wrapper the api uses internally, runnable standalone against
`$DB_URI` (falls back to the docker-compose default). Use this for the
explicit-migration-step deployment shape above, or to inspect/roll back a
schema outside the api process entirely.

---

## 3. Seeding and admin bootstrap

`./tools.sh seed [flags]` runs `api/cmd/firepit-seed`, an idempotent data
seeder — safe to run repeatedly against the same database; every step
upserts (by slug, by linkkeys identity, by repo) rather than blindly
inserting, so re-running it never creates duplicates or errors. It does
**not** run migrations itself — run `./tools.sh migrate up` first.

It reads `FIREPIT_DB_URI` (falling back to `DB_URI`, then the same
docker-compose default `./tools.sh migrate` uses), so it's usable as a
standalone step in any deployment, not just local dev.

### What it seeds unconditionally

Every run — no flags required — upserts:

- **Project boards**: one discussion board each for `firepit`, `csilgen`,
  `linkkeys`, `reactorcide`, and `longhouse` (this org's own repos,
  PLANDOC.md §2's "prior art" table), titled/described from each project's
  actual purpose. Plus a general-purpose **`general`** discussion board and
  an announce-kind **`announcements`** board.
- A synthetic **seed-bot system user** (`kind='system'`, linkkeys identity
  `seed.firepit.system:firepit-seed-bot`) that attributes `created_by` on
  rows this tool creates. It has no admin role and can't be logged in as —
  it's attribution plumbing, not an account.

### Admin bootstrap — `--admin domain:user_id` (repeatable)

**This is the documented path for creating the first instance admin.**
Every admin-only CSIL op (`BoardService.create-board`,
`IntegrationService.*`, ...) requires an existing admin to call it
(`csilservices.requireAdmin`) — there is no in-product way to grant the
first one, by design (an unauthenticated "make me admin" op would be a
standing vulnerability). Run this once, against the identity of whoever
should hold the role:

```sh
./tools.sh seed --admin todandlorna.com:tod
```

`domain:user_id` is the linkkeys identity (the same `(linkkeys_domain,
linkkeys_user_id)` pair the login flow upserts by) — **not** an email
address or a display name. Splitting happens on the *first* colon, so a
`user_id` containing its own colons round-trips correctly. This:

1. Upserts a `users` row for that identity if none exists yet (the person
   doesn't need to have ever logged in first — the row is created ahead of
   time, exactly as if they had).
2. Grants the `admin` role (`users.roles`) if it isn't already present.
   Already-admin is a no-op, not an error — safe to re-run, and safe to use
   for granting a *second* or *third* admin later (each `--admin` is
   independent; pass it multiple times in one invocation to bootstrap
   several at once).

**Caveat on identity fields**: `store.UpsertUser`'s documented behavior is
that an *existing* user's `handle`/`display_name` are sticky — never
silently overwritten by a later real login. Since this CLI path has no
linkkeys claims to draw from, it invents a placeholder handle (slugified
from `user_id`) and display name (title-cased from the same). That
placeholder is what the admin will see everywhere in the UI **until** you
give them a rename op (there is none in v1) — cosmetic, but worth knowing
before you bootstrap someone under a `user_id` that doesn't read well as a
handle.

### `--demo`

Additionally seeds four demo users (`alice`, `bob`, `carol`, `dave` under
the fake identity domain `demo.firepit.local` — nobody can ever actually
log in as them) and three threads with nested comments, endorsements, and
subscriptions across the `general`/`firepit`/`announcements` boards, so a
fresh environment is immediately browsable. **Skip this flag in any real
deployment** — it exists purely so `./tools.sh dev` (or a from-scratch
`migrate && seed --demo`) gives you something to look at.

Idempotency here works a little differently than the upsert-by-key
approach above: since demo posts/comments have no natural unique key of
their own, the seeder checks for one canary post (the "Welcome to
Firepit" post on the `general` board) before creating anything, and skips
the entire demo-content block if it's already there. Demo *users* are
still upserted unconditionally (harmless either way).

### `--github-mappings`

Additionally seeds `github_mappings` rows for three real
catalystcommunity repos (`catalystcommunity/firepit`,
`catalystcommunity/csilgen`, `catalystcommunity/reactorcide`), routed to
their matching project boards. **Off by default** — a prod run of
`firepit-seed` should not silently opt an instance into accepting webhook
deliveries for repos nobody has actually wired secrets for yet. This only
writes firepit's own routing rows; it does not set the
`FIREPIT_GH_SECRET_*` env vars or touch anything on GitHub's side — see §5
for the rest of that setup.

### Verifying it worked

```sh
docker compose up -d postgres
./tools.sh migrate up
./tools.sh seed --demo
./tools.sh seed --demo   # run again: every line should read created=false / role_granted=false
```

A second run logging `created=false` (or, for admins, `role_granted=false`)
across the board, with row counts unchanged, is the idempotency guarantee
holding.

---

## 4. linkkeys RP requirements and API-key minting

firepit-api never talks to an IDP directly — it authenticates to a
**linkkeys server running in RP (relying-party) mode**, over a Bearer
API key, and that RP does the actual `sign-request` / `decrypt-token` /
`verify-assertion` / `userinfo-fetch` work (PLANDOC.md §3's auth flow
diagram). Provisioning that RP instance, minting its API key, and wiring
the chart's secrets around it is a Kubernetes/Helm concern owned by
**helm_chart/README.md** — specifically its "Required secrets" section
(#2: linkkeys RP API key, #3: RP domain key passphrase) and "First-deploy
runbook" steps 2–5 (bring the chart up with no API key yet → `linkkeys
domain init` → `linkkeys user create ... --api-key` inside the RP pod →
feed the minted key back in as `linkkeys.pki.apiKey` → re-deploy). Follow
that runbook rather than duplicating it here; the short version is:

1. The RP needs its own domain key initialized (`linkkeys domain init`)
   before it can do anything.
2. firepit's own Bearer API key against the RP is minted **by the RP
   itself** (`linkkeys user create firepit-api "Firepit API" --api-key`) —
   there's no way to generate it ahead of time, which is why first deploy
   is a two-step dance (deploy without it, mint it, redeploy with it).
3. Once minted, that key becomes `FIREPIT_LINKKEYS_PKI_API_KEY` (§1).

If you're running firepit outside the Helm chart (bare binary + your own
linkkeys RP process), the equivalent manual steps are the same three
`linkkeys` CLI invocations against whatever RP instance
`FIREPIT_LINKKEYS_PKI_URL` (or the `tcp` transport's
`FIREPIT_LINKKEYS_TCP_ADDR`) points at.

**Local dev note**: `docker-compose.yaml`'s `linkkeys-rp` service is still
a commented placeholder (see that file and CLAUDE.md — it gets wired up
once task D1's image exists), so **real linkkeys login does not work
against a plain `./tools.sh dev` today.** What *does* work without an RP:
every anonymous-read path (browsing boards, reading threads, reading
endorsement lists) — that's exactly what this task's own verification
exercised (see the repo README's quickstart). Building/testing/browsing a
seeded forum has never required a working login; only writing content,
subscribing, or hitting any admin op does.

---

## 5. GitHub webhook setup

Per PLANDOC.md §9 decision 6: v1 GitHub ingestion is **plain per-repo
webhooks with HMAC secrets** (no GitHub App). Each repo you want flowing
into a board needs:

1. A `github_mappings` row (`IntegrationService.create-github-mapping`, or
   `./tools.sh seed --github-mappings` for the three repos it knows about —
   see §3). This is `repo` ("owner/name") → `board_id`, which
   event types to accept, and a `thread_mode`.
2. An environment variable on firepit-api named whatever that mapping's
   `secret_ref` says — **not** the secret's value; `secret_ref` is only the
   *name* of an env var firepit-api reads at verification time
   (`api/internal/github`'s package doc comment, "Secret resolution").
   Storing only the name (never the value) in the database means a
   database dump never leaks a webhook secret. Convention:
   `FIREPIT_GH_SECRET_<MAPPING_ID_OR_NAME>`, e.g.
   `FIREPIT_GH_SECRET_CATALYSTCOMMUNITY_FIREPIT`.
3. A webhook configured on the GitHub repo itself (Settings → Webhooks →
   Add webhook):
   - **Payload URL**: `https://<your-host>/webhooks/github` — one fixed
     URL for every repo; the receiver resolves which mapping applies from
     the payload's own `repository.full_name`, not the URL.
   - **Content type**: `application/json`.
   - **Secret**: the exact same value you put in the `FIREPIT_GH_SECRET_*`
     env var in step 2.
   - **Which events**: "Let me select individual events" →
     **Releases**, **Issues**, **Pull requests**. These are the only three
     event types firepit understands in v1 (anything else GitHub sends is
     accepted with `200` and dropped, so subscribing to more is harmless
     but pointless) — specifically `release` (published), `issues`
     (opened/closed), and `pull_request` (closed with `merged: true`). See
     `api/internal/github/doc.go`'s "Event rules v1" section for the exact
     post-vs-comment behavior each one triggers and how `thread_mode`
     changes it for merged PRs.

Every webhook delivery except a bad/missing signature gets HTTP `200`
(unmapped repo, unsubscribed event type, an already-processed redelivery)
— GitHub retries and eventually disables a hook on repeated non-2xx
responses, and "we understood you but chose not to act" isn't a delivery
failure. A `401` means the signature didn't verify: check the secret
value matches on both sides and that the `FIREPIT_GH_SECRET_*` env var is
actually set (an unresolvable `secret_ref` fails closed as `401`, never as
"skip verification").

---

## 6. Notification model summary

See PLANDOC.md §4 for the full design; this is the operator-facing
summary of *what generates a notification and who controls it* (useful
context when a user asks "why didn't I get notified" or "why did I get
notified").

**What generates notifications** (`api/internal/notify`'s fan-out, run
in-process in the same transaction as the write that caused it):

- A **new post or comment** notifies every subscriber of the board, the
  post, or (for a comment) any ancestor comment in its thread — whichever
  is the *most specific* match wins for a user who matches more than one
  (comment > post > board), and that match's own `muted` flag governs
  whether they're actually notified. This is how "subscribe to a board,
  mute one noisy post" works: the post-level mute overrides the
  board-level subscribe for that post specifically.
- An **@mention** notifies the mentioned user, gated by their
  `mention_policy` (`user_settings.mention_policy`, default
  **`subscribed`**): notified only if the mention is inside a board/post/
  comment they already subscribe to, OR the author holds a
  `mention_grants` row from them. `everyone` and `nobody` are the two
  blanket policies; `authorized` means grants-only (no subscribed-context
  carve-out). A denied mention still renders as text — it just doesn't
  notify.
- An **endorsement** notifies the endorsed content's author, unless
  they've set `notify_on_endorse=false`.
- The **actor is never notified of their own action** (self-notifications
  are filtered out at the SQL level, not just suppressed client-side).
- Edits do **not** re-notify subscribers (only mentions are re-scanned on
  an edit, in case a new `@handle` was added).

**Read/unread** is a hybrid model: `read_marks` is a per-post watermark
(advanced automatically on viewing a thread, or manually via mark-read),
and `unread_overrides` lets a user pin a specific post/comment back to
unread regardless of the watermark ("keep this unread" beats the
watermark). There is no email/push digest in v1 — notifications and
unread counts are poll-only, in-app.

---

## 7. Backup

Postgres is the **only** stateful component firepit has. There is no
uploaded-file storage (v1 has no image/file uploads — markdown + links
only), no search index, no queue, nothing else to back up. Back up the
database using whatever mechanism your Postgres provisioning already gives
you:

- **Zalando postgres-operator** deployments (the provisioning path
  helm_chart/README.md assumes, per PLANDOC.md §9 decision 7): the
  operator supports WAL-E/WAL-G continuous archiving and periodic base
  backups to object storage — configure that at the `postgresql` CR level;
  this repo's Helm chart never manages Postgres itself, so backup
  configuration lives entirely in your cluster's Postgres provisioning, not
  here.
- **Anything else** (bare Postgres, a managed cloud database): use its
  native backup/PITR mechanism (`pg_dump`/`pg_basebackup`/managed
  snapshots). No firepit-specific consideration applies beyond "back up the
  one database" — restoring a snapshot restores the entire product state
  (boards, posts, comments, revisions, endorsements, subscriptions,
  notifications, sessions).

Sessions (`sessions` table) restore along with everything else on a
snapshot restore; a restore to an older snapshot silently invalidates any
session token that didn't exist yet in that snapshot (normal,
inconsequential — the affected users just log in again) but also
*revives* any session that was logged out or expired after the snapshot
was taken. Not a security concern worth extra tooling around (session TTL
default is 30 days,`store.DefaultSessionTTL`), but worth knowing if a
restore is ever done for reasons other than disaster recovery.

---

## Appendix: application error codes

Every CSIL op with a declared `ServiceError` arm returns one of these
fixed codes (`api/internal/csilservices/errors.go`) — distinct from CSIL-RPC's
own transport-level status, which never carries application semantics.

| Code | Name | Meaning |
|---|---|---|
| 1 | `Unimplemented` | The op has no real implementation yet (should not appear in a released build). |
| 2 | `Validation` | Caller input failed a product-level check (`Field` names which one). |
| 3 | `Unauthenticated` | No active session, and the op requires one. |
| 4 | `Forbidden` | A session exists but lacks the required role/ownership. |
| 5 | `NotFound` | The resource doesn't exist — or the caller isn't permitted to know whether it does; both cases read identically, deliberately (never leak existence via a different code). |
| 6 | `Conflict` | The request is well-formed but contradicts existing state (e.g. a duplicate board slug, an already-configured repo mapping). |
| 7 | `Internal` | Everything else (a genuine bug or infrastructure failure). |
