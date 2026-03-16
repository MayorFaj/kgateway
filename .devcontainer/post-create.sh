#!/bin/bash
set -e

: "${DEVCONTAINER_IMAGE_REGISTRY:?DEVCONTAINER_IMAGE_REGISTRY must be set}"
: "${DEVCONTAINER_VERSION:?DEVCONTAINER_VERSION must be set}"

make setup-base

images=(
  "kgateway:${DEVCONTAINER_VERSION}"
  "sds:${DEVCONTAINER_VERSION}"
  "envoy-wrapper:${DEVCONTAINER_VERSION}"
  "dummy-idp:0.0.1"
  "extproc-server:0.0.1"
)

for img in "${images[@]}"; do
  full_image="${DEVCONTAINER_IMAGE_REGISTRY}/${img}"

  if ! docker image inspect "${full_image}" >/dev/null 2>&1; then
    if ! docker pull "${full_image}"; then
      echo "Warning: failed to pull ${img}, skipping kind load"
      continue
    fi
  fi

  go tool kind load docker-image "${full_image}" --name kind || \
    echo "Warning: failed to load ${img} into kind"
done

IMAGE_REGISTRY="${DEVCONTAINER_IMAGE_REGISTRY}" VERSION="${DEVCONTAINER_VERSION}" make package-kgateway-charts
