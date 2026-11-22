#!/bin/sh
# Lab test app for a raw compose deployment: serves the address it was given
# in its env file on the server, so a domain change can be checked.
set -e
[ -n "$APP_URL" ] || { echo "APP_URL missing (env file on the server not read)" >&2; exit 1; }
mkdir -p /www
echo ok > /www/health
{
  echo "app=lab-raw"
  echo "app_url=$APP_URL"
  echo "cookie_domain=$COOKIE_DOMAIN"
} > /www/index.html
exec httpd -f -p 8080 -h /www
