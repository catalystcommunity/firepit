#!/bin/sh
set -e

SEMVER_TAGS_VERSION="v0.4.0"
BUILDKIT_VERSION="0.17.3"
GHCLI_VERSION="2.63.2"

# Script is invoked from the repo root (runnerbase:dev's job convention —
# see test-go.yaml/test-web.yaml, which `cd /job/src` the same way).
cd "${REACTORCIDE_REPOROOT:-/job/src}"

# run-local parity (longhouse/reactorcide's own release.sh convention):
# SKIP_GITHUB=true skips the version-bump push and the `gh release create`
# step, so the version-file edits + image build/push still run against a
# working tree without publishing anything.
if [ "${REACTORCIDE_WORKER_MODE:-remote}" = "local" ] && [ -z "${SKIP_GITHUB:-}" ]; then
  echo "=== run-local detected: SKIP_GITHUB=true ==="
  SKIP_GITHUB=true
fi

# -------------------------------------------------------------------
# 0. Git auth + land on main (semver-tags pushes the new tag itself, so
#    the credential needs to be on `origin` first).
# -------------------------------------------------------------------
if [ "${SKIP_GITHUB:-false}" = "true" ]; then
  echo "=== SKIP_GITHUB=true: leaving git auth + branch state alone ==="
else
  git config user.name "Catalyst Community (automation)"
  git config user.email "automation@catalystcommunity.dev"
  git remote set-url origin "https://x-access-token:${GITHUB_PAT}@github.com/${REACTORCIDE_REPO}.git"
  echo "=== Aligning to origin/main ==="
  git fetch origin main
  git checkout -B main origin/main
fi

# -------------------------------------------------------------------
# 1. Install semver-tags
# -------------------------------------------------------------------
echo "=== Installing semver-tags ${SEMVER_TAGS_VERSION} ==="
curl -fsSL "https://github.com/catalystcommunity/semver-tags/releases/download/${SEMVER_TAGS_VERSION}/semver-tags.tar.gz" \
  -o /tmp/semver-tags.tar.gz
tar -xzf /tmp/semver-tags.tar.gz -C /tmp
chmod +x /tmp/semver-tags
export PATH="/tmp:$PATH"

# -------------------------------------------------------------------
# 2. Determine version bump from conventional commits
# -------------------------------------------------------------------
echo "=== Running semver-tags ==="
semver-tags run --output_json > /tmp/semver-output.txt 2>&1 || true
cat /tmp/semver-output.txt
OUTPUT=$(tail -1 /tmp/semver-output.txt)

NEW_TAG=$(echo "${OUTPUT}" | grep -o '"New_release_git_tag":"[^"]*"' | cut -d'"' -f4)
PUBLISHED=$(echo "${OUTPUT}" | grep -o '"New_release_published":"[^"]*"' | cut -d'"' -f4)

if [ "${PUBLISHED}" != "true" ]; then
  echo "No new release needed."
  exit 0
fi

echo "=== New release: ${NEW_TAG} ==="
VERSION="${NEW_TAG#v}"

# -------------------------------------------------------------------
# 3. Update versioned files
# -------------------------------------------------------------------
echo "=== Updating versioned files to ${VERSION} ==="
sed -i "s/^version: .*/version: ${VERSION}/" helm_chart/Chart.yaml
sed -i "s/^appVersion: .*/appVersion: \"${VERSION}\"/" helm_chart/Chart.yaml
echo "${VERSION}" > version/VERSION.txt

if [ "${SKIP_GITHUB:-false}" = "true" ]; then
  echo "=== SKIP_GITHUB=true: skipping version-bump commit and push ==="
else
  git add helm_chart/Chart.yaml version/VERSION.txt
  if git diff --cached --quiet; then
    echo "No version changes to commit"
  else
    git commit -m "ci: bump version to ${VERSION}"
    # Explicit refspec: `git checkout -B main origin/main` didn't set
    # upstream tracking, so a bare `git push` fails with "no upstream".
    git push origin HEAD:main
  fi
fi

# -------------------------------------------------------------------
# 4. Install buildctl (no buildkitd — the `builder` capability's sidecar
#    provides the daemon; BUILDKIT_HOST is injected into this job's
#    environment automatically, same as
#    reactorcide/.reactorcide/jobs/scripts/release.sh and
#    reactorcide/jobs/build-all.yaml).
# -------------------------------------------------------------------
echo "=== Installing buildctl ${BUILDKIT_VERSION} ==="
curl -fsSL "https://github.com/moby/buildkit/releases/download/v${BUILDKIT_VERSION}/buildkit-v${BUILDKIT_VERSION}.linux-amd64.tar.gz" -o /tmp/buildkit.tar.gz
tar -xzf /tmp/buildkit.tar.gz -C /usr/local bin/buildctl
rm /tmp/buildkit.tar.gz

echo "=== Waiting for builder sidecar ==="
for i in $(seq 1 30); do
  if buildctl debug info >/dev/null 2>&1; then
    echo "builder sidecar is ready"
    break
  fi
  sleep 1
done

# -------------------------------------------------------------------
# 5. Registry auth
# -------------------------------------------------------------------
echo "=== Configuring registry auth ==="
export HOME="${HOME:-/root}"
mkdir -p "$HOME/.docker"
AUTH=$(printf "%s:%s" "$REGISTRY_USER" "$REGISTRY_PASSWORD" | base64 -w 0)
printf '{"auths":{"%s":{"auth":"%s"}}}\n' "$REGISTRY" "$AUTH" > "$HOME/.docker/config.json"

# Build + push helper. Uses buildctl's multi-name output so each image
# builds once and is pushed with both :$VERSION and :latest (mirrors
# reactorcide/.reactorcide/jobs/scripts/release.sh's build_and_push).
build_and_push() {
  dockerfile="$1"
  repo="$2"
  echo "=== Building and pushing ${repo}:${VERSION} and :latest ==="
  buildctl build \
    --frontend dockerfile.v0 \
    --local context=. \
    --local dockerfile=. \
    --opt filename="$dockerfile" \
    --opt build-arg:CACHEBUST="$(date +%s)" \
    --output "type=image,\"name=${repo}:${VERSION},${repo}:latest\",push=true"
}

# -------------------------------------------------------------------
# 6. Build and push firepit-api and firepit-webapp
# -------------------------------------------------------------------
echo "=== Building images ==="
build_and_push api/Dockerfile "${REGISTRY}/catalystcommunity/firepit-api"
build_and_push webapp/Dockerfile "${REGISTRY}/catalystcommunity/firepit-webapp"

# -------------------------------------------------------------------
# 7. GitHub release
# -------------------------------------------------------------------
if [ "${SKIP_GITHUB:-false}" = "true" ]; then
  echo "=== SKIP_GITHUB=true: skipping GitHub release create ==="
else
  echo "=== Creating GitHub release ==="
  curl -fsSL "https://github.com/cli/cli/releases/download/v${GHCLI_VERSION}/gh_${GHCLI_VERSION}_linux_amd64.tar.gz" -o /tmp/gh.tar.gz
  tar -xzf /tmp/gh.tar.gz -C /tmp
  export PATH="/tmp/gh_${GHCLI_VERSION}_linux_amd64/bin:$PATH"

  GH_TOKEN="${GITHUB_PAT}" gh release create "${NEW_TAG}" \
    --repo "${REACTORCIDE_REPO}" \
    --title "${NEW_TAG}" \
    --generate-notes

  echo "=== Released ${NEW_TAG} ==="
fi

echo ""
echo "================================================"
echo "firepit ${NEW_TAG} released"
echo "  ${REGISTRY}/catalystcommunity/firepit-api:${VERSION}"
echo "  ${REGISTRY}/catalystcommunity/firepit-webapp:${VERSION}"
echo "================================================"
