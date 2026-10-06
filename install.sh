#!/bin/sh
# Installs the latest coolify-mirror release into /usr/local/bin and opens it.
#   curl -fsSL https://raw.githubusercontent.com/mtalavi/coolify-mirror/main/install.sh | sudo sh
# Arguments are passed to coolify-mirror after the install, for example the
# command the source server prints for a share:
#   ... | sudo sh -s restore 203.0.113.10/abcd-efgh-ijkl-mnop-qrst-uvwx-yz
# Pin a version with ... | sudo VERSION=v1.5.0 sh; only install with NO_RUN=1.
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

# GitHub's download servers fail now and then (HTTP 500): retry a few times.
fetch() {
  n=0
  until curl -fsSL "$1" -o "$2"; do
    n=$((n + 1))
    [ "$n" -lt 5 ] || { echo "coolify-mirror: could not download $1" >&2; exit 1; }
    echo "  retrying in 3 s…" >&2
    sleep 3
  done
}

echo "Downloading $BIN ($VERSION)…"
fetch "$BASE/$BIN" "$TMP/$BIN"
fetch "$BASE/SHA256SUMS" "$TMP/SHA256SUMS"
( cd "$TMP" && grep " $BIN\$" SHA256SUMS | sha256sum -c - >/dev/null ) || { echo "coolify-mirror: checksum mismatch, not installed" >&2; exit 1; }

install -m 0755 "$TMP/$BIN" "$DEST/coolify-mirror"
rm -rf "$TMP"
echo "Installed $("$DEST/coolify-mirror" version) to $DEST/coolify-mirror"

[ "${NO_RUN:-0}" = 1 ] && exit 0
# The script itself arrives on stdin, so the tool reads the keyboard from the
# terminal instead.
if (exec </dev/tty) 2>/dev/null; then
  exec "$DEST/coolify-mirror" "$@" </dev/tty
fi
if [ "$#" -gt 0 ]; then
  exec "$DEST/coolify-mirror" "$@"
fi
echo "Run it with:  sudo coolify-mirror"
