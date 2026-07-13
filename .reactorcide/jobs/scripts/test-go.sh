#!/bin/sh
set -eu

GO_VERSION="${GO_VERSION:-1.26.4}"
REPO_ROOT="${REACTORCIDE_JOB_DIR:-${REACTORCIDE_CODE_DIR:-/job/src}}"

export HOME="${HOME:-/home/runner}"
mkdir -p "$HOME/.local" "$HOME/go"

echo "=== Installing Go ${GO_VERSION} ==="
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
rm -rf "$HOME/.local/go"
tar -C "$HOME/.local" -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz

export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
export GOCACHE="${GOCACHE:-/tmp/firepit-gocache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/firepit-gomodcache}"

cd "$REPO_ROOT"

echo "=== ./tools.sh lint-go ==="
./tools.sh lint-go

echo "=== ./tools.sh test-go ==="
./tools.sh test-go

echo "=== ./tools.sh test-integration ==="
# Requires the `docker` capability. testcontainers-go talks to the Docker
# Engine API through DOCKER_HOST; it does not need a docker CLI binary.
if command -v docker >/dev/null 2>&1 || [ -n "${DOCKER_HOST:-}" ] || [ -S /var/run/docker.sock ]; then
    ./tools.sh test-integration
elif [ "${REACTORCIDE_WORKER_MODE:-remote}" = "local" ]; then
    echo "Skipping integration tests: Reactorcide's local Docker runner does not expose a Docker Engine endpoint to docker-capability jobs."
    echo "Deployed CI still requires the docker capability and will fail if DOCKER_HOST or /var/run/docker.sock is absent."
else
    ./tools.sh test-integration
fi
