#!/bin/sh
set -e
[ -n "$SECRET_TOKEN" ] || { echo "SECRET_TOKEN missing" >&2; exit 1; }
while true; do date > /tmp/worker-alive; sleep 30; done
