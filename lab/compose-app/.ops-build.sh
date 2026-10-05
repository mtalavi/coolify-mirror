#!/usr/bin/env bash
# Deployment-only launcher: reads the build policy that lives on the host.
set -Eeuo pipefail
TMP="$(mktemp /tmp/ops-build.XXXXXX)"
trap 'rm -f -- "$TMP"' EXIT
docker run --rm --network none --mount type=bind,src=/data/coolify/ops/build-policy.sh,dst=/ops-build.sh,readonly \
  --entrypoint /bin/cat alpine:3.20 /ops-build.sh > "$TMP"
bash "$TMP" "$@"
