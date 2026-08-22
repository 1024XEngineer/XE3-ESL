#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$staging_directory/manage.sh"
readonly compose_file="$staging_directory/compose.yaml"
readonly portal_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
readonly server_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
readonly git_sha="cccccccccccccccccccccccccccccccccccccccc"

fail() {
  printf 'staging deploy test: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local name=$1
  shift
  if "$@" >"$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

write_environment() {
  local destination=$1
  local server_environment=$2

  printf '%s\n' \
    'STAGING_POSTGRES_DB=speakup_staging' \
    'STAGING_POSTGRES_USER=speakup_staging' \
    'STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
    'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
    "STAGING_SERVER_ENV_FILE=$server_environment" \
    'STAGING_PORTAL_HOST=staging.speak-up.top' \
    'STAGING_API_HOST=staging-api.speak-up.top' \
    "STAGING_TLS_CERTIFICATE=$temporary_directory/fullchain.pem" \
    "STAGING_TLS_CERTIFICATE_KEY=$temporary_directory/privkey.pem" \
    "STAGING_HTPASSWD_FILE=$temporary_directory/staging.htpasswd" \
    "STAGING_ACME_ROOT=$temporary_directory/acme" \
    "STAGING_PUBLIC_ROOT=$temporary_directory/public" >"$destination"
}

write_manifest() {
  local destination=$1
  local selected_portal_digest=${2:-$portal_digest}

  printf '%s\n' \
    '{' \
    '  "manifest_version": 1,' \
    '  "version": "0.1.1",' \
    "  \"git_sha\": \"$git_sha\"," \
    '  "version_code": 2,' \
    '  "portal_image": "ghcr.io/1024xengineer/xe3-esl-portal",' \
    "  \"portal_image_digest\": \"$selected_portal_digest\"," \
    '  "server_image": "ghcr.io/1024xengineer/xe3-esl-server",' \
    "  \"server_image_digest\": \"$server_digest\"," \
    '  "database_schema_version": 7' \
    '}' >"$destination"
}

temporary_directory=$(mktemp -d)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p \
  "$temporary_directory/acme" \
  "$temporary_directory/fake-bin" \
  "$temporary_directory/public"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$temporary_directory/server.env"
printf '%s\n' 'test-user:test-password-hash' >"$temporary_directory/staging.htpasswd"
printf '%s\n' 'test-certificate-placeholder' >"$temporary_directory/fullchain.pem"
printf '%s\n' 'test-key-placeholder' >"$temporary_directory/privkey.pem"
write_environment "$temporary_directory/staging.env" "$temporary_directory/server.env"
write_manifest "$temporary_directory/release-manifest.json"

bash -n "$manage" "$0"

"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/staging.env" \
  >"$temporary_directory/validate.out"
grep -Fq 'validated=true' "$temporary_directory/validate.out" ||
  fail "valid deployment contract was not accepted"

chmod 0777 "$temporary_directory/public"
expect_failure "world-writable public root" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
chmod 0700 "$temporary_directory/public"

mkdir -p "$temporary_directory/public/downloads/android"
chmod 0777 "$temporary_directory/public/downloads/android"
expect_failure "world-writable Android public directory" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
chmod 0700 "$temporary_directory/public/downloads/android"
rmdir \
  "$temporary_directory/public/downloads/android" \
  "$temporary_directory/public/downloads"

write_manifest "$temporary_directory/invalid-manifest.json" 'latest'
expect_failure "mutable Portal image reference" \
  "$manage" validate \
  --manifest "$temporary_directory/invalid-manifest.json" \
  --env-file "$temporary_directory/staging.env"

write_manifest \
  "$temporary_directory/zero-digest-manifest.json" \
  'sha256:0000000000000000000000000000000000000000000000000000000000000000'
expect_failure "placeholder Portal digest" \
  "$manage" validate \
  --manifest "$temporary_directory/zero-digest-manifest.json" \
  --env-file "$temporary_directory/staging.env"

expect_failure "missing release manifest" \
  "$manage" validate \
  --manifest "$temporary_directory/missing.json" \
  --env-file "$temporary_directory/staging.env"

expect_failure "missing environment file" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/not-there.env"

sed 's/^PORTAL_ADMIN_PASSWORD=.*/PORTAL_ADMIN_PASSWORD=/' \
  "$temporary_directory/staging.env" >"$temporary_directory/missing-value.env"
expect_failure "missing required environment value" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/missing-value.env"

sed '/^PORTAL_ADMIN_PASSWORD=/d' \
  "$temporary_directory/staging.env" >"$temporary_directory/missing-key.env"
expect_failure "ambient value replacing a missing environment key" \
  env PORTAL_ADMIN_PASSWORD=ambient-password-must-not-be-used \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/missing-key.env"

printf '%s\n' \
  "$(<"$temporary_directory/staging.env")" \
  'UNKNOWN_SETTING=typo' >"$temporary_directory/unknown.env"
expect_failure "unknown environment setting" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/unknown.env"

write_environment "$temporary_directory/missing-server.env" "$temporary_directory/not-there.env"
expect_failure "missing Server environment file" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/missing-server.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/staging.env" \
  --output "$temporary_directory/staging.conf" \
  >"$temporary_directory/render.out"
if grep -Eq '__STAGING_[A-Z_]+__' "$temporary_directory/staging.conf"; then
  fail "rendered Nginx configuration contains a placeholder"
fi
[[ $(grep -Fc 'auth_basic "SpeakUp Staging";' "$temporary_directory/staging.conf") -eq 1 ]] ||
  fail "Nginx Basic Auth must apply only to the Portal host"
sed -n '/server_name staging-api.speak-up.top;/,$p' \
  "$temporary_directory/staging.conf" >"$temporary_directory/api-server.conf"
if grep -Fq 'auth_basic' "$temporary_directory/api-server.conf"; then
  fail "API host must preserve application Bearer auth without HTTP Basic Auth"
fi
grep -Fq 'proxy_set_header Authorization $http_authorization;' \
  "$temporary_directory/api-server.conf" ||
  fail "API proxy does not explicitly preserve the Authorization header"
[[ $(grep -Fc 'location = /metrics {' "$temporary_directory/staging.conf") -eq 2 ]] ||
  fail "Nginx does not block /metrics on both hosts"
grep -Fq 'proxy_pass http://127.0.0.1:28082;' "$temporary_directory/staging.conf" ||
  fail "Portal proxy is not loopback-only"
grep -Fq 'proxy_pass http://127.0.0.1:28083;' "$temporary_directory/staging.conf" ||
  fail "Server proxy is not loopback-only"

STAGING_POSTGRES_DB=speakup_staging \
STAGING_POSTGRES_USER=speakup_staging \
STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef \
PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests \
STAGING_SERVER_ENV_FILE="$temporary_directory/server.env" \
STAGING_PORTAL_HOST=staging.speak-up.top \
PORTAL_IMAGE_DIGEST="$portal_digest" \
SERVER_IMAGE_DIGEST="$server_digest" \
COMPOSE_PROJECT_NAME=xe3-speakup-production \
  docker compose \
    --env-file /dev/null \
    --project-name xe3-speakup-staging \
    --file "$compose_file" \
    --profile migration \
    config --format json >"$temporary_directory/compose.json"

jq --exit-status \
  --arg portal_digest "$portal_digest" \
  --arg server_digest "$server_digest" '
    .name == "xe3-speakup-staging" and
    .services.portal.image ==
      ("ghcr.io/1024xengineer/xe3-esl-portal@" + $portal_digest) and
    .services.server.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    .services.migrate.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    ([.services[] | has("build")] | any | not) and
    ([.services[] | has("container_name")] | any | not) and
    .services.portal.ports[0].host_ip == "127.0.0.1" and
    (.services.portal.ports[0].published | tostring) == "28082" and
    .services.server.ports[0].host_ip == "127.0.0.1" and
    (.services.server.ports[0].published | tostring) == "28083" and
    (.services.postgres | has("ports") | not) and
    .networks.database.internal == true and
    (.services.portal.networks | keys) == ["portal_edge"] and
    (.services.postgres.networks | keys) == ["database"] and
    (.services.migrate.networks | keys) == ["database"] and
    (.services.server.networks | keys | sort) == ["database", "server_edge"] and
    (.volumes | keys | sort) == ["portal_data", "postgres_data"]
  ' "$temporary_directory/compose.json" >/dev/null ||
  fail "resolved Compose model violates the isolation contract"

printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "docker %s\n" "$*" >>"$COMMAND_LOG"' \
  'if [[ "${FAIL_MIGRATION:-0}" == 1 && "$*" == *"run --rm --no-deps migrate"* ]]; then' \
  '  exit 71' \
  'fi' >"$temporary_directory/fake-bin/docker"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "curl %s\n" "$*" >>"$COMMAND_LOG"' \
  '[[ "${FAIL_HEALTH:-0}" != 1 ]]' >"$temporary_directory/fake-bin/curl"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'exit 0' >"$temporary_directory/fake-bin/sleep"
chmod +x \
  "$temporary_directory/fake-bin/docker" \
  "$temporary_directory/fake-bin/curl" \
  "$temporary_directory/fake-bin/sleep"

COMMAND_LOG="$temporary_directory/deploy.log" \
PATH="$temporary_directory/fake-bin:$PATH" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    >"$temporary_directory/deploy.out"

postgres_line=$(grep -nF 'up --detach --no-build --wait --wait-timeout 90 postgres' \
  "$temporary_directory/deploy.log" | cut -d: -f1)
migration_line=$(grep -nF 'run --rm --no-deps migrate' \
  "$temporary_directory/deploy.log" | cut -d: -f1)
application_line=$(grep -nF 'up --detach --no-build --wait --wait-timeout 90 portal server' \
  "$temporary_directory/deploy.log" | cut -d: -f1)
[[ -n "$postgres_line" && -n "$migration_line" && -n "$application_line" ]] ||
  fail "deployment steps were not recorded"
((postgres_line < migration_line && migration_line < application_line)) ||
  fail "migration did not finish before the application switch"
grep -Fq -- '--project-name xe3-speakup-staging' "$temporary_directory/deploy.log" ||
  fail "deployment does not pin the isolated Compose project name"
grep -Fq 'http://127.0.0.1:28083/health' "$temporary_directory/deploy.log" ||
  fail "deployment did not verify /health"
grep -Fq 'http://127.0.0.1:28083/readyz' "$temporary_directory/deploy.log" ||
  fail "deployment did not verify /readyz"

if COMMAND_LOG="$temporary_directory/migration-failure.log" \
  FAIL_MIGRATION=1 \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    >"$temporary_directory/migration-failure.out" 2>&1; then
  fail "failed migration unexpectedly succeeded"
fi
if grep -Fq 'up --detach --no-build --wait --wait-timeout 90 portal server' \
  "$temporary_directory/migration-failure.log"; then
  fail "application switched after migration failure"
fi

if COMMAND_LOG="$temporary_directory/health-failure.log" \
  FAIL_HEALTH=1 \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    >"$temporary_directory/health-failure.out" 2>&1; then
  fail "failed health verification unexpectedly succeeded"
fi
grep -Fq 'did not become healthy' "$temporary_directory/health-failure.out" ||
  fail "health verification failure was not reported explicitly"

printf '%s\n' 'staging deploy contract tests passed'
