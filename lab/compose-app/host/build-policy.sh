#!/usr/bin/env bash
# Host build policy (lab): builds only through the bounded buildx builder.
set -Eeuo pipefail
[[ "${1:-}" == docker && "${2:-}" == compose ]] || { echo 'Expected docker compose command' >&2; exit 64; }
docker buildx inspect --bootstrap cm-bounded >/dev/null
echo "BUILD_POLICY=bounded"
exec "$@"
