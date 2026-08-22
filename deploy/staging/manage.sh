#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly compose_file="$staging_directory/compose.yaml"
readonly nginx_template="$staging_directory/nginx.conf.template"
readonly compose_project="xe3-speakup-staging"
readonly portal_health_url="http://127.0.0.1:28082/"
readonly server_health_url="http://127.0.0.1:28083/health"
readonly server_readiness_url="http://127.0.0.1:28083/readyz"

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh validate --manifest FILE --env-file FILE
  manage.sh deploy --manifest FILE --env-file FILE
  manage.sh verify --manifest FILE --env-file FILE
  manage.sh status --manifest FILE --env-file FILE
  manage.sh down --manifest FILE --env-file FILE
  manage.sh render-nginx --env-file FILE --output FILE
EOF
}

fail() {
  printf 'staging deploy: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

allowed_configuration_key() {
  case "$1" in
    STAGING_POSTGRES_DB | \
      STAGING_POSTGRES_USER | \
      STAGING_POSTGRES_PASSWORD | \
      PORTAL_ADMIN_PASSWORD | \
      STAGING_SERVER_ENV_FILE | \
      STAGING_PORTAL_HOST | \
      STAGING_API_HOST | \
      STAGING_TLS_CERTIFICATE | \
      STAGING_TLS_CERTIFICATE_KEY | \
      STAGING_HTPASSWD_FILE | \
      STAGING_ACME_ROOT)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

load_configuration() {
  local file=$1
  local line name value

  [[ -f "$file" ]] || fail "environment file does not exist: $file"
  [[ -r "$file" ]] || fail "environment file is not readable: $file"

  unset \
    STAGING_POSTGRES_DB \
    STAGING_POSTGRES_USER \
    STAGING_POSTGRES_PASSWORD \
    PORTAL_ADMIN_PASSWORD \
    STAGING_SERVER_ENV_FILE \
    STAGING_PORTAL_HOST \
    STAGING_API_HOST \
    STAGING_TLS_CERTIFICATE \
    STAGING_TLS_CERTIFICATE_KEY \
    STAGING_HTPASSWD_FILE \
    STAGING_ACME_ROOT

  while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line%$'\r'}
    case "$line" in
      "" | \#*)
        continue
        ;;
    esac
    [[ "$line" == *=* ]] || fail "invalid environment line: $line"
    name=${line%%=*}
    value=${line#*=}
    [[ "$name" =~ ^[A-Z][A-Z0-9_]*$ ]] || fail "invalid environment key: $name"
    allowed_configuration_key "$name" || fail "unsupported environment key: $name"
    printf -v "$name" '%s' "$value"
    export "$name"
  done <"$file"
}

require_value() {
  local name=$1
  [[ -n "${!name:-}" ]] || fail "$name is required"
}

valid_hostname() {
  local value=$1
  local label
  local -a labels

  [[ ${#value} -le 253 ]] || return 1
  [[ "$value" != *..* ]] || return 1
  IFS='.' read -r -a labels <<<"$value"
  ((${#labels[@]} >= 2)) || return 1
  for label in "${labels[@]}"; do
    [[ "$label" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]] || return 1
  done
}

valid_absolute_path() {
  local value=$1
  [[ "$value" =~ ^/[A-Za-z0-9._/-]+$ ]] &&
    [[ "$value" != *//* ]] &&
    [[ "$value" != */../* ]] &&
    [[ "$value" != */./* ]] &&
    [[ "$value" != */.. ]] &&
    [[ "$value" != */. ]]
}

validate_configuration() {
  local name
  local required=(
    STAGING_POSTGRES_DB
    STAGING_POSTGRES_USER
    STAGING_POSTGRES_PASSWORD
    PORTAL_ADMIN_PASSWORD
    STAGING_SERVER_ENV_FILE
    STAGING_PORTAL_HOST
    STAGING_API_HOST
    STAGING_TLS_CERTIFICATE
    STAGING_TLS_CERTIFICATE_KEY
    STAGING_HTPASSWD_FILE
    STAGING_ACME_ROOT
  )
  for name in "${required[@]}"; do
    require_value "$name"
  done

  [[ "$STAGING_POSTGRES_DB" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] ||
    fail "STAGING_POSTGRES_DB must be a lowercase PostgreSQL identifier"
  [[ "$STAGING_POSTGRES_USER" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] ||
    fail "STAGING_POSTGRES_USER must be a lowercase PostgreSQL identifier"
  [[ "$STAGING_POSTGRES_PASSWORD" =~ ^[A-Za-z0-9._~-]{24,}$ ]] ||
    fail "STAGING_POSTGRES_PASSWORD must be at least 24 URL-safe characters"
  [[ ${#PORTAL_ADMIN_PASSWORD} -ge 16 ]] ||
    fail "PORTAL_ADMIN_PASSWORD must be at least 16 characters"

  valid_hostname "$STAGING_PORTAL_HOST" ||
    fail "STAGING_PORTAL_HOST must be a lowercase DNS hostname"
  valid_hostname "$STAGING_API_HOST" ||
    fail "STAGING_API_HOST must be a lowercase DNS hostname"
  [[ "$STAGING_PORTAL_HOST" != "$STAGING_API_HOST" ]] ||
    fail "Staging Portal and API hostnames must be different"

  for name in \
    STAGING_SERVER_ENV_FILE \
    STAGING_TLS_CERTIFICATE \
    STAGING_TLS_CERTIFICATE_KEY \
    STAGING_HTPASSWD_FILE \
    STAGING_ACME_ROOT; do
    valid_absolute_path "${!name}" || fail "$name must be a safe absolute path"
  done

  [[ -s "$STAGING_SERVER_ENV_FILE" ]] ||
    fail "server environment file is missing or empty: $STAGING_SERVER_ENV_FILE"
}

validate_manifest() {
  local file=$1
  local digest hexadecimal

  require_command jq
  [[ -f "$file" ]] || fail "release manifest does not exist: $file"
  [[ -r "$file" ]] || fail "release manifest is not readable: $file"

  jq --exit-status '
    type == "object" and
    .manifest_version == 1 and
    (.version | type == "string" and
      test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")) and
    (.git_sha | type == "string" and test("^[0-9a-f]{40}$")) and
    .portal_image == "ghcr.io/1024xengineer/xe3-esl-portal" and
    (.portal_image_digest | type == "string" and
      test("^sha256:[0-9a-f]{64}$")) and
    .server_image == "ghcr.io/1024xengineer/xe3-esl-server" and
    (.server_image_digest | type == "string" and
      test("^sha256:[0-9a-f]{64}$")) and
    (.database_schema_version | type == "number" and
      . >= 1 and . == floor)
  ' "$file" >/dev/null || fail "release manifest is invalid"

  for digest in \
    "$(jq --raw-output '.portal_image_digest' "$file")" \
    "$(jq --raw-output '.server_image_digest' "$file")"; do
    hexadecimal=${digest#sha256:}
    [[ "$hexadecimal" =~ [1-9a-f] ]] || fail "image digest cannot be a placeholder"
  done
  [[ "$(jq --raw-output '.git_sha' "$file")" =~ [1-9a-f] ]] ||
    fail "git_sha cannot be a placeholder"

  PORTAL_IMAGE_DIGEST=$(jq --raw-output '.portal_image_digest' "$file")
  SERVER_IMAGE_DIGEST=$(jq --raw-output '.server_image_digest' "$file")
  RELEASE_VERSION=$(jq --raw-output '.version' "$file")
  RELEASE_GIT_SHA=$(jq --raw-output '.git_sha' "$file")
  RELEASE_SCHEMA_VERSION=$(jq --raw-output '.database_schema_version' "$file")
  export PORTAL_IMAGE_DIGEST SERVER_IMAGE_DIGEST
}

compose() {
  docker compose \
    --env-file /dev/null \
    --project-name "$compose_project" \
    --project-directory "$staging_directory" \
    --file "$compose_file" \
    "$@"
}

validate_compose() {
  require_command docker
  docker compose version >/dev/null
  compose --profile migration config --quiet
}

render_nginx() {
  local output=$1
  local temporary

  [[ -d "$(dirname "$output")" ]] || fail "Nginx output directory does not exist"
  temporary=$(mktemp "${output}.tmp.XXXXXX")
  if ! sed \
    -e "s|__STAGING_PORTAL_HOST__|$STAGING_PORTAL_HOST|g" \
    -e "s|__STAGING_API_HOST__|$STAGING_API_HOST|g" \
    -e "s|__STAGING_TLS_CERTIFICATE__|$STAGING_TLS_CERTIFICATE|g" \
    -e "s|__STAGING_TLS_CERTIFICATE_KEY__|$STAGING_TLS_CERTIFICATE_KEY|g" \
    -e "s|__STAGING_HTPASSWD_FILE__|$STAGING_HTPASSWD_FILE|g" \
    -e "s|__STAGING_ACME_ROOT__|$STAGING_ACME_ROOT|g" \
    "$nginx_template" >"$temporary"; then
    rm -f "$temporary"
    fail "failed to render Nginx template"
  fi
  if grep -q '__STAGING_[A-Z_]*__' "$temporary"; then
    rm -f "$temporary"
    fail "Nginx template contains an unresolved placeholder"
  fi
  chmod 0644 "$temporary"
  mv -f "$temporary" "$output"
}

wait_for_endpoint() {
  local name=$1
  local url=$2
  local attempt

  for attempt in {1..30}; do
    if curl --fail --silent --max-time 3 "$url" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  curl --fail --show-error --max-time 3 "$url" >/dev/null || true
  fail "$name did not become healthy: $url"
}

verify_endpoints() {
  require_command curl
  wait_for_endpoint "Portal" "$portal_health_url"
  wait_for_endpoint "Server liveness" "$server_health_url"
  wait_for_endpoint "Server readiness" "$server_readiness_url"
}

validate_all() {
  local manifest=$1
  local rendered

  validate_configuration
  validate_manifest "$manifest"
  validate_compose
  rendered=$(mktemp)
  render_nginx "$rendered"
  rm -f "$rendered"
}

main() {
  local command=${1:-}
  local manifest=""
  local environment_file=""
  local output=""

  [[ -n "$command" ]] || {
    usage
    exit 2
  }
  shift
  while (($# > 0)); do
    case "$1" in
      --manifest)
        (($# >= 2)) || fail "--manifest requires a value"
        manifest=$2
        shift 2
        ;;
      --env-file)
        (($# >= 2)) || fail "--env-file requires a value"
        environment_file=$2
        shift 2
        ;;
      --output)
        (($# >= 2)) || fail "--output requires a value"
        output=$2
        shift 2
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done

  [[ -n "$environment_file" ]] || fail "--env-file is required"
  load_configuration "$environment_file"

  case "$command" in
    render-nginx)
      [[ -z "$manifest" ]] || fail "render-nginx does not accept --manifest"
      [[ -n "$output" ]] || fail "render-nginx requires --output"
      validate_configuration
      render_nginx "$output"
      printf 'rendered=%s\n' "$output"
      ;;
    validate | deploy | verify | status | down)
      [[ -n "$manifest" ]] || fail "$command requires --manifest"
      [[ -z "$output" ]] || fail "$command does not accept --output"
      validate_all "$manifest"
      case "$command" in
        validate)
          printf 'version=%s git_sha=%s schema=%s validated=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA" "$RELEASE_SCHEMA_VERSION"
          ;;
        deploy)
          compose pull postgres migrate server portal
          compose up --detach --no-build --wait --wait-timeout 90 postgres
          compose run --rm --no-deps migrate
          compose up --detach --no-build --wait --wait-timeout 90 portal server
          verify_endpoints
          printf 'version=%s git_sha=%s deployed=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA"
          ;;
        verify)
          verify_endpoints
          printf 'version=%s verified=true\n' "$RELEASE_VERSION"
          ;;
        status)
          compose ps
          ;;
        down)
          compose down --remove-orphans
          ;;
      esac
      ;;
    *)
      usage
      exit 2
      ;;
  esac
}

main "$@"
