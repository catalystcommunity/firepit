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
    err "csil schema not yet defined (see PLANDOC.md task A2) — nothing to generate yet"
    exit 1
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
