#!/usr/bin/env bash

set -euo pipefail

readonly tls_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$tls_directory/manage.sh"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"

fail() {
  printf 'TLS bootstrap Nginx test: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker version >/dev/null || fail "Docker Engine is unavailable"

temporary_directory=$(mktemp -d /tmp/xe3-tls-nginx-test.XXXXXX)
trap 'rm -rf "$temporary_directory"' EXIT
mkdir -p \
  "$temporary_directory/certbot" \
  "$temporary_directory/staging-acme" \
  "$temporary_directory/production-acme" \
  "$temporary_directory/state"
chmod 0700 "$temporary_directory/certbot" "$temporary_directory/state"
chmod 0755 "$temporary_directory/staging-acme" "$temporary_directory/production-acme"
printf '%s\n' '#!/usr/bin/env bash' 'exit 0' >"$temporary_directory/nginx"
chmod 0755 "$temporary_directory/nginx"
printf '%s\n' \
  "TLS_CERTBOT_CONFIG_ROOT=$temporary_directory/certbot" \
  "TLS_STAGING_ACME_ROOT=$temporary_directory/staging-acme" \
  "TLS_PRODUCTION_ACME_ROOT=$temporary_directory/production-acme" \
  "TLS_STATE_ROOT=$temporary_directory/state" \
  "TLS_NGINX_BINARY=$temporary_directory/nginx" \
  >"$temporary_directory/tls.env"
chmod 0600 "$temporary_directory/tls.env"

for environment in staging production; do
  rendered="$temporary_directory/$environment.conf"
  "$manage" render-bootstrap \
    --environment "$environment" \
    --env-file "$temporary_directory/tls.env" \
    --output "$rendered" >/dev/null
  if grep -Eq '__TLS_[A-Z_]+__' "$rendered"; then
    fail "$environment bootstrap has an unresolved placeholder"
  fi
  docker run \
    --rm \
    --name "xe3-tls-nginx-$environment-$$" \
    --read-only \
    --tmpfs /var/cache/nginx:rw,nosuid,nodev,noexec,size=8m \
    --tmpfs /var/run:rw,nosuid,nodev,noexec,size=1m \
    --volume "$rendered:/etc/nginx/conf.d/default.conf:ro" \
    "$nginx_image" \
    nginx -t
done

printf '%s\n' 'TLS bootstrap Nginx checks passed'
