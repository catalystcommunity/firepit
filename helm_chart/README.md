# Firepit Helm chart

Deploys firepit-api, the webapp (SolidJS SPA behind nginx), and — optionally
— a linkkeys server instance running in RP (relying-party) mode. See
PLANDOC.md §3/§6/§9 for the architecture and decisions this chart
implements, and CLAUDE.md for the repo's overall layout.

This chart deliberately does **not** manage Postgres. See "Database" below.

## Components

| Component | What it is | Toggle |
|---|---|---|
| `api` | `firepit-api` Go binary — CSIL-RPC, `/auth/callback`, `/webhooks/github`, `/healthz` | always on |
| `webapp` | SolidJS SPA served by nginx (`webapp/Dockerfile`) | always on |
| `linkkeysRp` | A `linkkeys` server instance in RP mode (sign-request/decrypt-token/verify-assertion/userinfo) that `api` authenticates to over a Bearer API key | `linkkeysRp.enabled` (default `true`); set `false` to point at an external RP instead |

## Required secrets

This chart creates **some** Secrets itself (from values, at `helm
upgrade` time — never checked into git) and **references** others that it
never creates, owns, or rotates. Know which is which before deploying:

### 1. Database — `database.existingSecretName` (chart does NOT create this)

Per PLANDOC.md §9 decision 7, Postgres is provisioned as a **Zalando
postgres-operator** (`postgresql.acid.zalan.do`) custom resource applied
**alongside** this Helm release, not templated by it. The operator creates
its own credentials Secret in the format
`<user>.<cluster-name>.credentials.postgresql.acid.zalan.do` with `username`
and `password` keys — not a ready-made connection string. Ops must
assemble a single-key Secret from that (see "First deploy" below) named
whatever `database.existingSecretName` points at (default:
`firepit-db-uri`), with one key (`database.existingSecretKey`, default
`DB_URI`) holding the full `postgresql://user:pass@host:5432/dbname` URI.
The chart mounts it directly via `secretKeyRef` as `FIREPIT_DB_URI` on the
api container — no fallback, no chart-owned Postgres.

### 2. linkkeys RP API key — `linkkeys.pki.apiKey`

There's no way to generate this ahead of time: it's minted **by the RP
itself** the first time you run `linkkeys user create ... --api-key`
against it (see the runbook below). Once minted, feed it back in as
`linkkeys.pki.apiKey` via a values overlay or `--set-string` (never commit
the real value) — the chart turns it into a key
(`FIREPIT_LINKKEYS_PKI_API_KEY`) on a Secret it creates
(`<release>-firepit-api`). In CI this flows in via reactorcide's
`${secret:catalystcommunity/firepit:linkkeys_api_key}` (see
`.reactorcide/jobs/deploy.yaml`).

### 3. linkkeys RP domain key passphrase — `linkkeysRp.domainKeyPassphrase`

Encrypts the RP's own on-disk Ed25519 domain key material (persisted on a
small PVC, SQLite backend). Leave blank for a throwaway dev install — the
chart generates one and **persists it across upgrades** (via Helm's
`lookup`, same pattern as `api.sessionNonceSecret` below) so it doesn't
silently rotate out from under an already-initialized domain key. For
anything beyond dev, set it explicitly (values overlay/`--set-string`,
never committed) and back it up — losing it after `linkkeys domain init`
has run means losing the domain key.

### 4. Session nonce secret — `api.sessionNonceSecret`

HMACs firepit's login nonce (`FIREPIT_SESSION_NONCE_SECRET`). Same
generate-and-persist-across-upgrades behavior as #3. Only needs to be set
explicitly if you want a specific value (e.g. matching a value from before
this chart existed).

### 5. Registry pull secret — `imagePullSecrets`

`containers.catalystsquad.com` is a private registry. Create a
`kubernetes.io/dockerconfigjson` Secret (conventionally named `regcred`) in
the release namespace yourself — `.reactorcide/jobs/scripts/deploy.sh` does
this before `helm upgrade` in CI — and reference it:

```yaml
imagePullSecrets:
  - name: regcred
```

## Gateway prerequisites

HTTPRoutes are only rendered when `gateway.enabled: true`. They attach to
an **existing** Gateway API `Gateway` resource by name/namespace
(`gateway.gatewayName` / `gateway.gatewayNamespace` /
`gateway.sectionName`) — this chart never creates a Gateway itself,
mirroring `reactorcide/helm_chart`'s `httproute-app.yaml`/
`httproute-web.yaml` pattern. Two HTTPRoutes are created per domain in
`gateway.domains`:

- `<release>-firepit-api-<domain>` — `PathPrefix /csil`, `/auth`,
  `/webhooks` and `Exact /healthz`, backed by the api Service.
- `<release>-firepit-webapp-<domain>` — catch-all `PathPrefix /`, backed by
  the webapp Service (the default backend).

Gateway API resolves the more specific path matches to the api route
regardless of which HTTPRoute object declares them, as long as both attach
to the same Gateway + hostname — no explicit rule ordering/priority is
needed.

Before enabling this, confirm:

- The named Gateway exists in the target cluster/namespace with an `https`
  (or whatever `gateway.sectionName` names) listener bound to the same
  hostname(s) you're about to list in `gateway.domains`.
- The Gateway's controller (e.g. contour, per longhouse's own
  `deploy/values-catalystsquad.yaml`) supports Gateway API v1 `HTTPRoute`.
- DNS for each domain in `gateway.domains` points at the Gateway's LB.

## First-deploy runbook (`firepit.catalystsquad.com`)

1. **Provision Postgres.** Apply a Zalando `postgresql` CR for firepit
   (teamId/database/user of your choosing) in the target namespace, wait
   for the operator to create its credentials Secret, then build the
   single-key `DB_URI` Secret this chart expects:

   ```sh
   NS=firepit
   CLUSTER=firepit-postgres     # the CR's metadata.name
   USER=firepit
   read -r HOST < <(kubectl get svc -n "$NS" "$CLUSTER" -o jsonpath='{.spec.clusterIP}')
   PASSWORD=$(kubectl get secret -n "$NS" "$USER.$CLUSTER.credentials.postgresql.acid.zalan.do" \
     -o jsonpath='{.data.password}' | base64 -d)
   kubectl create secret generic firepit-db-uri -n "$NS" \
     --from-literal=DB_URI="postgresql://$USER:$PASSWORD@$HOST:5432/firepit_db?sslmode=require"
   ```

2. **First `helm upgrade --install`** with `linkkeysRp.enabled: true` and
   no `linkkeys.pki.apiKey` yet — the RP comes up with no domain key and no
   API-key users; `api` will fail login attempts (everything else still
   works) until step 3.

3. **Initialize the RP's domain key and mint an API key:**

   ```sh
   POD=$(kubectl get pod -n firepit -l app.kubernetes.io/component=linkkeys-rp -o name | head -1)
   kubectl exec -n firepit -it "$POD" -- linkkeys domain init
   kubectl exec -n firepit -it "$POD" -- linkkeys user create firepit-api "Firepit API" --api-key
   # copy the printed key
   ```

4. **Feed the API key back in** and re-deploy:

   ```sh
   helm upgrade --install firepit ./helm_chart \
     -f deploy/values-catalystsquad.yaml \
     --set-string linkkeys.pki.apiKey="<key from step 3>"
   ```

   (In CI, this is `.reactorcide/jobs/deploy.yaml` sourcing the same value
   from `${secret:catalystcommunity/firepit:linkkeys_api_key}` — store it
   there once so subsequent tag-triggered deploys don't need step 3/4
   again.)

5. **Publish the domain's DNS TXT record** as printed by
   `linkkeys domain dns-check` inside the RP pod, if this firepit instance
   is also meant to be discoverable as its own linkkeys domain (not just a
   consumer of `linkkeys.todandlorna.com`'s IDP).

6. **Enable the Gateway routes** (`gateway.enabled: true` +
   `gatewayName`/`gatewayNamespace`/`domains`) once DNS for
   `firepit.catalystsquad.com` points at the cluster's Gateway LB.

## Validating locally (no cluster required)

```sh
helm lint helm_chart
helm template firepit helm_chart                       # defaults
helm template firepit helm_chart -f deploy/values-example.yaml
```

`helm template` renders fully offline — Helm's `lookup` function (used by
the two generate-and-persist secrets above) returns nothing without a
live cluster, so every render mints fresh dev-only secret values; this is
expected for `helm template`/`--dry-run` and harmless for validation.

## Values reference

See `values.yaml` — every field has an inline doc comment. Key groups:
`image.*`, `database.*` (Zalando convention), `api.*`
(`FIREPIT_*` env surface), `linkkeys.*` (client-side RP config),
`linkkeysRp.*` (this chart's own RP Deployment), `webapp.*`, `gateway.*`
(HTTPRoutes), `resources.*`.
