#!/bin/bash
set -e

: "${DEVCONTAINER_IMAGE_REGISTRY:?DEVCONTAINER_IMAGE_REGISTRY must be set}"
: "${DEVCONTAINER_VERSION:?DEVCONTAINER_VERSION must be set}"

images=(
  "kgateway:${DEVCONTAINER_VERSION}"
  "sds:${DEVCONTAINER_VERSION}"
  "envoy-wrapper:${DEVCONTAINER_VERSION}"
  "dummy-idp:0.0.1"
  "extproc-server:0.0.1"
)

for img in "${images[@]}"; do
  full_image="${DEVCONTAINER_IMAGE_REGISTRY}/${img}"
  docker pull "${full_image}" || \
    echo "Warning: failed to pull ${full_image}, build it locally with: IMAGE_REGISTRY=${DEVCONTAINER_IMAGE_REGISTRY} VERSION=${DEVCONTAINER_VERSION} make <target>-docker"
done

# Package Helm charts so e2e tests can find _test/index.yaml even if
# postCreateCommand fails (e.g. due to Codespaces caching an old command).
IMAGE_REGISTRY="${DEVCONTAINER_IMAGE_REGISTRY}" VERSION="${DEVCONTAINER_VERSION}" make package-kgateway-charts
