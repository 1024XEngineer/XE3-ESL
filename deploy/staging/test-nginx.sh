#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$staging_directory/manage.sh"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' 'docker is required for the Nginx configuration check' >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  printf '%s\n' 'openssl is required for the Nginx configuration check' >&2
  exit 1
}

temporary_directory=$(mktemp -d)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$temporary_directory/acme"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$temporary_directory/server.env"
printf '%s\n' 'staging:test-password-hash' >"$temporary_directory/staging.htpasswd"
openssl req \
  -x509 \
  -newkey rsa:2048 \
  -nodes \
  -subj '/CN=staging.speak-up.top' \
  -days 1 \
  -keyout "$temporary_directory/privkey.pem" \
  -out "$temporary_directory/fullchain.pem" \
  >/dev/null 2>&1
chmod 0600 \
  "$temporary_directory/server.env" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/privkey.pem"

printf '%s\n' \
  'STAGING_POSTGRES_DB=speakup_staging' \
  'STAGING_POSTGRES_USER=speakup_staging' \
  'STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
  'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
  "STAGING_SERVER_ENV_FILE=$temporary_directory/server.env" \
  'STAGING_PORTAL_HOST=staging.speak-up.top' \
  'STAGING_API_HOST=staging-api.speak-up.top' \
  "STAGING_TLS_CERTIFICATE=$temporary_directory/fullchain.pem" \
  "STAGING_TLS_CERTIFICATE_KEY=$temporary_directory/privkey.pem" \
  "STAGING_HTPASSWD_FILE=$temporary_directory/staging.htpasswd" \
  "STAGING_ACME_ROOT=$temporary_directory/acme" \
  >"$temporary_directory/staging.env"
chmod 0600 "$temporary_directory/staging.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/staging.env" \
  --output "$temporary_directory/default.conf" \
  >/dev/null

docker run --rm \
  --volume "$temporary_directory:$temporary_directory:ro" \
  --volume "$temporary_directory/default.conf:/etc/nginx/conf.d/default.conf:ro" \
  "$nginx_image" \
  nginx -t

printf '%s\n' 'staging Nginx configuration check passed'
