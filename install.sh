#!/bin/sh
# Installs the latest coolify-mirror release into /usr/local/bin.
#   curl -fsSL https://raw.githubusercontent.com/mtalavi/coolify-mirror/main/install.sh | sudo sh
# Pin a version with: ... | sudo VERSION=v1.1.0 sh
set -eu

REPO="mtalavi/coolify-mirror"
DEST="${DEST:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) echo "coolify-mirror: unsupported CPU $(uname -m) (amd64 and arm64 only)" >&2; exit 1 ;;
esac
[ "$(uname -s)" = Linux ] || { echo "coolify-mirror: Linux only" >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "coolify-mirror: run as root (… | sudo sh)" >&2; exit 1; }
command -v curl >/dev/null || { echo "coolify-mirror: curl is required" >&2; exit 1; }

if [ "$VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
BIN="coolify-mirror-linux-$ARCH"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading $BIN ($VERSION)…"
curl -fsSL "$BASE/$BIN" -o "$TMP/$BIN"
curl -fsSL "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"
( cd "$TMP" && grep " $BIN\$" SHA256SUMS | sha256sum -c - >/dev/null ) || { echo "coolify-mirror: checksum mismatch, not installed" >&2; exit 1; }

install -m 0755 "$TMP/$BIN" "$DEST/coolify-mirror"
echo "Installed $("$DEST/coolify-mirror" version) to $DEST/coolify-mirror"
echo "Run it with:  sudo coolify-mirror"
