#!/usr/bin/env bash
# Builds static Linux binaries (amd64 + arm64) into dist/ with checksums.
# Usage: scripts/build.sh [version]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-$(grep -oE 'Version = "[^"]+"' internal/engine/manifest.go | cut -d'"' -f2)}"
mkdir -p dist
for arch in amd64 arm64; do
  out="dist/coolify-mirror-linux-${arch}"
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/mtalavi/coolify-mirror/internal/engine.Version=${VERSION}" \
    -o "$out" ./cmd/coolify-mirror
  echo "built $out"
done
( cd dist && sha256sum coolify-mirror-linux-* > SHA256SUMS && cat SHA256SUMS )
