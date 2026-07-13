#!/bin/sh
set -eu

NODE_VERSION="${NODE_VERSION:-26.1.0}"
REPO_ROOT="${REACTORCIDE_JOB_DIR:-${REACTORCIDE_CODE_DIR:-/job/src}}"

export HOME="${HOME:-/home/runner}"
mkdir -p "$HOME/.local"

echo "=== Installing Node ${NODE_VERSION} ==="
curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" -o /tmp/node.tar.xz
rm -rf "$HOME/.local/node"
tar -xJf /tmp/node.tar.xz -C "$HOME/.local"
mv "$HOME/.local/node-v${NODE_VERSION}-linux-x64" "$HOME/.local/node"
rm /tmp/node.tar.xz
export PATH="$HOME/.local/node/bin:$PATH"

cd "$REPO_ROOT"

echo "=== npm ci (webapp) ==="
( cd webapp && npm ci )

echo "=== ./tools.sh lint-web ==="
./tools.sh lint-web

echo "=== ./tools.sh test-web ==="
./tools.sh test-web

echo "=== webapp typecheck ==="
( cd webapp && npm run typecheck )

echo "=== webapp build ==="
( cd webapp && npm run build )
