#!/usr/bin/env bash
set -euo pipefail
shopt -s extglob

# Match PowerShell's null-coalescing defaults: use the default only when the
# variable is unset, while allowing an explicitly empty value.
branch=${BRANCH-1.4.1}
registry=${REGISTRY-registry.kasemsan.com}
# platforms=${PLATFORMS-linux/amd64,linux/arm64}
platforms=${PLATFORMS-linux/amd64}

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

if [[ ${RUN_MIGRATIONS-} == "true" ]]; then
  if [[ -z ${DB_DSN-} ]]; then
    echo "RUN_MIGRATIONS=true requires DB_DSN" >&2
    exit 1
  fi

  "${script_dir}/apps/gateway/scripts/migrate.sh" up -Dsn "${DB_DSN}"
fi

vite_base_path=${VITE_BASE_PATH:-/admin/}

# Publish the immutable release tag and the current canonical release tag from
# the same multi-platform build manifest.
tags=(
  -t "${registry}/webrtc-sip-gateway-stack:${branch}"
  -t "${registry}/webrtc-sip-gateway-stack:latest"
)

# During the operator-defined migration window, point legacy repository names at
# the same immutable stack image. Example: LEGACY_IMAGE_NAMES=k2-stack
if [[ -n ${LEGACY_IMAGE_NAMES-} ]]; then
  IFS=',' read -r -a legacy_images <<< "${LEGACY_IMAGE_NAMES}"
  for legacy_image in "${legacy_images[@]}"; do
    # PowerShell trims each comma-separated name and drops empty entries.
    legacy_image=${legacy_image##+([[:space:]])}
    legacy_image=${legacy_image%%+([[:space:]])}
    [[ -n ${legacy_image} ]] && tags+=(-t "${registry}/${legacy_image}:${branch}")
  done
fi

# Build identity for the gateway's GET /api/version, taken from the checkout
# that is the build context (the current directory).
git_commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
git_version=$(git describe --tags --always --dirty 2>/dev/null || echo unknown)
build_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if [[ -n $(git status --porcelain 2>/dev/null) ]]; then
  git_dirty=true
else
  git_dirty=false
fi
echo "gateway build: version=${git_version} commit=${git_commit} branch=${git_branch} dirty=${git_dirty} build_time=${build_time}"

docker buildx build --push \
  --platform "${platforms}" \
  "${tags[@]}" \
  --build-arg "VITE_GATEWAY_URL=${VITE_GATEWAY_URL-}" \
  --build-arg "VITE_CONFIG_AUTORECORD=${VITE_CONFIG_AUTORECORD-}" \
  --build-arg "VITE_BASE_PATH=${vite_base_path}" \
  --build-arg "GIT_COMMIT=${git_commit}" \
  --build-arg "GIT_BRANCH=${git_branch}" \
  --build-arg "GIT_VERSION=${git_version}" \
  --build-arg "BUILD_TIME=${build_time}" \
  --build-arg "GIT_DIRTY=${git_dirty}" \
  -f deploy/Dockerfile.unified .
