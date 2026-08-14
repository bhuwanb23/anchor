#!/usr/bin/env bash
# Build (and optionally push) Infer runtime images for the tags the agent expects.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
IMAGE_BASE="${1:-ghcr.io/yourname/infer}"
PUSH=0
if [[ "${2:-}" == "--push" ]] || [[ "${1:-}" == "--push" ]]; then
  PUSH=1
  if [[ "${1:-}" == "--push" ]]; then
    IMAGE_BASE="${2:-ghcr.io/yourname/infer}"
  fi
fi

DOCKERFILE="$ROOT/infer/docker/Dockerfile"
CONTEXT="$ROOT/infer/docker"

echo "Building Infer images under: $IMAGE_BASE"

# Host-arch image (what this machine can actually produce without buildx multi-arch).
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  aarch64|arm64) TAGS=(arm64 arm64-dotprod arm64-i8mm arm64-i8mm-sve arm64-sve2-i8mm) ;;
  x86_64|amd64)  TAGS=(x86_64) ;;
  *) echo "unsupported arch: $HOST_ARCH"; exit 1 ;;
esac

PRIMARY="${TAGS[0]}"
docker build -f "$DOCKERFILE" -t "${IMAGE_BASE}:${PRIMARY}" "$CONTEXT"

for t in "${TAGS[@]}"; do
  if [[ "$t" != "$PRIMARY" ]]; then
    docker tag "${IMAGE_BASE}:${PRIMARY}" "${IMAGE_BASE}:${t}"
  fi
  if [[ "$PUSH" -eq 1 ]]; then
    docker push "${IMAGE_BASE}:${t}"
  fi
done

echo ""
echo "Done. Point agents at these images with:"
echo "  export ANCHOR_INFER_IMAGE_BASE=${IMAGE_BASE}"
echo "Or set ANCHOR_INFER_IMAGE_BASE in the agent systemd unit / config."
