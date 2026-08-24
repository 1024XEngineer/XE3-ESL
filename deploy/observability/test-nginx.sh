#!/usr/bin/env bash

set -euo pipefail

readonly observability_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly nginx_configuration="$observability_directory/monitor-nginx.conf"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"

fail() {
  printf 'observability Nginx test: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v openssl >/dev/null 2>&1 || fail 'openssl is required'
docker version >/dev/null || fail 'Docker Engine is unavailable'

temporary_directory=$(mktemp -d /tmp/xe3-observability-nginx-test.XXXXXX)
trap 'rm -rf "$temporary_directory"' EXIT
certificate_directory="$temporary_directory/certbot/conf/live/monitor.speak-up.top"
mkdir -p "$certificate_directory" "$temporary_directory/acme" "$temporary_directory/logs"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj /CN=monitor.speak-up.top \
  -addext subjectAltName=DNS:monitor.speak-up.top \
  -keyout "$certificate_directory/privkey.pem" \
  -out "$certificate_directory/fullchain.pem" >/dev/null 2>&1
chmod 0600 "$certificate_directory/privkey.pem"

docker run \
  --rm \
  --name "xe3-observability-nginx-test-$$" \
  --read-only \
  --tmpfs /var/cache/nginx:rw,nosuid,nodev,noexec,size=8m \
  --tmpfs /var/run:rw,nosuid,nodev,noexec,size=1m \
  --volume "$nginx_configuration:/etc/nginx/conf.d/default.conf:ro" \
  --volume "$certificate_directory:/opt/xe3-speakup-portal/certbot/conf/live/monitor.speak-up.top:ro" \
  --volume "$temporary_directory/acme:/opt/xe3-speakup-portal/certbot/www:ro" \
  --volume "$temporary_directory/logs:/etc/nginx/logs" \
  "$nginx_image" \
  nginx -t

printf '%s\n' 'Observability Nginx configuration passed'
