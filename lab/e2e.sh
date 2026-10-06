#!/usr/bin/env bash
# End-to-end compatibility test: two fresh servers (containers with systemd and
# their own Docker) get the official Coolify of the given version; the source
# gets real resources; coolify-mirror backs them up, shares them with a share
# code and restores them on the target; then the data and the running state
# are compared. Exit code 0 = compatible.
#
#   lab/e2e.sh COOLIFY_VERSION path/to/coolify-mirror-linux-amd64
#
# Needs Docker on the host (privileged containers). KEEP=1 keeps the servers.
set -euo pipefail
V="${1:?Coolify version, e.g. 4.3.23}"
BIN="$(cd "$(dirname "${2:?coolify-mirror binary}")" && pwd)/$(basename "$2")"
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${E2E_REPO:-https://github.com/mtalavi/coolify-mirror}"
BRANCH="${E2E_BRANCH:-main}"
NET=cm-e2e
SRC=cm-e2e-src
DST=cm-e2e-dst

log() { printf '\n=== %s\n' "$*"; }
on() { local c=$1; shift; docker exec "$c" bash -c "$*"; }

cleanup() {
  [ "${KEEP:-0}" = 1 ] && return
  docker rm -f "$SRC" "$DST" >/dev/null 2>&1 || true
  for c in "$SRC" "$DST"; do docker volume rm "$c-docker" "$c-containerd" "$c-data" >/dev/null 2>&1 || true; done
  docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
# E2E_REUSE=1 (with KEEP=1 before): use the servers of the last run as they are.
if [ "${E2E_REUSE:-0}" = 1 ] && docker inspect "$SRC" "$DST" >/dev/null 2>&1; then
  SKIP_INSTALL=1
else
  SKIP_INSTALL=0
  cleanup
fi

if [ "$SKIP_INSTALL" = 0 ]; then
log "build the server image"
docker build -q -t cm-e2e-host "$HERE" >/dev/null
docker network create "$NET" >/dev/null
for c in "$SRC" "$DST"; do
  docker run -d --name "$c" --hostname "$c" --network "$NET" --privileged --cgroupns=private \
    --tmpfs /run --tmpfs /run/lock -v "$c-docker:/var/lib/docker" -v "$c-containerd:/var/lib/containerd" \
    -v "$c-data:/data" cm-e2e-host >/dev/null
done

log "install Coolify $V on both servers"
install() {
  on "$1" 'for i in $(seq 1 60); do systemctl is-system-running >/dev/null 2>&1 && break; [ "$(systemctl is-system-running 2>/dev/null)" = degraded ] && break; sleep 1; done; rm -f /run/nologin'
  on "$1" "curl -fsSL https://cdn.coollabs.io/coolify/install.sh -o /root/coolify-install.sh && USER=root bash /root/coolify-install.sh $V" >"/tmp/$1-install.log" 2>&1
  for i in $(seq 1 120); do
    on "$1" 'curl -fsS http://localhost:8000/api/health' 2>/dev/null | grep -q OK && return 0
    sleep 5
  done
  echo "Coolify did not come up on $1"; tail -40 "/tmp/$1-install.log"; return 1
}
install "$SRC" & p1=$!
install "$DST" & p2=$!
wait $p1; wait $p2
for c in "$SRC" "$DST"; do
  got=$(on "$c" "docker inspect -f '{{.Config.Image}}' coolify")
  echo "$c: $got"
  case "$got" in *":$V") ;; *) echo "expected Coolify $V"; exit 1 ;; esac
done
fi

log "seed the source"
docker cp "$HERE/bootstrap.php" "$SRC:/root/bootstrap.php"
docker cp "$HERE/e2e_compose.php" "$SRC:/root/e2e_compose.php"
on "$SRC" 'docker cp /root/bootstrap.php coolify:/tmp/bootstrap.php && docker cp /root/e2e_compose.php coolify:/tmp/e2e_compose.php'
TOKEN=""
for i in $(seq 1 30); do
  out=$(on "$SRC" 'docker exec -e LAB_PASSWORD=e2e-Pa55-word coolify php /tmp/bootstrap.php' 2>&1 || true)
  TOKEN=$(echo "$out" | sed -n 's/^TOKEN=//p')
  [ -n "$TOKEN" ] && break
  sleep 10
done
[ -n "$TOKEN" ] || { echo "$out" | tail -20; echo "no API token"; exit 1; }
docker cp "$HERE/e2e_seed.sh" "$SRC:/root/e2e_seed.sh"
eval "$(on "$SRC" "bash /root/e2e_seed.sh '$TOKEN' '$REPO' '$BRANCH'" | grep -E '^(COMPOSE|PG|WEB)=')"
echo "compose=$COMPOSE pg=$PG web=$WEB"

log "wait until everything runs on the source"
# The compose test app refuses to start without data in its volume.
CMARK="compose-$(date +%s)-$RANDOM"
for i in $(seq 1 180); do
  on "$SRC" "docker exec \$(docker ps -q --filter name=^worker-$COMPOSE) sh -c 'echo $CMARK > /data/marker.txt'" 2>/dev/null && break
  sleep 5
done
for i in $(seq 1 180); do
  up=$(on "$SRC" "docker ps --format '{{.Names}} {{.Status}}'" || true)
  if echo "$up" | grep -q "^web-$COMPOSE.*healthy" && echo "$up" | grep -q "^$PG .*healthy" && echo "$up" | grep -q "^$WEB"; then break; fi
  [ "$i" = 180 ] && { echo "$up"; echo "source resources did not start"; exit 1; }
  sleep 5
done
MARK="e2e-$(date +%s)-$RANDOM"
on "$SRC" "docker exec \$(docker ps -q --filter name=^$WEB) sh -c 'echo $MARK > /usr/share/nginx/html/marker.txt'"
on "$SRC" "docker exec $PG psql -U e2e -d e2e -v ON_ERROR_STOP=1 -c 'create table t(x int); insert into t select generate_series(1,1234)'" >/dev/null

log "back up and share on the source"
docker cp "$BIN" "$SRC:/usr/local/bin/coolify-mirror"
docker cp "$BIN" "$DST:/usr/local/bin/coolify-mirror"
on "$SRC" "nohup coolify-mirror backup --all --serve --mode direct --host $SRC >/root/share.out 2>&1 &"
for i in $(seq 1 120); do
  CODE=$(on "$SRC" "sed -n 's/^  Share code: \([^ ]*\).*/\1/p' /root/share.out" || true)
  [ -n "$CODE" ] && break
  on "$SRC" 'grep -q ERROR /root/share.out' && { on "$SRC" 'cat /root/share.out'; exit 1; }
  sleep 3
done
[ -n "$CODE" ] || { on "$SRC" 'cat /root/share.out'; echo "no share code"; exit 1; }
on "$SRC" 'grep -E "^  ! " /root/share.out' || true
echo "share code: $CODE"

log "restore on the target with the share code"
set +e
on "$DST" "coolify-mirror restore $CODE --yes" | tee /tmp/cm-e2e-restore.log
rc=${PIPESTATUS[0]}
set -e
[ "$rc" = 0 ] || { echo "restore failed (exit $rc)"; exit 1; }
grep -q '^SUCCESS' /tmp/cm-e2e-restore.log || { echo "no SUCCESS"; exit 1; }
grep -q 'started from the restored images' /tmp/cm-e2e-restore.log || { echo "the compose app was not started from the restored images"; exit 1; }

log "compare data on the target"
fail=0
got=$(on "$DST" "curl -fsS --resolve nginx.e2e.test:80:127.0.0.1 http://nginx.e2e.test/marker.txt" || true)
[ "$got" = "$MARK" ] && echo "ok  volume file through the proxy" || { echo "BAD volume file: '$got' != '$MARK'"; fail=1; }
got=$(on "$DST" "docker exec $PG psql -U e2e -d e2e -At -c 'select count(*) from t'" || true)
[ "$got" = 1234 ] && echo "ok  database rows" || { echo "BAD database rows: $got"; fail=1; }
got=$(on "$DST" "docker exec \$(docker ps -q --filter name=^web-$COMPOSE) printenv SECRET_TOKEN" || true)
[ "$got" = e2e-secret-token-42 ] && echo "ok  compose app secret" || { echo "BAD compose secret: $got"; fail=1; }
got=$(on "$DST" "curl -fsS --resolve web.e2e.test:80:127.0.0.1 http://web.e2e.test/" || true)
echo "$got" | grep -q "marker=$CMARK" && echo "ok  compose app volume data through the proxy" || { echo "BAD compose app: '$got'"; fail=1; }
[ "$fail" = 0 ] || exit 1
echo
echo "COMPATIBLE: coolify-mirror $("$BIN" version 2>/dev/null | awk '{print $2}') with Coolify $V"
