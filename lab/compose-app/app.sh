#!/bin/sh
# Lab test app: refuses to run without its secret and its data, then serves
# what it sees so the restore can be compared with the source.
set -e
[ -n "$SECRET_TOKEN" ] || { echo "SECRET_TOKEN missing" >&2; exit 1; }
[ -f /data/marker.txt ] || { echo "data volume is empty (marker missing)" >&2; exit 1; }
mkdir -p /www
echo ok > /www/health
token=$(printf %s "$SECRET_TOKEN" | sha256sum | cut -c1-16)
{
  echo "app=lab-compose"
  echo "token_sha=$token"
  echo "marker=$(cat /data/marker.txt)"
  echo "rows=$(cat /data/rows.txt 2>/dev/null || echo none)"
} > /www/index.html
exec httpd -f -p 8080 -h /www
