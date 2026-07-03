#!/usr/bin/env bash
#
# Firepit task runner. One script, portable bash, run from anywhere in the
# repo (or outside it — it always resolves paths off its own location).
#
#   ./tools.sh gen               # csilgen -> api/internal/csil, webapp/src/gen, clients/
#   ./tools.sh test               # go test (api + coredb) + npm test (webapp)
#   ./tools.sh test-integration   # testcontainers-backed integration suite
#   ./tools.sh lint                # go vet (api + coredb) + eslint (webapp, if configured)
#   ./tools.sh migrate [up|down|status]   # goose migrate against DB_URI (default: docker-compose postgres)
#   ./tools.sh dev                  # docker compose up (postgres [+ linkkeys-rp] + api + webapp)
#   ./tools.sh build-images          # build deployable container images
#
# See PLANDOC.md and CLAUDE.md for the architecture these verbs operate on.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GREEN=$'\e[0;32m'
RED=$'\e[0;31m'
NC=$'\e[0m'

log_status() {
    echo "${GREEN}--------------------------------  ${1}  --------------------------------${NC}"
}
err() {
    echo "${RED}$*${NC}" >&2
}

usage() {
    cat <<EOF
Usage: ./tools.sh <command>

Commands:
  gen                Regenerate CSIL-derived code (server, TS client, clients/)
  test               Run all tests: go test (api, coredb) + npm test (webapp)
  test-integration   Run the testcontainers-backed integration suite
  lint               go vet (api, coredb) + eslint (webapp, if configured)
  migrate [verb]     Run goose against \$DB_URI (default: docker-compose postgres).
                     verb is one of up|down|status (default: up).
  dev                Boot the local dev stack via docker compose
  build-images       Build deployable container images (api, webapp)
EOF
}

not_implemented() {
    err "not yet implemented: $1"
    exit 1
}

cmd_gen() {
    log_status "gen"

    local SPEC="$SCRIPT_DIR/csil/firepit.csil"
    local GO_SERVER_OUT="$SCRIPT_DIR/api/internal/csil"
    local TS_CLIENT_OUT="$SCRIPT_DIR/webapp/src/gen"
    local CLIENTS_OUT="$SCRIPT_DIR/clients"
    local VERSION
    VERSION="$(tr -d '[:space:]' < "$SCRIPT_DIR/version/VERSION.txt")"

    # csilgen is installed via the org's csilgen install flow and normally lives
    # at ~/.local/bin/csilgen (see ~/.csilgen/generators/ for the wasm
    # generators it dispatches to). That directory isn't always on a
    # non-interactive PATH, so add it here rather than forcing every caller to
    # fix their environment — mirrors the longhouse regenerate.sh pattern.
    if [ -d "$HOME/.local/bin" ]; then
        export PATH="$HOME/.local/bin:$PATH"
    fi
    if [ -d "$HOME/.cargo/bin" ]; then
        export PATH="$HOME/.cargo/bin:$PATH"
    fi

    if ! command -v csilgen >/dev/null 2>&1; then
        err "csilgen not on PATH (looked in \$PATH, ~/.local/bin, ~/.cargo/bin)"
        err "install it from the catalystcommunity/csilgen repo (see its README) and re-run"
        exit 1
    fi

    log_status "gen: validate + lint"
    csilgen validate --input "$SPEC"
    # Lint currently only ever emits stylistic warnings against this schema
    # (PascalCase service names, kebab-case ops, \`;;;\` doc comments) — the
    # exact conventions csilgen's own docs/examples and longhouse use. Gate on
    # its exit code (non-zero = errors), not on warning count, so the build
    # doesn't fail on lint disagreeing with idiomatic CSIL style.
    csilgen lint "$SCRIPT_DIR/csil"

    # ---- Go server surface (api/internal/csil) ----
    # Bare \`go\` target = types + service interfaces, no emit_packages (this is
    # in-tree server code consumed by the api module, not a standalone
    # package). package_name is set in the spec's options block, so no
    # post-generation package-name rewrite is needed (unlike longhouse, which
    # sets no package_name and seds \`package api\` -> \`package csil\` after).
    log_status "gen: go server -> api/internal/csil"
    mkdir -p "$GO_SERVER_OUT"
    csilgen generate --input "$SPEC" --target go --output "$GO_SERVER_OUT"
    if command -v gofmt >/dev/null 2>&1; then
        gofmt -w "$GO_SERVER_OUT"
    fi

    # ---- TypeScript client surface (webapp/src/gen) ----
    # typescript-client = types + a typed client, no server/handler surface.
    # In-tree, imported directly by webapp source — no package.json/tsconfig
    # here (this isn't a standalone package, unlike clients/typescript below).
    log_status "gen: typescript-client -> webapp/src/gen"
    mkdir -p "$TS_CLIENT_OUT"
    csilgen generate --input "$SPEC" --target typescript-client --output "$TS_CLIENT_OUT" \
        --readme-csil-rpc

    # ---- Standalone, publishable clients (clients/go, clients/typescript) ----
    # Each gets its own options block spliced onto the shared spec (coordinates
    # are per-language; emit_packages must NOT leak into the server/webapp
    # generation above, which shares $SPEC unmodified) — same technique as
    # longhouse's clients/regenerate.sh.
    log_status "gen: full clients -> clients/{go,typescript}"

    # The spliced per-language specs must live alongside firepit.csil (not in
    # /tmp) so their relative `include "types/...csil"` paths still resolve —
    # csilgen resolves includes relative to the including file's own
    # directory. Cleaned up unconditionally on return.
    local TMPDIR="$SCRIPT_DIR/csil"
    _cleanup_spliced() { rm -f "$TMPDIR"/.gen-tmp-*.csil; }
    trap _cleanup_spliced RETURN

    _splice_options() {
        # $1 = extra options (comma-joined, no trailing comma), $2 = output path
        #
        # CSIL's grammar is `*import_statement [options_block] *rule` — the
        # options block must come AFTER the file's `include`s, not before. So
        # this replaces the spec's existing options block IN PLACE (wherever
        # it falls, after the includes) rather than prepending a new one
        # before them, which would parse as an options block followed by more
        # imports — a grammar violation (\`Expected identifier ... but found
        # 'include'\`).
        awk -v extra="$1" -v version="$VERSION" '
            /^options[[:space:]]*\{/ { in_opts=1
                print "options {"
                print "    package: \"firepit\","
                print "    version: \"v1alpha\","
                print "    package_version: \"" version "\","
                print "    " extra
                print "}"
                next
            }
            in_opts && /^}/ { in_opts=0; next }
            in_opts { next }
            { print }
        ' "$SPEC" > "$2"
    }

    # go_package is deliberately omitted: its last path segment becomes the Go
    # package name, and this module's own last segment is "go" (a reserved
    # word — `package go` doesn't parse). package_name alone (with no
    # go_package to override it) names the package "firepitclient" instead.
    local go_spec="$TMPDIR/.gen-tmp-go-client.csil"
    _splice_options "emit_packages: [\"go\"], package_name: \"firepitclient\", go_module: \"github.com/catalystcommunity/firepit/clients/go\"" "$go_spec"
    rm -rf "$CLIENTS_OUT/go"
    mkdir -p "$CLIENTS_OUT/go"
    csilgen generate --input "$go_spec" --target go-client --output "$CLIENTS_OUT/go" --readme-csil-rpc
    if command -v gofmt >/dev/null 2>&1; then
        gofmt -w "$CLIENTS_OUT/go"
    fi

    local ts_spec="$TMPDIR/.gen-tmp-ts-client.csil"
    _splice_options "emit_packages: [\"typescript\"], package_name: \"@firepit/client\"" "$ts_spec"
    rm -rf "$CLIENTS_OUT/typescript"
    mkdir -p "$CLIENTS_OUT/typescript"
    csilgen generate --input "$ts_spec" --target typescript-client --output "$CLIENTS_OUT/typescript" --readme-csil-rpc

    log_status "gen: done"
    echo "  api/internal/csil:   $(ls "$GO_SERVER_OUT"/*.gen.go 2>/dev/null | wc -l) file(s)"
    echo "  webapp/src/gen:      $(ls "$TS_CLIENT_OUT"/*.gen.ts 2>/dev/null | wc -l) file(s)"
    echo "  clients/go:          $(find "$CLIENTS_OUT/go" -type f 2>/dev/null | wc -l) file(s)"
    echo "  clients/typescript:  $(find "$CLIENTS_OUT/typescript" -type f 2>/dev/null | wc -l) file(s)"
}

cmd_test() {
    log_status "go test ./... (api)"
    ( cd "$SCRIPT_DIR/api" && go test ./... )

    log_status "go test ./... (coredb)"
    ( cd "$SCRIPT_DIR/coredb" && go test ./... )

    log_status "npm test (webapp)"
    ( cd "$SCRIPT_DIR/webapp" && npm test )

    log_status "all tests passed"
}

cmd_test_integration() {
    log_status "test-integration"
    command -v docker >/dev/null 2>&1 || { err "docker is required for './tools.sh test-integration' (testcontainers)"; exit 1; }

    log_status "go test -tags=integration (api/internal/store)"
    ( cd "$SCRIPT_DIR/api" && go test -tags=integration ./internal/store/... )

    log_status "test-integration passed"
}

cmd_lint() {
    log_status "go vet (api)"
    ( cd "$SCRIPT_DIR/api" && go vet ./... )

    log_status "go vet (coredb)"
    ( cd "$SCRIPT_DIR/coredb" && go vet ./... )

    if [ -f "$SCRIPT_DIR/webapp/.eslintrc" ] || [ -f "$SCRIPT_DIR/webapp/.eslintrc.js" ] \
        || [ -f "$SCRIPT_DIR/webapp/.eslintrc.cjs" ] || [ -f "$SCRIPT_DIR/webapp/eslint.config.js" ] \
        || [ -f "$SCRIPT_DIR/webapp/eslint.config.ts" ]; then
        log_status "eslint (webapp)"
        ( cd "$SCRIPT_DIR/webapp" && npx eslint . )
    else
        log_status "eslint (webapp) — skipped, no eslint config present yet"
    fi

    log_status "lint passed"
}

cmd_migrate() {
    log_status "migrate"
    local verb="${2:-up}"
    case "$verb" in
        up|down|status) ;;
        *) err "unknown migrate verb: $verb (expected up|down|status)"; exit 1 ;;
    esac
    ( cd "$SCRIPT_DIR/coredb" && go run ./cmd/migrate "$verb" )
}

cmd_dev() {
    log_status "dev"
    command -v docker >/dev/null 2>&1 || { err "docker is required for './tools.sh dev'"; exit 1; }
    docker compose up
}

cmd_build_images() {
    log_status "build-images"
    not_implemented "build-images (Dockerfiles land with task D1)"
}

case "${1:-}" in
    gen)               cmd_gen ;;
    test)              cmd_test ;;
    test-integration)  cmd_test_integration ;;
    lint)              cmd_lint ;;
    migrate)           cmd_migrate "$@" ;;
    dev)               cmd_dev ;;
    build-images)      cmd_build_images ;;
    ""|-h|--help|help) usage ;;
    *)
        err "unknown command: $1"
        usage
        exit 1
        ;;
esac
