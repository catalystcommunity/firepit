# Firepit

A dev coordination forum for open source projects — threaded discussion
(mailing-list style, arbitrary depth), endorsements instead of upvotes,
subscriptions/notifications, and GitHub event ingestion so project activity
flows into the same threads humans use. Full product spec, data model, API
surface, and task breakdown: **PLANDOC.md** (read that first for anything
beyond "how do I build/run this").

**Status: built.** Waves A–D are complete. The backend, web application,
deployment files, seed command, and test suites are implemented. Categories
and shared threads are also implemented. `PLANDOC.md` is the design record.

## Architecture

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

- **One Go binary** (`firepit-api`). Notification fan-out runs in-process on
  write — a forum's write rate doesn't justify a queue.
- **CSIL is the contract.** `csil/` holds the schema (`csil/firepit.csil` +
  `csil/types/*.csil`); every client (web, and later TUI/mobile/bots) is a
  generated consumer of it, never a bespoke REST surface. All ops are CBOR
  over `POST /csil/v1/rpc`; HTTP-native routes outside CSIL are only
  `GET /auth/callback`, `POST /webhooks/github`, `GET /healthz`.
- **Auth**: linkkeys (`user@domain` identity) via a linkkeys sidecar running
  in RP mode. The sidecar only verifies identity — sessions are firepit's
  own (cookie, minted after `verify-assertion` + `userinfo-fetch`). Flow
  ported from `longhouse`'s `api/internal/linkkeys/` pattern.
- **Data model** (coredb baseline, see PLANDOC.md §4): ULID PKs
  (`generate_ulid()`), `timestamptz` created/updated, soft-delete on user
  content. Core entities: `users`, `boards`, `posts`, `comments` (threaded
  via `parent_comment_id` + an `ltree` materialized path for one-query
  subtree fetch), `revisions` (full edit history, snapshotted before every
  edit), `endorsements` (retractable, per-viewer ordered, never ranks
  content), `subscriptions`/`read_marks`/`unread_overrides` (hybrid
  watermark + manual-pin unread model), `notifications` (fan-out target),
  `github_mappings` (webhook → board routing).

## Repo layout

```
firepit/
├── PLANDOC.md  CLAUDE.md  AGENTS.md  LICENSE  .releaserc.json  tools.sh
├── csil/                    # firepit.csil + types/*.csil — THE contract
├── api/                     # go module github.com/catalystcommunity/firepit/api
│   ├── cmd/firepit-api/      # binary entrypoint
│   └── internal/{config,transport,csil,csilservices,linkkeys,notify,github,store}
├── coredb/                  # go module …/firepit/coredb — goose embedded migrations
│   └── migrations/           # numbered goose migrations
├── webapp/                  # SolidJS + Vite + TS, npm, vitest
│   └── src/{gen,lib,components,pages}
├── clients/                 # generated: go/, typescript/ (more languages later)
├── helm_chart/              # api + linkkeys-rp sidecar + HTTPRoute (D1)
└── .reactorcide/            # trusted plugins, workflow DAGs, jobs, tests, grants
```

Three independent Go/npm modules (`api`, `coredb`, `webapp`) plus
schema/generated-code dirs (`csil`, `clients`) — the CSIL schema is what
makes parallel work across them safe (change the schema, regen, then
implementations catch up).

## `tools.sh`

Portable bash task runner — the only supported entrypoint for build/test/dev
tasks in this repo (no Makefile). Run `./tools.sh` with no args for the verb
list. Verbs:

| Verb | Does |
|---|---|
| `gen` | Regenerate CSIL-derived code in `api/internal/csil`, `webapp/src/gen`, and `clients/`. Run it after a `csil/*.csil` change. |
| `test` | `go test ./...` for `api` and `coredb`, then `npm test` for `webapp`. Must stay green. |
| `test-go` | Run the `api` and `coredb` Go tests. |
| `test-web` | Run the web application tests. |
| `test-integration` | testcontainers-backed Postgres integration suite: `go test -tags=integration ./...` across every `api` package (store, csilservices, github, server). Requires `docker`. |
| `lint` | Run `go vet` for `api` and `coredb`, then ESLint for `webapp`. |
| `lint-go` | Run `go vet` for `api` and `coredb`. |
| `lint-web` | Run ESLint for `webapp`. |
| `migrate` | Run `up`, `down`, or `status` against `$DB_URI`. The default verb is `up`. |
| `seed` | Seed boards and optional demo, admin, trust-domain, and GitHub mapping data. |
| `dev` | Start Postgres, the API, and the web application with Docker Compose. |
| `build-images` | Build the API and web application container images. |

Run `./tools.sh help` for command flags and requirements.

## Local dev

```sh
./tools.sh dev     # postgres + api + webapp via docker compose
./tools.sh test    # go test (api, coredb) + npm test (webapp)
./tools.sh lint    # go vet + eslint
```

`docker-compose.yaml` defines the local stack. The `linkkeys-rp` service is
disabled until you configure local linkkeys settings. Anonymous access does
not require that service.

## Conventions

- **CSIL-first.** `csil/` is the source of truth for the Go service interfaces and
  every `clients/<lang>` package, plus `webapp/src/gen`. Don't hand-edit
  generated files — change the schema and regen.
- **PostgreSQL only.** goose for migrations (`coredb`), gorm-on-pgx for the
  ORM layer (`api/internal/store`).
- **Multi-module Go project**: `api` and `coredb` are separate Go modules
  (`github.com/catalystcommunity/firepit/api`,
  `github.com/catalystcommunity/firepit/coredb`).
- **Module path convention**: `github.com/catalystcommunity/firepit/<module>`.
- **Conventional commits** + semantic-release-style versioning
  (`.releaserc.json`, `version/VERSION.txt`).
- **`tools.sh`**, not Make, is the task runner.
- Apache-2.0 licensed.
- See PLANDOC.md §2 for what's ported/reused from `longhouse`, `csilgen`,
  `linkkeys`, `reactorcide`, `app-utils-go`, and `zest`.
