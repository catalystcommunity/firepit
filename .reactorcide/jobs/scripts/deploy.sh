#!/usr/bin/env bash
set -euo pipefail

echo "================================================"
echo "Firepit Deploy"
echo "================================================"

cd "${REACTORCIDE_REPOROOT:-/job/src}"

if [[ -z "${K8S_NAMESPACE:-}" ]]; then
    echo "ERROR: K8S_NAMESPACE must be set"
    exit 1
fi
if [[ -z "${HELM_RELEASE:-}" ]]; then
    echo "ERROR: HELM_RELEASE must be set"
    exit 1
fi
if [[ -z "${HELM_VALUES_FILE:-}" ]]; then
    echo "ERROR: HELM_VALUES_FILE must be set"
    exit 1
fi

# REACTORCIDE_BRANCH carries the pushed tag's name for this job's
# tag_created trigger (e.g. "v0.3.0" — see coordinator_api/internal/
# handlers/eval_job.go's extractBranchOrTag; despite the var's name, it's
# the tag here, not a branch). release.yaml already built and pushed
# firepit-api/-webapp under that same version (without the leading "v")
# before this tag existed, so pinning to it here is always a real,
# already-published image. Falls back to version/VERSION.txt so
# `reactorcide run-local` (which has no real tag push behind it) still
# resolves to something.
if [[ -n "${REACTORCIDE_BRANCH:-}" ]]; then
    IMAGE_TAG="${REACTORCIDE_BRANCH#v}"
elif [[ -s version/VERSION.txt ]]; then
    IMAGE_TAG="$(tr -d '[:space:]' < version/VERSION.txt)"
else
    IMAGE_TAG="latest"
fi

echo "Namespace:  ${K8S_NAMESPACE}"
echo "Release:    ${HELM_RELEASE}"
echo "Values:     ${HELM_VALUES_FILE}"
echo "Image tag:  ${IMAGE_TAG}"

# ================================================
# Setup tools
# ================================================
export HOME="${HOME:-/root}"
LOCAL_BIN="$HOME/.local/bin"
mkdir -p "$LOCAL_BIN"
export PATH="$LOCAL_BIN:$PATH"

if ! command -v helm &> /dev/null; then
    echo "Installing helm..."
    curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | USE_SUDO=false HELM_INSTALL_DIR="$LOCAL_BIN" bash
fi

if ! command -v kubectl &> /dev/null; then
    echo "Installing kubectl..."
    KUBECTL_VERSION=$(curl -fsSL https://dl.k8s.io/release/stable.txt)
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o "$LOCAL_BIN/kubectl"
    chmod +x "$LOCAL_BIN/kubectl"
fi

# ================================================
# Configure kubectl
# ================================================
mkdir -p ~/.kube
echo "${KUBECONFIG_CONTENT}" > ~/.kube/config
chmod 600 ~/.kube/config

# ================================================
# Create namespace and registry pull secret
# ================================================
kubectl create namespace "${K8S_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

if [[ -n "${REGISTRY_USER:-}" ]] && [[ -n "${REGISTRY_PASSWORD:-}" ]]; then
    kubectl create secret docker-registry regcred \
        --namespace "${K8S_NAMESPACE}" \
        --save-config \
        --dry-run=client \
        --docker-server="containers.catalystsquad.com" \
        --docker-username="${REGISTRY_USER}" \
        --docker-password="${REGISTRY_PASSWORD}" \
        -o yaml | kubectl apply -f -
fi

# ================================================
# Deploy with Helm
# ================================================
echo ""
echo "================================================"
echo "Deploying with Helm"
echo "================================================"

# Runtime-only overrides layered on top of HELM_VALUES_FILE. May carry
# secret-sourced values (linkkeys API key, RP domain-key passphrase) that
# shouldn't linger on disk, so this file is tightened and shredded on exit
# — same pattern as longhouse's deploy.sh.
RUNTIME_VALUES="/tmp/runtime-values.yaml"
( umask 077 && : > "${RUNTIME_VALUES}" )
trap 'shred -u "${RUNTIME_VALUES}" 2>/dev/null || rm -f "${RUNTIME_VALUES}"' EXIT

{
    cat <<VALS
image:
  api:
    tag: "${IMAGE_TAG}"
  webapp:
    tag: "${IMAGE_TAG}"
VALS
    if [[ -n "${LINKKEYS_PKI_API_KEY:-}" ]]; then
        cat <<VALS
linkkeys:
  pki:
    apiKey: "${LINKKEYS_PKI_API_KEY}"
VALS
    fi
    if [[ -n "${LINKKEYS_RP_DOMAIN_KEY_PASSPHRASE:-}" ]]; then
        cat <<VALS
linkkeysRp:
  domainKeyPassphrase: "${LINKKEYS_RP_DOMAIN_KEY_PASSPHRASE}"
VALS
    fi
} > "${RUNTIME_VALUES}"

helm upgrade \
    --install \
    --create-namespace \
    --namespace "${K8S_NAMESPACE}" \
    "${HELM_RELEASE}" \
    ./helm_chart \
    -f "${HELM_VALUES_FILE}" \
    -f "${RUNTIME_VALUES}" \
    --wait \
    --timeout 5m

echo ""
echo "================================================"
echo "Deployment complete!"
echo "Namespace: ${K8S_NAMESPACE}"
echo "Release:   ${HELM_RELEASE}"
echo "Image tag: ${IMAGE_TAG}"
echo "================================================"
