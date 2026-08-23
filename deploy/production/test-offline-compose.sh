#!/usr/bin/env bash

set -euo pipefail

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly compose_file="$production_directory/compose.yaml"
readonly offline_compose_file="$production_directory/compose.offline.yaml"
readonly target_services='["migrate", "portal", "postgres", "server"]'

fail() {
  printf 'production offline Compose test: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker compose version >/dev/null 2>&1 || fail "docker compose is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"

temporary_directory=$(mktemp -d /tmp/production-offline-compose-test.XXXXXX)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

readonly server_environment="$temporary_directory/server.env"
readonly base_model="$temporary_directory/base.json"
readonly offline_model="$temporary_directory/offline.json"

printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' > "$server_environment"
chmod 0600 "$server_environment"

export PRODUCTION_POSTGRES_DB=speakup_production
export PRODUCTION_POSTGRES_USER=speakup_production
export PRODUCTION_POSTGRES_PASSWORD=production-postgres-password-for-tests
export PRODUCTION_SERVER_ENV_FILE="$server_environment"
export PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=172.31.253.1/32
export PRODUCTION_PORTAL_HOST=speak-up.top
export PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests
export SERVER_IMAGE_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export PORTAL_IMAGE_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

docker compose \
  --file "$compose_file" \
  --profile migration \
  config \
  --format json > "$base_model"
docker compose \
  --file "$compose_file" \
  --file "$offline_compose_file" \
  --profile migration \
  config \
  --format json > "$offline_model"

jq --exit-status \
  --argjson services "$target_services" '
    . as $model
    | all($services[]; $model.services[.].pull_policy == "always")
  ' "$base_model" >/dev/null ||
  fail "base Compose model must use pull_policy=always for the four target services"

jq --exit-status \
  --argjson services "$target_services" '
    . as $model
    | all($services[]; $model.services[.].pull_policy == "never")
  ' "$offline_model" >/dev/null ||
  fail "offline Compose model must use pull_policy=never for the four target services"

jq --exit-status \
  --argjson services "$target_services" \
  --slurpfile offline "$offline_model" '
    . as $base
    | ($offline[0] | reduce $services[] as $service (
        .;
        .services[$service].pull_policy = "always"
      )) == $base
  ' "$base_model" >/dev/null ||
  fail "offline override changed fields other than the four pull policies"

printf '%s\n' 'Production offline Compose contract tests passed'
