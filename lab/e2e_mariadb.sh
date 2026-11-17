#!/usr/bin/env bash
# Runs inside an e2e server: a MariaDB account added after the first start
# (it lives only in the mysql schema, not in the image's environment) must
# arrive with its password and grants, next to the rows it may read.
#   e2e_mariadb.sh CONTAINER seed|verify
set -euo pipefail
C=$1
sql() { docker exec "$C" sh -c 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb -uroot -N -B -e "$1"' sh "$1"; }
for i in $(seq 1 60); do
  sql 'select 1' >/dev/null 2>&1 && break
  [ "$i" = 60 ] && { echo "MariaDB $C is not ready"; docker logs --tail 20 "$C" 2>&1; exit 1; }
  sleep 5
done
case "$2" in
seed)
  sql "CREATE TABLE e2e.notes (id int PRIMARY KEY, v varchar(40)); INSERT INTO e2e.notes VALUES (1,'one'),(2,'two'),(3,'three');
       CREATE USER 'reporter'@'%' IDENTIFIED BY 'Rep-0rt-Pa55'; GRANT SELECT ON e2e.* TO 'reporter'@'%';"
  ;;
verify)
  got=$(docker exec "$C" sh -c 'MYSQL_PWD=Rep-0rt-Pa55 mariadb -ureporter -h127.0.0.1 -N -B -e "SELECT group_concat(v ORDER BY id) FROM e2e.notes"' 2>&1 || true)
  [ "$got" = "one,two,three" ] || { echo "reporter account on $C: $got"; exit 1; }
  ;;
*) echo "seed or verify" >&2; exit 2 ;;
esac
