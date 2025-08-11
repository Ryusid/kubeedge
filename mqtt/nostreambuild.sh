#!/usr/bin/env bash
set -euo pipefail

IMAGE_REPO="ryusid/motion-mqtt-mapper"
PLATFORM="linux/arm64"
DOCKERFILE="Dockerfile_nostream"
PUSH="--push"

TAG="${1:-}"

if [[ -z "$TAG" ]]; then
  read -rp "Enter image tag for ${IMAGE_REPO} (e.g., arm64v5): " TAG
  while [[ -z "$TAG" ]]; do
    read -rp "Tag cannot be empty. Enter image tag: " TAG
  done
fi

echo "Building ${IMAGE_REPO}:${TAG} (platform: ${PLATFORM})"
sudo docker buildx build --platform "${PLATFORM}" -f "${DOCKERFILE}" -t "${IMAGE_REPO}:${TAG}" . ${PUSH}
echo "Done. Image: ${IMAGE_REPO}:${TAG}"