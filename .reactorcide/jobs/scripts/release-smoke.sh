#!/bin/sh
set -eu

HELM_VERSION="${HELM_VERSION:-v3.18.4}"
REPO_ROOT="${REACTORCIDE_CODE_DIR:-${REACTORCIDE_JOB_DIR:-/job/src}}"

export HOME="${HOME:-/home/runner}"
mkdir -p "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"

cd "$REPO_ROOT"

echo "=== shell syntax ==="
sh -n .reactorcide/jobs/scripts/release.sh
sh -n .reactorcide/jobs/scripts/deploy.sh

if ! command -v helm >/dev/null 2>&1; then
    echo "=== Installing helm ${HELM_VERSION} ==="
    curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" -o /tmp/helm.tar.gz
    tar -xzf /tmp/helm.tar.gz -C /tmp
    cp /tmp/linux-amd64/helm "$HOME/.local/bin/helm"
    rm -rf /tmp/helm.tar.gz /tmp/linux-amd64
fi

echo "=== helm template ==="
helm template firepit ./helm_chart \
    -f deploy/values-catalystsquad.yaml \
    --set image.api.tag=ci-smoke \
    --set image.webapp.tag=ci-smoke \
    --set linkkeys.pki.apiKey=ci-smoke \
    --set linkkeysRp.domainKeyPassphrase=ci-smoke \
    >/tmp/firepit-rendered.yaml

echo "=== release smoke passed ==="
