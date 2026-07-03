# Firepit

A dev coordination forum for open source projects. Threaded discussion in
the spirit of old mailing lists and reddit threads, built for async
communication across an open source community. No upvotes or karma —
instead, **endorsements**: publicly putting your name to a post or
comment. Machine actors (GitHub events, CI, releases) post into the same
threads humans use, so the forum replaces the social layer of GitHub:
discussion of projects, issues, releases, and operating/supporting those
projects.

Login is via [linkkeys](https://github.com/catalystcommunity/linkkeys)
(`user@domain` identity); anonymous read access works on every public
board with no login at all. First deployment target:
`firepit.catalystsquad.com`.

## Status

**Waves A–D implemented.** Every Wave B backend service (auth, boards,
threads, endorsements, settings/social, subscriptions/read, notifications,
GitHub ingestion) and the Wave C SolidJS webapp are real, tested
implementations. The Helm chart, end-to-end integration suite, and this
seed/docs/dogfood setup (Wave D) are in place. See
[PLANDOC.md](PLANDOC.md) §7–§8 for the full task breakdown and milestone
definitions if you want the blow-by-blow.

## Quickstart

```sh
# 1. Bring up Postgres and migrate it
docker compose up -d postgres
./tools.sh migrate up

# 2. Seed some data — real project boards always; --demo adds browsable
#    sample threads; --admin bootstraps your own instance-admin account
#    (see docs/OPERATING.md's "Admin bootstrap" section)
./tools.sh seed --demo
./tools.sh seed --admin yourdomain.com:your-user-id

# 3. Run everything
./tools.sh dev     # docker compose: postgres + api (+ webapp/linkkeys-rp as their images land)
./tools.sh test    # go test (api, coredb) + npm test (webapp)
./tools.sh lint    # go vet (api, coredb) + eslint (webapp)
```

Anonymous read works out of the box against a freshly seeded instance —
boards render and threads open with no login required. Real linkkeys login
needs a working RP sidecar wired up (not yet part of local
`docker-compose.yaml` — see [docs/OPERATING.md](docs/OPERATING.md) §4),
so for local development the webapp's mock-transport mode
(`VITE_FIREPIT_MOCK=1`) is the fastest way to exercise anything
login-gated; `npm run dev` without that flag talks to the real api over
the same CBOR CSIL-RPC wire.

Run `./tools.sh` with no arguments for the full verb list (`gen`, `test`,
`test-integration`, `lint`, `migrate`, `seed`, `dev`, `build-images`).

## Architecture, one page

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

One Go binary (`firepit-api`), one Postgres database, a
[CSIL](https://github.com/catalystcommunity/csilgen) schema
(`csil/firepit.csil`) as the single source of truth for the API — every
client (the SolidJS SPA today, anything else later) is a generated
consumer of it, never a bespoke REST surface.

For the full architecture, data model, and API surface: **[PLANDOC.md](PLANDOC.md)**
(the product spec and task plan this was built from) and
**[CLAUDE.md](CLAUDE.md)** (the maintained repo map/conventions doc — read
that first for "how do I build/run this," PLANDOC for "why does it work
this way"). For running a real deployment (env vars, migrations, seeding,
linkkeys/GitHub setup, notification semantics, backups): **[docs/OPERATING.md](docs/OPERATING.md)**.
For the Kubernetes deploy runbook itself: **[helm_chart/README.md](helm_chart/README.md)**.

## License

[Apache License 2.0](LICENSE).
