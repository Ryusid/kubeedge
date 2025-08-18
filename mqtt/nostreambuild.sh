#!/usr/bin/env bash
set -euo pipefail

# Image & build settings
IMAGE_REPO="ryusid/motion-mqtt-mapper"
PLATFORM="${PLATFORM:-linux/arm64}"
DOCKERFILE="${DOCKERFILE:-Dockerfile}"        # <-- use the new Dockerfile
PUSH="${PUSH:---push}"                        # use --load for local testing

# Optional module proxy override (defaults to the one in Dockerfile)
GOPROXY_ARG=${GOPROXY_ARG:-"https://goproxy.cn,direct"}

# Optional: registry cache location (uncomment & set to your registry to enable remote cache)
CACHE_IMAGE="docker.io/ryusid/motion-mqtt-mapper:buildcache"
CACHE_FROM=()
CACHE_TO=()
if [[ -n "${CACHE_IMAGE:-}" ]]; then
  CACHE_FROM+=(--cache-from "type=registry,ref=${CACHE_IMAGE}")
  CACHE_TO+=(--cache-to "type=registry,mode=max,ref=${CACHE_IMAGE}")
fi

# Tag prompt
TAG="${1:-}"
if [[ -z "$TAG" ]]; then
  read -rp "Enter image tag for ${IMAGE_REPO} (e.g., arm64v5): " TAG
  while [[ -z "$TAG" ]]; do
    read -rp "Tag cannot be empty. Enter image tag: " TAG
  done
fi

# Ensure buildx builder exists
if ! docker buildx inspect >/dev/null 2>&1; then
  echo "No buildx builder found. Creating one..."
  docker buildx create --use --name builder || true
  docker buildx inspect --bootstrap
fi

echo "Building ${IMAGE_REPO}:${TAG} (platform: ${PLATFORM}) with ${DOCKERFILE}"
sudo docker buildx build \
  --platform "${PLATFORM}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_REPO}:${TAG}" \
  --build-arg "GOPROXY=${GOPROXY_ARG}" \
  "${CACHE_FROM[@]}" \
  "${CACHE_TO[@]}" \
  ${PUSH} \
  .

echo "Done."
