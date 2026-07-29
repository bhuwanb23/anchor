#!/usr/bin/env bash
set -euo pipefail

# ================================================
# YourPlatform Release Builder
# Usage: ./scripts/release.sh <version>
# ================================================

VERSION="${1:-}"

if [ -z "$VERSION" ]; then
  echo "Usage: ./scripts/release.sh <version>"
  echo ""
  echo "Example:"
  echo "  ./scripts/release.sh 0.1.0"
  echo ""
  echo "This will:"
  echo "  1. Build agent binaries for Linux amd64 and arm64"
  echo "  2. Generate SHA-256 checksums"
  echo "  3. Print next steps for tagging and uploading"
  exit 1
fi

echo "Building release v${VERSION}..."
echo ""

# Clean previous release
rm -rf release/
mkdir -p release

# Build
echo "--- Building linux/amd64 ---"
make build-agent-linux-amd64
echo ""

echo "--- Building linux/arm64 ---"
make build-agent-linux-arm64
echo ""

# Checksums
echo "--- Generating checksums ---"
make checksum
echo ""

# Summary
echo "========================================"
echo " Release v${VERSION} Ready"
echo "========================================"
echo ""
echo "Artifacts:"
ls -lh release/
echo ""
echo "Checksums:"
cat release/*.sha256
echo ""
echo "Next steps:"
echo "  1. Update version in agent/cmd/agent/main.go (const Version)"
echo "  2. Update AGENT_VERSION in scripts/install.sh"
echo "  3. Commit changes:"
echo "       git add release/"
echo "       git commit -m 'release: v${VERSION}'"
echo "  4. Tag the release:"
echo "       git tag v${VERSION}"
echo "       git push origin v${VERSION}"
echo "  5. Create GitHub release and upload artifacts:"
echo "       gh release create v${VERSION} release/* --title 'v${VERSION}' --generate-notes"
