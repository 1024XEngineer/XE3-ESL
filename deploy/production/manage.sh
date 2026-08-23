#!/usr/bin/env bash

set -euo pipefail

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly compose_file="$production_directory/compose.yaml"
readonly nginx_template="$production_directory/nginx.conf.template"
readonly compose_project="xe3-speakup-production"
readonly portal_data_volume="xe3-speakup-portal-data"
readonly postgres_data_volume="xe3-speakup-postgres-data"
readonly server_edge_network="xe3-speakup-production-server-edge"
readonly portal_image_repository="ghcr.io/1024xengineer/xe3-esl-portal"
readonly server_image_repository="ghcr.io/1024xengineer/xe3-esl-server"
readonly postgres_image="postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108"
readonly portal_health_url="http://127.0.0.1:18082/"
readonly server_health_url="http://127.0.0.1:18083/health"
readonly server_readiness_url="http://127.0.0.1:18083/readyz"

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh validate --manifest FILE --env-file FILE
  manage.sh verify --manifest FILE --env-file FILE
  manage.sh status --manifest FILE --env-file FILE
  manage.sh render-nginx --env-file FILE --output FILE
EOF
}

fail() {
  printf 'production contract: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_regular_file() {
  local description=$1
  local file=$2

  [[ -f "$file" ]] || fail "$description is not a regular file: $file"
  [[ -r "$file" ]] || fail "$description is not readable: $file"
  [[ -s "$file" ]] || fail "$description is empty: $file"
}

require_private_file() {
  local description=$1
  local file=$2
  local mode

  [[ ! -L "$file" ]] || fail "$description must not be a symbolic link: $file"
  require_regular_file "$description" "$file"
  if mode=$(stat -c '%a' -- "$file" 2>/dev/null); then
    :
  elif mode=$(stat -f '%Lp' "$file" 2>/dev/null); then
    :
  else
    fail "cannot inspect permissions for $description: $file"
  fi
  case "$mode" in
    400 | 600)
      ;;
    *)
      fail "$description must have mode 0400 or 0600: $file"
      ;;
  esac
}

require_private_tls_key() {
  local description=$1
  local file=$2
  local target=$file
  local mode owner current_uid

  if [[ -L "$file" ]]; then
    require_command realpath
    target=$(realpath "$file" 2>/dev/null) ||
      fail "cannot resolve $description symbolic link: $file"
  fi

  [[ ! -L "$target" ]] || fail "$description does not resolve to a regular file: $file"
  require_regular_file "$description" "$target"

  if mode=$(stat -c '%a' -- "$target" 2>/dev/null); then
    :
  elif mode=$(stat -f '%Lp' "$target" 2>/dev/null); then
    :
  else
    fail "cannot inspect permissions for $description: $file"
  fi
  case "$mode" in
    400 | 600)
      ;;
    *)
      fail "$description target must have mode 0400 or 0600: $file"
      ;;
  esac

  if owner=$(stat -c '%u' -- "$target" 2>/dev/null); then
    :
  elif owner=$(stat -f '%u' "$target" 2>/dev/null); then
    :
  else
    fail "cannot inspect owner for $description: $file"
  fi
  current_uid=$(id -u) || fail "cannot determine the current user for $description"
  [[ "$owner" == "$current_uid" ]] ||
    fail "$description target must be owned by the current user: $file"
}

require_owned_public_directory() {
  local description=$1
  local directory=$2
  local owner mode

  [[ ! -L "$directory" && -d "$directory" ]] ||
    fail "$description must be a real directory: $directory"
  if owner=$(stat -c '%u' -- "$directory" 2>/dev/null); then
    mode=$(stat -c '%a' -- "$directory") ||
      fail "cannot inspect permissions for $description: $directory"
  elif owner=$(stat -f '%u' "$directory" 2>/dev/null); then
    mode=$(stat -f '%Lp' "$directory") ||
      fail "cannot inspect permissions for $description: $directory"
  else
    fail "cannot inspect ownership for $description: $directory"
  fi
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current user: $directory"
  (( (8#$mode & 0022) == 0 )) ||
    fail "$description cannot be group or world writable: $directory"
}

allowed_configuration_key() {
  case "$1" in
    PRODUCTION_POSTGRES_DB | \
      PRODUCTION_POSTGRES_USER | \
      PRODUCTION_POSTGRES_PASSWORD | \
      PORTAL_ADMIN_PASSWORD | \
      PRODUCTION_SERVER_ENV_FILE | \
      PRODUCTION_SERVER_EDGE_GATEWAY_CIDR | \
      PRODUCTION_PORTAL_HOST | \
      PRODUCTION_PORTAL_REDIRECT_HOST | \
      PRODUCTION_API_HOST | \
      PRODUCTION_TLS_CERTIFICATE | \
      PRODUCTION_TLS_CERTIFICATE_KEY | \
      PRODUCTION_ACME_ROOT | \
      PRODUCTION_PUBLIC_ROOT)
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

  require_private_file "environment file" "$file"

  unset \
    PRODUCTION_POSTGRES_DB \
    PRODUCTION_POSTGRES_USER \
    PRODUCTION_POSTGRES_PASSWORD \
    PORTAL_ADMIN_PASSWORD \
    PRODUCTION_SERVER_ENV_FILE \
    PRODUCTION_SERVER_EDGE_GATEWAY_CIDR \
    PRODUCTION_PORTAL_HOST \
    PRODUCTION_PORTAL_REDIRECT_HOST \
    PRODUCTION_API_HOST \
    PRODUCTION_TLS_CERTIFICATE \
    PRODUCTION_TLS_CERTIFICATE_KEY \
    PRODUCTION_ACME_ROOT \
    PRODUCTION_PUBLIC_ROOT

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
  done < "$file"
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
  IFS='.' read -r -a labels <<< "$value"
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

valid_ipv4_gateway_cidr() {
  local value=$1
  local address octet
  local -a octets

  [[ "$value" == */32 ]] || return 1
  address=${value%/32}
  IFS='.' read -r -a octets <<<"$address"
  ((${#octets[@]} == 4)) || return 1
  for octet in "${octets[@]}"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] || return 1
    ((10#$octet <= 255)) || return 1
  done
}

validate_configuration() {
  local name
  local required=(
    PRODUCTION_POSTGRES_DB
    PRODUCTION_POSTGRES_USER
    PRODUCTION_POSTGRES_PASSWORD
    PORTAL_ADMIN_PASSWORD
    PRODUCTION_SERVER_ENV_FILE
    PRODUCTION_SERVER_EDGE_GATEWAY_CIDR
    PRODUCTION_PORTAL_HOST
    PRODUCTION_PORTAL_REDIRECT_HOST
    PRODUCTION_API_HOST
    PRODUCTION_TLS_CERTIFICATE
    PRODUCTION_TLS_CERTIFICATE_KEY
    PRODUCTION_ACME_ROOT
    PRODUCTION_PUBLIC_ROOT
  )
  for name in "${required[@]}"; do
    require_value "$name"
  done

  [[ "$PRODUCTION_POSTGRES_DB" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] ||
    fail "PRODUCTION_POSTGRES_DB must be a lowercase PostgreSQL identifier"
  [[ "$PRODUCTION_POSTGRES_USER" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] ||
    fail "PRODUCTION_POSTGRES_USER must be a lowercase PostgreSQL identifier"
  [[ "$PRODUCTION_POSTGRES_PASSWORD" =~ ^[A-Za-z0-9._~-]{24,}$ ]] ||
    fail "PRODUCTION_POSTGRES_PASSWORD must be at least 24 URL-safe characters"
  [[ ${#PORTAL_ADMIN_PASSWORD} -ge 16 ]] ||
    fail "PORTAL_ADMIN_PASSWORD must be at least 16 characters"
  valid_ipv4_gateway_cidr "$PRODUCTION_SERVER_EDGE_GATEWAY_CIDR" ||
    fail "PRODUCTION_SERVER_EDGE_GATEWAY_CIDR must be a canonical IPv4 gateway/32"

  for name in \
    PRODUCTION_PORTAL_HOST \
    PRODUCTION_PORTAL_REDIRECT_HOST \
    PRODUCTION_API_HOST; do
    valid_hostname "${!name}" || fail "$name must be a lowercase DNS hostname"
  done
  [[ "$PRODUCTION_PORTAL_HOST" != "$PRODUCTION_PORTAL_REDIRECT_HOST" ]] ||
    fail "Production Portal hosts must be different"
  [[ "$PRODUCTION_PORTAL_HOST" != "$PRODUCTION_API_HOST" ]] ||
    fail "Production Portal and API hosts must be different"
  [[ "$PRODUCTION_PORTAL_REDIRECT_HOST" != "$PRODUCTION_API_HOST" ]] ||
    fail "Production redirect and API hosts must be different"

  for name in \
    PRODUCTION_SERVER_ENV_FILE \
    PRODUCTION_TLS_CERTIFICATE \
    PRODUCTION_TLS_CERTIFICATE_KEY \
    PRODUCTION_ACME_ROOT \
    PRODUCTION_PUBLIC_ROOT; do
    valid_absolute_path "${!name}" || fail "$name must be a safe absolute path"
  done

  require_private_file "server environment file" "$PRODUCTION_SERVER_ENV_FILE"
  require_regular_file "TLS certificate" "$PRODUCTION_TLS_CERTIFICATE"
  require_private_tls_key "TLS certificate key" "$PRODUCTION_TLS_CERTIFICATE_KEY"
  [[ -d "$PRODUCTION_ACME_ROOT" ]] ||
    fail "ACME root directory does not exist: $PRODUCTION_ACME_ROOT"
  require_owned_public_directory "PRODUCTION_PUBLIC_ROOT" "$PRODUCTION_PUBLIC_ROOT"
  for name in \
    "$PRODUCTION_PUBLIC_ROOT/downloads" \
    "$PRODUCTION_PUBLIC_ROOT/downloads/android"; do
    if [[ -e "$name" || -L "$name" ]]; then
      require_owned_public_directory "Production public download directory" "$name"
    fi
  done
}

validate_manifest() {
  local file=$1

  require_command jq
  require_regular_file "release manifest" "$file"

  jq --exit-status --slurp '
    length == 1 and
    (.[0] |
      type == "object" and
      keys == [
        "abis",
        "apk_certificate_sha256",
        "application_id",
        "database_schema_version",
        "git_sha",
        "manifest_version",
        "minimum_android_api",
        "portal_image",
        "portal_image_digest",
        "production_apk_file",
        "production_apk_sha256",
        "production_apk_size_bytes",
        "quality_run_url",
        "server_image",
        "server_image_digest",
        "staging_apk_file",
        "staging_apk_sha256",
        "version",
        "version_code"
      ] and
      .manifest_version == 1 and
      (.version | type == "string" and
        test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")) and
      (.version_code | type == "number" and
        . >= 1 and . <= 9007199254740991 and . == floor) and
      (.git_sha | type == "string" and
        test("^[0-9a-f]{40}$") and test("[1-9a-f]")) and
      .portal_image == "ghcr.io/1024xengineer/xe3-esl-portal" and
      (.portal_image_digest | type == "string" and
        test("^sha256:[0-9a-f]{64}$") and
        (ltrimstr("sha256:") | test("[1-9a-f]"))) and
      .server_image == "ghcr.io/1024xengineer/xe3-esl-server" and
      (.server_image_digest | type == "string" and
        test("^sha256:[0-9a-f]{64}$") and
        (ltrimstr("sha256:") | test("[1-9a-f]"))) and
      .staging_apk_file ==
        ("speakup-v" + .version + "-staging-arm64.apk") and
      (.staging_apk_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      .production_apk_file ==
        ("speakup-v" + .version + "-production-arm64.apk") and
      (.production_apk_size_bytes | type == "number" and
        . >= 1 and . <= 9007199254740991 and . == floor) and
      (.production_apk_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      .application_id == "com.xengineer.speakup" and
      .minimum_android_api == 24 and
      .abis == ["arm64-v8a"] and
      (.apk_certificate_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      (.database_schema_version | type == "number" and
        . >= 1 and . <= 9007199254740991 and . == floor) and
      (.quality_run_url | type == "string" and
        test("^https://github\\.com/1024XEngineer/XE3-ESL/actions/runs/[1-9][0-9]*$"))
    )
  ' "$file" >/dev/null || fail "release manifest is invalid"

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
    --project-directory "$production_directory" \
    --file "$compose_file" \
    "$@"
}

validate_compose() {
  require_command docker
  docker compose version >/dev/null
  compose --profile migration config --quiet
}

validate_external_volumes() {
  docker volume inspect "$portal_data_volume" >/dev/null 2>&1 ||
    fail "required existing Portal data volume is missing: $portal_data_volume"
  docker volume inspect "$postgres_data_volume" >/dev/null 2>&1 ||
    fail "required Production PostgreSQL volume is missing: $postgres_data_volume"
}

validate_server_edge_network() {
  local inspection gateway

  gateway=${PRODUCTION_SERVER_EDGE_GATEWAY_CIDR%/32}
  inspection=$(docker network inspect "$server_edge_network") ||
    fail "required Production Server edge network is missing: $server_edge_network"
  jq --exit-status --slurp \
    --arg name "$server_edge_network" \
    --arg gateway "$gateway" '
      length == 1 and
      (.[0] |
        type == "array" and length == 1 and
        .[0].Name == $name and
        ([.[0].IPAM.Config[]? | .Gateway? // empty] == [$gateway])
      )
    ' <<<"$inspection" >/dev/null 2>&1 ||
    fail "$server_edge_network gateway does not match PRODUCTION_SERVER_EDGE_GATEWAY_CIDR"
}

render_nginx() {
  local output=$1
  local temporary

  [[ -d "$(dirname "$output")" ]] || fail "Nginx output directory does not exist"
  temporary=$(mktemp "${output}.tmp.XXXXXX")
  if ! sed \
    -e "s|__PRODUCTION_PORTAL_HOST__|$PRODUCTION_PORTAL_HOST|g" \
    -e "s|__PRODUCTION_PORTAL_REDIRECT_HOST__|$PRODUCTION_PORTAL_REDIRECT_HOST|g" \
    -e "s|__PRODUCTION_API_HOST__|$PRODUCTION_API_HOST|g" \
    -e "s|__PRODUCTION_TLS_CERTIFICATE__|$PRODUCTION_TLS_CERTIFICATE|g" \
    -e "s|__PRODUCTION_TLS_CERTIFICATE_KEY__|$PRODUCTION_TLS_CERTIFICATE_KEY|g" \
    -e "s|__PRODUCTION_ACME_ROOT__|$PRODUCTION_ACME_ROOT|g" \
    -e "s|__PRODUCTION_PUBLIC_ROOT__|$PRODUCTION_PUBLIC_ROOT|g" \
    "$nginx_template" > "$temporary"; then
    rm -f "$temporary"
    fail "failed to render Nginx template"
  fi
  if grep -q '__PRODUCTION_[A-Z_]*__' "$temporary"; then
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

runtime_container_id() {
  local service=$1
  local containers

  containers=$(
    docker ps \
      --filter "label=com.docker.compose.project=$compose_project" \
      --filter "label=com.docker.compose.service=$service" \
      --format '{{.ID}}'
  )
  [[ -n "$containers" ]] ||
    fail "$compose_project/$service is not running"
  [[ "$containers" != *$'\n'* ]] ||
    fail "$compose_project/$service has multiple running containers"
  [[ "$containers" =~ ^[0-9a-f]{12,64}$ ]] ||
    fail "$compose_project/$service returned an invalid container ID"
  printf '%s\n' "$containers"
}

inspect_runtime_service() {
  local service=$1
  local expected_image=$2
  local container_id inspection

  container_id=$(runtime_container_id "$service")
  inspection=$(docker inspect "$container_id") ||
    fail "cannot inspect $compose_project/$service"
  jq --exit-status --slurp \
    --arg container_id "$container_id" \
    --arg project "$compose_project" \
    --arg service "$service" \
    --arg expected_image "$expected_image" '
      length == 1 and
      (.[0] |
        type == "array" and length == 1 and
        (.[0] |
          (.Id | startswith($container_id)) and
          .Config.Labels["com.docker.compose.project"] == $project and
          .Config.Labels["com.docker.compose.service"] == $service and
          .Config.Image == $expected_image and
          .State.Status == "running" and
          .State.Running == true and
          .State.Health.Status == "healthy"
        )
      )
    ' <<<"$inspection" >/dev/null ||
    fail "$compose_project/$service identity, image, or health is invalid"
  printf '%s\n' "$inspection"
}

verify_runtime() {
  local portal server postgres

  portal=$(inspect_runtime_service \
    portal "$portal_image_repository@$PORTAL_IMAGE_DIGEST")
  jq --exit-status --arg network "${compose_project}_portal_edge" '
    def exact_env($key; $value):
      [.Config.Env[]? | select(startswith($key + "="))] ==
        [($key + "=" + $value)];
    .[0] |
    exact_env("PORTAL_ADMIN_PASSWORD"; env.PORTAL_ADMIN_PASSWORD) and
    exact_env("PORTAL_SQLITE_PATH"; "/app/.wrangler/portal.sqlite") and
    exact_env("VINEXT_TRUSTED_HOSTS"; env.PRODUCTION_PORTAL_HOST) and
    (.Mounts | length == 1) and
    .Mounts[0].Type == "volume" and
    .Mounts[0].Name == "xe3-speakup-portal-data" and
    .Mounts[0].Destination == "/app/.wrangler" and
    .Mounts[0].RW == true and
    .NetworkSettings.Ports == {
      "3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "18082"}]
    } and
    (.NetworkSettings.Networks | keys) == [$network]
  ' <<<"$portal" >/dev/null 2>&1 ||
    fail "$compose_project/portal runtime configuration is invalid"

  server=$(inspect_runtime_service \
    server "$server_image_repository@$SERVER_IMAGE_DIGEST")
  jq --exit-status \
    --arg database "${compose_project}_database" \
    --arg edge "$server_edge_network" '
      def exact_env($key; $value):
        [.Config.Env[]? | select(startswith($key + "="))] ==
          [($key + "=" + $value)];
      .[0] |
      exact_env("DATABASE_URL";
        "postgres://" + env.PRODUCTION_POSTGRES_USER + ":" +
        env.PRODUCTION_POSTGRES_PASSWORD + "@postgres:5432/" +
        env.PRODUCTION_POSTGRES_DB + "?sslmode=disable") and
      exact_env("SERVER_HOST"; "0.0.0.0") and
      exact_env("SERVER_PORT"; "8080") and
      exact_env("TRUSTED_PROXY_CIDRS";
        env.PRODUCTION_SERVER_EDGE_GATEWAY_CIDR) and
      exact_env("TRUSTED_PROXY_HEADER"; "x-forwarded-for") and
      (.Mounts | length == 0) and
      .NetworkSettings.Ports == {
        "8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "18083"}]
      } and
      (.NetworkSettings.Networks | keys) == ([$database, $edge] | sort)
    ' <<<"$server" >/dev/null 2>&1 ||
    fail "$compose_project/server runtime configuration is invalid"

  postgres=$(inspect_runtime_service postgres "$postgres_image")
  jq --exit-status --arg network "${compose_project}_database" '
    def exact_env($key; $value):
      [.Config.Env[]? | select(startswith($key + "="))] ==
        [($key + "=" + $value)];
    .[0] |
    exact_env("POSTGRES_DB"; env.PRODUCTION_POSTGRES_DB) and
    exact_env("POSTGRES_USER"; env.PRODUCTION_POSTGRES_USER) and
    exact_env("POSTGRES_PASSWORD"; env.PRODUCTION_POSTGRES_PASSWORD) and
    (.Mounts | length == 1) and
    .Mounts[0].Type == "volume" and
    .Mounts[0].Name == "xe3-speakup-postgres-data" and
    .Mounts[0].Destination == "/var/lib/postgresql" and
    .Mounts[0].RW == true and
    ([.NetworkSettings.Ports // {} | to_entries[] | select(.value != null)] |
      length == 0) and
    (.NetworkSettings.Networks | keys) == [$network]
  ' <<<"$postgres" >/dev/null 2>&1 ||
    fail "$compose_project/postgres runtime configuration is invalid"
}

validate_all() {
  local manifest=$1
  local rendered

  validate_configuration
  validate_manifest "$manifest"
  validate_compose
  validate_external_volumes
  validate_server_edge_network
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
    validate | verify | status)
      [[ -n "$manifest" ]] || fail "$command requires --manifest"
      [[ -z "$output" ]] || fail "$command does not accept --output"
      validate_all "$manifest"
      case "$command" in
        validate)
          printf 'version=%s git_sha=%s schema=%s validated=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA" "$RELEASE_SCHEMA_VERSION"
          ;;
        verify)
          verify_runtime
          verify_endpoints
          printf 'version=%s verified=true\n' "$RELEASE_VERSION"
          ;;
        status)
          compose ps
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
