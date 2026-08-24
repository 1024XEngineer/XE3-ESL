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
readonly android_download_manager="$production_directory/../android-download/manage.sh"
readonly production_lock_file="${SPEAKUP_PRODUCTION_LOCK_FILE:-/run/lock/xe3-speakup-production/deploy.lock}"
readonly postgres_backup_lock_file="${SPEAKUP_POSTGRES_BACKUP_LOCK_FILE:-/run/lock/xe3-postgres-backup.lock}"
readonly portal_backup_lock_file="${SPEAKUP_PORTAL_BACKUP_LOCK_FILE:-/run/lock/xe3-portal-sqlite-backup.lock}"
readonly postgres_backup_root="/var/lib/speakup/postgres-backups"
readonly portal_backup_root="/var/lib/speakup/portal-backups"
readonly portal_health_url="http://127.0.0.1:18082/"
readonly server_health_url="http://127.0.0.1:18083/health"
readonly server_readiness_url="http://127.0.0.1:18083/readyz"

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh validate --manifest FILE --env-file FILE
  manage.sh baseline --manifest FILE --env-file FILE --receipt FILE
  manage.sh deploy --manifest FILE --env-file FILE --bundle DIRECTORY --current-receipt FILE --receipt FILE
  manage.sh rollback --manifest FILE --env-file FILE --current-receipt FILE --target-receipt FILE --receipt FILE
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

path_mode() {
  local path=$1
  local mode

  if mode=$(stat -Lc '%a' -- "$path" 2>/dev/null); then
    :
  elif mode=$(stat -L -f '%Lp' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$mode"
}

path_owner() {
  local path=$1
  local owner

  if owner=$(stat -Lc '%u' -- "$path" 2>/dev/null); then
    :
  elif owner=$(stat -L -f '%u' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$owner"
}

path_group() {
  local path=$1
  local group

  if group=$(stat -Lc '%g' -- "$path" 2>/dev/null); then
    :
  elif group=$(stat -L -f '%g' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$group"
}

path_identity() {
  local path=$1
  local identity

  if identity=$(stat -Lc '%i' -- "$path" 2>/dev/null); then
    :
  elif identity=$(stat -L -f '%i' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$identity"
}

path_parent() {
  local path=$1
  local parent=${path%/*}

  printf '%s\n' "${parent:-/}"
}

require_owned_directory() {
  local description=$1
  local directory=$2
  local mode owner

  [[ ! -L "$directory" && -d "$directory" ]] ||
    fail "$description must be a real directory: $directory"
  owner=$(path_owner "$directory") ||
    fail "cannot inspect owner for $description: $directory"
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current user: $directory"
  mode=$(path_mode "$directory") ||
    fail "cannot inspect permissions for $description: $directory"
  (( (8#$mode & 0022) == 0 )) ||
    fail "$description cannot be group or world writable: $directory"
}

require_owned_executable() {
  local description=$1
  local file=$2
  local mode owner

  [[ ! -L "$file" ]] || fail "$description must not be a symbolic link: $file"
  require_regular_file "$description" "$file"
  [[ -x "$file" ]] || fail "$description is not executable: $file"
  owner=$(path_owner "$file") || fail "cannot inspect owner for $description: $file"
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current user: $file"
  mode=$(path_mode "$file") || fail "cannot inspect permissions for $description: $file"
  (( (8#$mode & 0022) == 0 )) ||
    fail "$description cannot be group or world writable: $file"
}

require_owned_nonwritable_file() {
  local description=$1
  local file=$2
  local mode owner

  [[ ! -L "$file" ]] || fail "$description must not be a symbolic link: $file"
  require_regular_file "$description" "$file"
  owner=$(path_owner "$file") || fail "cannot inspect owner for $description: $file"
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current user: $file"
  mode=$(path_mode "$file") || fail "cannot inspect permissions for $description: $file"
  (( (8#$mode & 0022) == 0 )) ||
    fail "$description cannot be group or world writable: $file"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -- "$1" | awk '{print $1}'
  else
    fail "sha256sum or shasum is required"
  fi
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
      PRODUCTION_PUBLIC_ROOT | \
      PRODUCTION_NGINX_BINARY | \
      PRODUCTION_NGINX_CONFIG | \
      PRODUCTION_POSTGRES_BACKUP_PROGRAM | \
      PRODUCTION_POSTGRES_BACKUP_ENV_FILE | \
      PRODUCTION_PORTAL_BACKUP_PROGRAM | \
      PRODUCTION_PORTAL_BACKUP_ENV_FILE)
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
    PRODUCTION_PUBLIC_ROOT \
    PRODUCTION_NGINX_BINARY \
    PRODUCTION_NGINX_CONFIG \
    PRODUCTION_POSTGRES_BACKUP_PROGRAM \
    PRODUCTION_POSTGRES_BACKUP_ENV_FILE \
    PRODUCTION_PORTAL_BACKUP_PROGRAM \
    PRODUCTION_PORTAL_BACKUP_ENV_FILE

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
    PRODUCTION_NGINX_BINARY
    PRODUCTION_NGINX_CONFIG
    PRODUCTION_POSTGRES_BACKUP_PROGRAM
    PRODUCTION_POSTGRES_BACKUP_ENV_FILE
    PRODUCTION_PORTAL_BACKUP_PROGRAM
    PRODUCTION_PORTAL_BACKUP_ENV_FILE
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
    PRODUCTION_PUBLIC_ROOT \
    PRODUCTION_NGINX_BINARY \
    PRODUCTION_NGINX_CONFIG \
    PRODUCTION_POSTGRES_BACKUP_PROGRAM \
    PRODUCTION_POSTGRES_BACKUP_ENV_FILE \
    PRODUCTION_PORTAL_BACKUP_PROGRAM \
    PRODUCTION_PORTAL_BACKUP_ENV_FILE; do
    valid_absolute_path "${!name}" || fail "$name must be a safe absolute path"
  done

  require_private_file "server environment file" "$PRODUCTION_SERVER_ENV_FILE"
  require_regular_file "TLS certificate" "$PRODUCTION_TLS_CERTIFICATE"
  require_private_tls_key "TLS certificate key" "$PRODUCTION_TLS_CERTIFICATE_KEY"
  require_owned_executable "Nginx binary" "$PRODUCTION_NGINX_BINARY"
  require_owned_nonwritable_file "Nginx vhost" "$PRODUCTION_NGINX_CONFIG"
  [[ "${PRODUCTION_NGINX_CONFIG##*/}" == xe3-speakup-*.conf ]] ||
    fail "PRODUCTION_NGINX_CONFIG must name a SpeakUp-only vhost"
  require_owned_directory \
    "Nginx vhost directory" "$(path_parent "$PRODUCTION_NGINX_CONFIG")"
  require_owned_executable \
    "PostgreSQL backup program" "$PRODUCTION_POSTGRES_BACKUP_PROGRAM"
  require_private_file \
    "PostgreSQL backup environment" "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE"
  require_owned_executable \
    "Portal backup program" "$PRODUCTION_PORTAL_BACKUP_PROGRAM"
  require_private_file \
    "Portal backup environment" "$PRODUCTION_PORTAL_BACKUP_ENV_FILE"
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
  RELEASE_VERSION_CODE=$(jq --raw-output '.version_code' "$file")
  RELEASE_GIT_SHA=$(jq --raw-output '.git_sha' "$file")
  RELEASE_SCHEMA_VERSION=$(jq --raw-output '.database_schema_version' "$file")
  RELEASE_PRODUCTION_APK_FILE=$(jq --raw-output '.production_apk_file' "$file")
  RELEASE_PRODUCTION_APK_SIZE=$(jq --raw-output '.production_apk_size_bytes' "$file")
  RELEASE_PRODUCTION_APK_SHA256=$(jq --raw-output '.production_apk_sha256' "$file")
  RELEASE_APK_CERTIFICATE_SHA256=$(jq --raw-output '.apk_certificate_sha256' "$file")
  RELEASE_MANIFEST_SHA256=$(sha256_file "$file")
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

save_release_state() {
  local prefix=$1

  printf -v "${prefix}_MANIFEST_SHA256" '%s' "$RELEASE_MANIFEST_SHA256"
  printf -v "${prefix}_VERSION" '%s' "$RELEASE_VERSION"
  printf -v "${prefix}_VERSION_CODE" '%s' "$RELEASE_VERSION_CODE"
  printf -v "${prefix}_GIT_SHA" '%s' "$RELEASE_GIT_SHA"
  printf -v "${prefix}_SCHEMA_VERSION" '%s' "$RELEASE_SCHEMA_VERSION"
  printf -v "${prefix}_PORTAL_IMAGE_DIGEST" '%s' "$PORTAL_IMAGE_DIGEST"
  printf -v "${prefix}_SERVER_IMAGE_DIGEST" '%s' "$SERVER_IMAGE_DIGEST"
  printf -v "${prefix}_PRODUCTION_APK_FILE" '%s' "$RELEASE_PRODUCTION_APK_FILE"
  printf -v "${prefix}_PRODUCTION_APK_SIZE" '%s' "$RELEASE_PRODUCTION_APK_SIZE"
  printf -v "${prefix}_PRODUCTION_APK_SHA256" '%s' "$RELEASE_PRODUCTION_APK_SHA256"
  printf -v "${prefix}_APK_CERTIFICATE_SHA256" '%s' "$RELEASE_APK_CERTIFICATE_SHA256"
}

use_release_state() {
  local prefix=$1
  local manifest_name="${prefix}_MANIFEST_SHA256"
  local version_name="${prefix}_VERSION"
  local version_code_name="${prefix}_VERSION_CODE"
  local git_name="${prefix}_GIT_SHA"
  local schema_name="${prefix}_SCHEMA_VERSION"
  local portal_name="${prefix}_PORTAL_IMAGE_DIGEST"
  local server_name="${prefix}_SERVER_IMAGE_DIGEST"
  local apk_file_name="${prefix}_PRODUCTION_APK_FILE"
  local apk_size_name="${prefix}_PRODUCTION_APK_SIZE"
  local apk_sha_name="${prefix}_PRODUCTION_APK_SHA256"
  local apk_certificate_name="${prefix}_APK_CERTIFICATE_SHA256"

  RELEASE_MANIFEST_SHA256=${!manifest_name}
  RELEASE_VERSION=${!version_name}
  RELEASE_VERSION_CODE=${!version_code_name}
  RELEASE_GIT_SHA=${!git_name}
  RELEASE_SCHEMA_VERSION=${!schema_name}
  PORTAL_IMAGE_DIGEST=${!portal_name}
  SERVER_IMAGE_DIGEST=${!server_name}
  RELEASE_PRODUCTION_APK_FILE=${!apk_file_name}
  RELEASE_PRODUCTION_APK_SIZE=${!apk_size_name}
  RELEASE_PRODUCTION_APK_SHA256=${!apk_sha_name}
  RELEASE_APK_CERTIFICATE_SHA256=${!apk_certificate_name}
  export PORTAL_IMAGE_DIGEST SERVER_IMAGE_DIGEST
}

require_receipt_file() {
  local file=$1
  local mode

  valid_absolute_path "$file" || fail "receipt must use a safe absolute path"
  require_owned_nonwritable_file "Production receipt" "$file"
  mode=$(path_mode "$file") || fail "cannot inspect Production receipt permissions"
  [[ "$mode" == 400 || "$mode" == 444 ]] ||
    fail "Production receipt must have mode 0400 or 0444: $file"
}

validate_receipt() {
  local file=$1
  local config temporary

  require_receipt_file "$file"
  jq --exit-status --slurp '
    length == 1 and
    (.[0] |
      type == "object" and
      keys == [
        "android_bundle_manifest_sha256",
        "apk_certificate_sha256",
        "database_schema_version",
        "environment",
        "git_sha",
        "manifest_sha256",
        "nginx_config",
        "nginx_config_sha256",
        "operation",
        "portal_backup_id",
        "portal_container_id",
        "portal_image_digest",
        "postgres_backup_id",
        "postgres_container_id",
        "previous_receipt_sha256",
        "production_apk_file",
        "production_apk_sha256",
        "production_apk_size_bytes",
        "receipt_version",
        "recorded_at_utc",
        "rollback_target_receipt_sha256",
        "server_container_id",
        "server_image_digest",
        "version",
        "version_code"
      ] and
      .receipt_version == 1 and
      .environment == "production" and
      (.operation == "baseline" or .operation == "deploy" or
        .operation == "rollback") and
      (.manifest_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      (.version | type == "string" and
        test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")) and
      (.version_code | type == "number" and . >= 1 and
        . <= 9007199254740991 and . == floor) and
      (.git_sha | type == "string" and test("^[0-9a-f]{40}$") and
        test("[1-9a-f]")) and
      (.database_schema_version | type == "number" and . >= 1 and
        . <= 9007199254740991 and . == floor) and
      (.portal_image_digest | type == "string" and
        test("^sha256:[0-9a-f]{64}$") and
        (ltrimstr("sha256:") | test("[1-9a-f]"))) and
      (.server_image_digest | type == "string" and
        test("^sha256:[0-9a-f]{64}$") and
        (ltrimstr("sha256:") | test("[1-9a-f]"))) and
      .production_apk_file ==
        ("speakup-v" + .version + "-production-arm64.apk") and
      (.production_apk_size_bytes | type == "number" and . >= 1 and
        . <= 9007199254740991 and . == floor) and
      (.production_apk_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      (.apk_certificate_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      (.portal_container_id | type == "string" and test("^[0-9a-f]{12,64}$")) and
      (.server_container_id | type == "string" and test("^[0-9a-f]{12,64}$")) and
      (.postgres_container_id | type == "string" and test("^[0-9a-f]{12,64}$")) and
      (.nginx_config | type == "string" and length > 0) and
      (.nginx_config_sha256 | type == "string" and test("^[0-9a-f]{64}$")) and
      (.recorded_at_utc | type == "string" and
        test("^[0-9]{4}-(0[1-9]|1[0-2])-([012][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$") and
        (. as $timestamp |
          try ((fromdateiso8601 | todateiso8601) == $timestamp) catch false)) and
      (if .operation == "deploy" then
        (.postgres_backup_id | type == "string" and
          test("^[0-9]{8}T[0-9]{6}Z-predeploy$")) and
        (.portal_backup_id | type == "string" and
          test("^[0-9]{8}T[0-9]{9}Z$")) and
        (.android_bundle_manifest_sha256 | type == "string" and
          test("^[0-9a-f]{64}$")) and
        (.previous_receipt_sha256 | type == "string" and
          test("^[0-9a-f]{64}$")) and
        .rollback_target_receipt_sha256 == null
      elif .operation == "rollback" then
        .postgres_backup_id == null and .portal_backup_id == null and
        .android_bundle_manifest_sha256 == null and
        (.previous_receipt_sha256 | type == "string" and
          test("^[0-9a-f]{64}$")) and
        (.rollback_target_receipt_sha256 | type == "string" and
          test("^[0-9a-f]{64}$"))
      else
        .postgres_backup_id == null and .portal_backup_id == null and
        .android_bundle_manifest_sha256 == null and
        .previous_receipt_sha256 == null and
        .rollback_target_receipt_sha256 == null
      end)
    )
  ' "$file" >/dev/null || fail "Production receipt is invalid: $file"

  temporary=$(mktemp)
  jq --join-output --raw-output '.nginx_config' "$file" >"$temporary" || {
    rm -f "$temporary"
    fail "cannot extract Nginx configuration from Production receipt"
  }
  config=$(sha256_file "$temporary")
  rm -f "$temporary"
  [[ "$config" == "$(jq --raw-output '.nginx_config_sha256' "$file")" ]] ||
    fail "Production receipt Nginx SHA-256 is invalid: $file"
}

load_receipt() {
  local file=$1
  local prefix=$2

  validate_receipt "$file"
  printf -v "${prefix}_RECEIPT_SHA256" '%s' "$(sha256_file "$file")"
  printf -v "${prefix}_MANIFEST_SHA256" '%s' "$(jq --raw-output '.manifest_sha256' "$file")"
  printf -v "${prefix}_VERSION" '%s' "$(jq --raw-output '.version' "$file")"
  printf -v "${prefix}_VERSION_CODE" '%s' "$(jq --raw-output '.version_code' "$file")"
  printf -v "${prefix}_GIT_SHA" '%s' "$(jq --raw-output '.git_sha' "$file")"
  printf -v "${prefix}_SCHEMA_VERSION" '%s' "$(jq --raw-output '.database_schema_version' "$file")"
  printf -v "${prefix}_PORTAL_IMAGE_DIGEST" '%s' "$(jq --raw-output '.portal_image_digest' "$file")"
  printf -v "${prefix}_SERVER_IMAGE_DIGEST" '%s' "$(jq --raw-output '.server_image_digest' "$file")"
  printf -v "${prefix}_PRODUCTION_APK_FILE" '%s' "$(jq --raw-output '.production_apk_file' "$file")"
  printf -v "${prefix}_PRODUCTION_APK_SIZE" '%s' "$(jq --raw-output '.production_apk_size_bytes' "$file")"
  printf -v "${prefix}_PRODUCTION_APK_SHA256" '%s' "$(jq --raw-output '.production_apk_sha256' "$file")"
  printf -v "${prefix}_APK_CERTIFICATE_SHA256" '%s' "$(jq --raw-output '.apk_certificate_sha256' "$file")"
}

validate_receipt_matches_release() {
  local prefix=$1
  local manifest_name="${prefix}_MANIFEST_SHA256"
  local version_name="${prefix}_VERSION"
  local version_code_name="${prefix}_VERSION_CODE"
  local git_name="${prefix}_GIT_SHA"
  local schema_name="${prefix}_SCHEMA_VERSION"
  local portal_name="${prefix}_PORTAL_IMAGE_DIGEST"
  local server_name="${prefix}_SERVER_IMAGE_DIGEST"
  local apk_file_name="${prefix}_PRODUCTION_APK_FILE"
  local apk_size_name="${prefix}_PRODUCTION_APK_SIZE"
  local apk_sha_name="${prefix}_PRODUCTION_APK_SHA256"
  local apk_certificate_name="${prefix}_APK_CERTIFICATE_SHA256"

  [[ "${!manifest_name}" == "$RELEASE_MANIFEST_SHA256" &&
    "${!version_name}" == "$RELEASE_VERSION" &&
    "${!version_code_name}" == "$RELEASE_VERSION_CODE" &&
    "${!git_name}" == "$RELEASE_GIT_SHA" &&
    "${!schema_name}" == "$RELEASE_SCHEMA_VERSION" &&
    "${!portal_name}" == "$PORTAL_IMAGE_DIGEST" &&
    "${!server_name}" == "$SERVER_IMAGE_DIGEST" &&
    "${!apk_file_name}" == "$RELEASE_PRODUCTION_APK_FILE" &&
    "${!apk_size_name}" == "$RELEASE_PRODUCTION_APK_SIZE" &&
    "${!apk_sha_name}" == "$RELEASE_PRODUCTION_APK_SHA256" &&
    "${!apk_certificate_name}" == "$RELEASE_APK_CERTIFICATE_SHA256" ]] ||
    fail "Production receipt does not match the selected release manifest"
}

validate_receipt_target() {
  local receipt=$1
  local directory

  valid_absolute_path "$receipt" || fail "--receipt must be a safe absolute path"
  [[ ! -e "$receipt" && ! -L "$receipt" ]] ||
    fail "Production receipt already exists: $receipt"
  directory=$(path_parent "$receipt")
  require_owned_directory "Production receipt directory" "$directory"
  [[ -w "$directory" ]] ||
    fail "Production receipt directory is not writable: $directory"
}

require_safe_lock_file() {
  local description=$1
  local file=$2
  local mode

  [[ ! -L "$file" && -f "$file" && -r "$file" && -w "$file" ]] ||
    fail "$description lock must be a readable and writable regular file"
  [[ "$(path_owner "$file")" == "$(id -u)" ]] ||
    fail "$description lock must be owned by the current user"
  mode=$(path_mode "$file") || fail "cannot inspect $description lock permissions"
  [[ "$mode" == 600 ]] || fail "$description lock must have mode 0600"
}

require_system_lock_directory() {
  local directory=$1
  local mode owner group

  [[ "$directory" == /run/lock && ! -L "$directory" && -d "$directory" ]] ||
    fail "system lock directory is invalid: $directory"
  owner=$(path_owner "$directory") || fail "cannot inspect system lock owner"
  group=$(path_group "$directory") || fail "cannot inspect system lock group"
  [[ "$(id -u)" == 0 && "$owner" == 0 && "$group" == 0 ]] ||
    fail "system lock directory requires the root operator and root ownership"
  mode=$(path_mode "$directory") || fail "cannot inspect system lock permissions"
  (( (8#$mode & 0002) == 0 || (8#$mode & 01000) != 0 )) ||
    fail "world-writable system lock directory must have the sticky bit"
}

prepare_lock_file() {
  local description=$1
  local file=$2
  local directory

  valid_absolute_path "$file" || fail "$description lock must use a safe absolute path"
  directory=$(path_parent "$file")
  if [[ "$directory" == /run/lock/xe3-speakup-production &&
    ! -e "$directory" && ! -L "$directory" ]]; then
    require_system_lock_directory /run/lock
    (umask 077 && mkdir -- "$directory") 2>/dev/null ||
      [[ ! -L "$directory" && -d "$directory" ]] ||
      fail "cannot create Production deployment lock directory"
  fi
  if [[ "$directory" == /run/lock ]]; then
    require_system_lock_directory "$directory"
  else
    require_owned_directory "$description lock directory" "$directory"
  fi
  if [[ ! -e "$file" && ! -L "$file" ]]; then
    (umask 077 && set -o noclobber && : >"$file") 2>/dev/null ||
      [[ -e "$file" || -L "$file" ]] || fail "cannot create $description lock"
  fi
  require_safe_lock_file "$description" "$file"
}

require_lock_identity() {
  local description=$1
  local file=$2
  local fd=$3

  [[ "$(path_identity "/dev/fd/$fd")" == "$(path_identity "$file")" ]] ||
    fail "$description lock changed while it was being acquired"
}

acquire_production_lock() {
  require_command flock
  prepare_lock_file "Production deployment" "$production_lock_file"
  exec 9>>"$production_lock_file" || fail "cannot open Production deployment lock"
  require_lock_identity "Production deployment" "$production_lock_file" 9
  flock --nonblock 9 || fail "another Production deployment operation is running"
  require_lock_identity "Production deployment" "$production_lock_file" 9
}

acquire_backup_locks() {
  prepare_lock_file "PostgreSQL backup" "$postgres_backup_lock_file"
  exec 8>>"$postgres_backup_lock_file" || fail "cannot open PostgreSQL backup lock"
  require_lock_identity "PostgreSQL backup" "$postgres_backup_lock_file" 8
  flock --nonblock 8 || fail "another PostgreSQL backup operation is running"
  require_lock_identity "PostgreSQL backup" "$postgres_backup_lock_file" 8

  prepare_lock_file "Portal backup" "$portal_backup_lock_file"
  exec 7>>"$portal_backup_lock_file" || fail "cannot open Portal backup lock"
  require_lock_identity "Portal backup" "$portal_backup_lock_file" 7
  flock --nonblock 7 || fail "another Portal backup operation is running"
  require_lock_identity "Portal backup" "$portal_backup_lock_file" 7
}

load_backup_configuration() {
  local family=$1
  local file=$2
  local line name value

  require_private_file "$family backup environment" "$file"
  unset \
    POSTGRES_BACKUP_IMAGE \
    POSTGRES_BACKUP_DATABASE \
    POSTGRES_BACKUP_USER \
    POSTGRES_BACKUP_SOURCE_VOLUME \
    POSTGRES_BACKUP_DEPLOYMENT_VERSION \
    POSTGRES_BACKUP_GIT_SHA \
    POSTGRES_BACKUP_RETENTION_DAYS \
    POSTGRES_BACKUP_MAX_AGE_SECONDS \
    PORTAL_BACKUP_CONTAINER \
    PORTAL_BACKUP_SOURCE_VOLUME \
    PORTAL_BACKUP_IMAGE \
    PORTAL_BACKUP_DEPLOYMENT_VERSION \
    PORTAL_BACKUP_RETENTION_DAYS \
    PORTAL_BACKUP_MAX_AGE_SECONDS

  while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line%$'\r'}
    case "$line" in
      "" | \#*) continue ;;
    esac
    [[ "$line" == *=* ]] || fail "invalid $family backup environment line"
    name=${line%%=*}
    value=${line#*=}
    case "$family:$name" in
      postgres:POSTGRES_BACKUP_IMAGE | \
        postgres:POSTGRES_BACKUP_DATABASE | \
        postgres:POSTGRES_BACKUP_USER | \
        postgres:POSTGRES_BACKUP_SOURCE_VOLUME | \
        postgres:POSTGRES_BACKUP_DEPLOYMENT_VERSION | \
        postgres:POSTGRES_BACKUP_GIT_SHA | \
        postgres:POSTGRES_BACKUP_RETENTION_DAYS | \
        postgres:POSTGRES_BACKUP_MAX_AGE_SECONDS | \
        portal:PORTAL_BACKUP_CONTAINER | \
        portal:PORTAL_BACKUP_SOURCE_VOLUME | \
        portal:PORTAL_BACKUP_IMAGE | \
        portal:PORTAL_BACKUP_DEPLOYMENT_VERSION | \
        portal:PORTAL_BACKUP_RETENTION_DAYS | \
        portal:PORTAL_BACKUP_MAX_AGE_SECONDS)
        ;;
      *) fail "unsupported $family backup environment key: $name" ;;
    esac
    printf -v "$name" '%s' "$value"
    export "$name"
  done <"$file"
}

require_positive_integer() {
  local name=$1

  require_value "$name"
  [[ "${!name}" =~ ^[1-9][0-9]*$ ]] || fail "$name must be a positive integer"
}

validate_postgres_backup_configuration() {
  local version=$1
  local git_sha=$2
  local name

  for name in \
    POSTGRES_BACKUP_IMAGE \
    POSTGRES_BACKUP_DATABASE \
    POSTGRES_BACKUP_USER \
    POSTGRES_BACKUP_SOURCE_VOLUME \
    POSTGRES_BACKUP_DEPLOYMENT_VERSION \
    POSTGRES_BACKUP_GIT_SHA; do
    require_value "$name"
  done
  require_positive_integer POSTGRES_BACKUP_RETENTION_DAYS
  require_positive_integer POSTGRES_BACKUP_MAX_AGE_SECONDS
  [[ "$POSTGRES_BACKUP_IMAGE" == "$postgres_image" &&
    "$POSTGRES_BACKUP_DATABASE" == "$PRODUCTION_POSTGRES_DB" &&
    "$POSTGRES_BACKUP_USER" == "$PRODUCTION_POSTGRES_USER" &&
    "$POSTGRES_BACKUP_SOURCE_VOLUME" == "$postgres_data_volume" &&
    "$POSTGRES_BACKUP_DEPLOYMENT_VERSION" == "$version" &&
    "$POSTGRES_BACKUP_GIT_SHA" == "$git_sha" ]] ||
    fail "PostgreSQL backup configuration does not match the current Production release"
}

validate_portal_backup_configuration() {
  local version=$1
  local portal_digest=$2
  local name

  for name in \
    PORTAL_BACKUP_CONTAINER \
    PORTAL_BACKUP_SOURCE_VOLUME \
    PORTAL_BACKUP_IMAGE \
    PORTAL_BACKUP_DEPLOYMENT_VERSION; do
    require_value "$name"
  done
  require_positive_integer PORTAL_BACKUP_RETENTION_DAYS
  require_positive_integer PORTAL_BACKUP_MAX_AGE_SECONDS
  [[ "$PORTAL_BACKUP_CONTAINER" == "$RUNTIME_PORTAL_CONTAINER_NAME" &&
    "$PORTAL_BACKUP_SOURCE_VOLUME" == "$portal_data_volume" &&
    "$PORTAL_BACKUP_IMAGE" == "$portal_image_repository@$portal_digest" &&
    "$PORTAL_BACKUP_DEPLOYMENT_VERSION" == "$version" ]] ||
    fail "Portal backup configuration does not match the current Production release"
}

validate_backup_configurations() {
  load_backup_configuration postgres "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE"
  validate_postgres_backup_configuration "$RELEASE_VERSION" "$RELEASE_GIT_SHA"
  POSTGRES_RETENTION_DAYS=$POSTGRES_BACKUP_RETENTION_DAYS
  POSTGRES_MAX_AGE_SECONDS=$POSTGRES_BACKUP_MAX_AGE_SECONDS

  load_backup_configuration portal "$PRODUCTION_PORTAL_BACKUP_ENV_FILE"
  validate_portal_backup_configuration "$RELEASE_VERSION" "$PORTAL_IMAGE_DIGEST"
  PORTAL_RETENTION_DAYS=$PORTAL_BACKUP_RETENTION_DAYS
  PORTAL_MAX_AGE_SECONDS=$PORTAL_BACKUP_MAX_AGE_SECONDS
}

run_predeploy_backups() {
  local output check_output

  acquire_backup_locks

  load_backup_configuration postgres "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE"
  validate_postgres_backup_configuration "$RELEASE_VERSION" "$RELEASE_GIT_SHA"
  POSTGRES_RETENTION_DAYS=$POSTGRES_BACKUP_RETENTION_DAYS
  POSTGRES_MAX_AGE_SECONDS=$POSTGRES_BACKUP_MAX_AGE_SECONDS
  export POSTGRES_BACKUP_ROOT="$postgres_backup_root"
  output=$(
    unset PRODUCTION_POSTGRES_PASSWORD PORTAL_ADMIN_PASSWORD
    "$PRODUCTION_POSTGRES_BACKUP_PROGRAM" backup predeploy
  ) ||
    fail "PostgreSQL pre-deploy backup failed"
  [[ "$output" =~ ^backup_id=([0-9]{8}T[0-9]{6}Z-predeploy)\ restore=verified$ ]] ||
    fail "PostgreSQL pre-deploy backup returned invalid evidence"
  POSTGRES_BACKUP_ID=${BASH_REMATCH[1]}
  check_output=$(
    unset PRODUCTION_POSTGRES_PASSWORD PORTAL_ADMIN_PASSWORD
    "$PRODUCTION_POSTGRES_BACKUP_PROGRAM" check "$POSTGRES_BACKUP_ID"
  ) ||
    fail "PostgreSQL restore verification failed"
  [[ "$check_output" == "backup_id=$POSTGRES_BACKUP_ID restore=verified" ]] ||
    fail "PostgreSQL restore verification returned invalid evidence"

  load_backup_configuration portal "$PRODUCTION_PORTAL_BACKUP_ENV_FILE"
  validate_portal_backup_configuration "$RELEASE_VERSION" "$PORTAL_IMAGE_DIGEST"
  PORTAL_RETENTION_DAYS=$PORTAL_BACKUP_RETENTION_DAYS
  PORTAL_MAX_AGE_SECONDS=$PORTAL_BACKUP_MAX_AGE_SECONDS
  export PORTAL_BACKUP_ROOT="$portal_backup_root"
  output=$(
    unset PRODUCTION_POSTGRES_PASSWORD PORTAL_ADMIN_PASSWORD
    "$PRODUCTION_PORTAL_BACKUP_PROGRAM" backup
  ) ||
    fail "Portal pre-deploy backup failed"
  PORTAL_BACKUP_ID=$(sed -nE \
    's/^Portal SQLite backup completed: ([0-9]{8}T[0-9]{9}Z) \([1-9][0-9]* bytes, SHA-256 [0-9a-f]{64}\)$/\1/p' \
    <<<"$output")
  [[ -n "$PORTAL_BACKUP_ID" ]] ||
    fail "Portal pre-deploy backup returned invalid evidence"
  check_output=$(
    unset PRODUCTION_POSTGRES_PASSWORD PORTAL_ADMIN_PASSWORD
    "$PRODUCTION_PORTAL_BACKUP_PROGRAM" check
  ) ||
    fail "Portal restore verification failed"
  [[ "$check_output" =~ ^Portal\ SQLite\ restore\ check\ passed:\ ([0-9]{8}T[0-9]{9}Z)\ \([1-9][0-9]*\ bytes,\ SHA-256\ [0-9a-f]{64}\)$ &&
    "${BASH_REMATCH[1]}" == "$PORTAL_BACKUP_ID" ]] ||
    fail "Portal restore verification returned invalid evidence"
}

write_backup_configuration() {
  local family=$1
  local destination=$2
  local temporary=$3

  case "$family" in
    postgres)
      printf '%s\n' \
        '# Managed by deploy/production/manage.sh. Raw KEY=value only.' \
        "POSTGRES_BACKUP_IMAGE=$postgres_image" \
        "POSTGRES_BACKUP_DATABASE=$PRODUCTION_POSTGRES_DB" \
        "POSTGRES_BACKUP_USER=$PRODUCTION_POSTGRES_USER" \
        "POSTGRES_BACKUP_SOURCE_VOLUME=$postgres_data_volume" \
        "POSTGRES_BACKUP_DEPLOYMENT_VERSION=$RELEASE_VERSION" \
        "POSTGRES_BACKUP_GIT_SHA=$RELEASE_GIT_SHA" \
        "POSTGRES_BACKUP_RETENTION_DAYS=$POSTGRES_RETENTION_DAYS" \
        "POSTGRES_BACKUP_MAX_AGE_SECONDS=$POSTGRES_MAX_AGE_SECONDS" >"$temporary"
      ;;
    portal)
      printf '%s\n' \
        '# Managed by deploy/production/manage.sh. Contains no application Secret.' \
        "PORTAL_BACKUP_CONTAINER=$RUNTIME_PORTAL_CONTAINER_NAME" \
        "PORTAL_BACKUP_SOURCE_VOLUME=$portal_data_volume" \
        "PORTAL_BACKUP_IMAGE=$portal_image_repository@$PORTAL_IMAGE_DIGEST" \
        "PORTAL_BACKUP_DEPLOYMENT_VERSION=$RELEASE_VERSION" \
        "PORTAL_BACKUP_RETENTION_DAYS=$PORTAL_RETENTION_DAYS" \
        "PORTAL_BACKUP_MAX_AGE_SECONDS=$PORTAL_MAX_AGE_SECONDS" >"$temporary"
      ;;
    *) fail "unsupported backup configuration family: $family" ;;
  esac
  chmod 0600 "$temporary"
  require_private_file "$family backup configuration candidate" "$temporary"
  [[ "$(path_parent "$destination")" == "$(path_parent "$temporary")" ]] ||
    fail "$family backup candidate is outside its destination directory"
}

update_backup_configurations() {
  local postgres_directory portal_directory
  local postgres_candidate portal_candidate postgres_previous portal_previous

  postgres_directory=$(path_parent "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE")
  portal_directory=$(path_parent "$PRODUCTION_PORTAL_BACKUP_ENV_FILE")
  postgres_candidate=$(mktemp "$postgres_directory/.postgres-backup.next.XXXXXX")
  portal_candidate=$(mktemp "$portal_directory/.portal-backup.next.XXXXXX")
  postgres_previous=$(mktemp "$postgres_directory/.postgres-backup.previous.XXXXXX")
  portal_previous=$(mktemp "$portal_directory/.portal-backup.previous.XXXXXX")
  install -m 0600 "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE" "$postgres_previous"
  install -m 0600 "$PRODUCTION_PORTAL_BACKUP_ENV_FILE" "$portal_previous"
  write_backup_configuration postgres \
    "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE" "$postgres_candidate"
  write_backup_configuration portal \
    "$PRODUCTION_PORTAL_BACKUP_ENV_FILE" "$portal_candidate"

  if ! mv -f "$postgres_candidate" "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE"; then
    rm -f "$portal_candidate" "$postgres_previous" "$portal_previous"
    fail "cannot install PostgreSQL backup configuration"
  fi
  if ! mv -f "$portal_candidate" "$PRODUCTION_PORTAL_BACKUP_ENV_FILE"; then
    mv -f "$postgres_previous" "$PRODUCTION_POSTGRES_BACKUP_ENV_FILE" ||
      fail "cannot restore PostgreSQL backup configuration after Portal update failure"
    rm -f "$portal_candidate" "$portal_previous"
    fail "cannot install Portal backup configuration"
  fi
  rm -f "$postgres_previous" "$portal_previous"
  validate_backup_configurations
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

database_schema_version() {
  local output

  output=$(compose run --pull never --rm --no-deps migrate \
    /usr/local/bin/speakup-migrate version) ||
    fail "cannot verify the Production database schema"
  [[ "$output" =~ ^version=([1-9][0-9]*)\ dirty=false$ ]] ||
    fail "database schema output must be exactly version=N dirty=false"
  printf '%s\n' "${BASH_REMATCH[1]}"
}

verify_database_schema() {
  [[ "$(database_schema_version)" == "$RELEASE_SCHEMA_VERSION" ]] ||
    fail "database schema version does not match the selected release"
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

  RUNTIME_PORTAL_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<<"$portal")
  RUNTIME_SERVER_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<<"$server")
  RUNTIME_POSTGRES_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<<"$postgres")
  RUNTIME_PORTAL_CONTAINER_NAME=$(jq --raw-output '.[0].Name | ltrimstr("/")' <<<"$portal")
  [[ "$RUNTIME_PORTAL_CONTAINER_NAME" =~ ^xe3-speakup-production-portal-[1-9][0-9]*$ ]] ||
    fail "$compose_project/portal returned an invalid container name"
}

verify_deployment() {
  verify_runtime
  verify_database_schema
  verify_endpoints
}

receipt_nginx_configuration() {
  local receipt=$1
  local output=$2

  jq --join-output --raw-output '.nginx_config' "$receipt" >"$output" ||
    fail "cannot extract Nginx configuration from Production receipt"
  [[ "$(sha256_file "$output")" == \
    "$(jq --raw-output '.nginx_config_sha256' "$receipt")" ]] ||
    fail "Production receipt Nginx configuration changed"
}

verify_live_nginx_against_receipt() {
  local receipt=$1
  local temporary

  temporary=$(mktemp)
  receipt_nginx_configuration "$receipt" "$temporary"
  if ! cmp -s "$temporary" "$PRODUCTION_NGINX_CONFIG"; then
    rm -f "$temporary"
    fail "live Nginx vhost does not match the current Production receipt"
  fi
  rm -f "$temporary"
  "$PRODUCTION_NGINX_BINARY" -t || fail "nginx -t failed for the live Production vhost"
}

verify_live_nginx_against_template() {
  local temporary

  temporary=$(mktemp)
  render_nginx "$temporary"
  if ! cmp -s "$temporary" "$PRODUCTION_NGINX_CONFIG"; then
    rm -f "$temporary"
    fail "live Nginx vhost does not match the reviewed Production template"
  fi
  rm -f "$temporary"
  "$PRODUCTION_NGINX_BINARY" -t || fail "nginx -t failed for the live Production vhost"
}

restore_nginx_configuration() {
  local previous=$1
  local reload=$2
  local temporary

  temporary=$(mktemp "$(path_parent "$PRODUCTION_NGINX_CONFIG")/.nginx-restore.XXXXXX") ||
    return 1
  if ! install -m 0644 "$previous" "$temporary" ||
    ! mv -f "$temporary" "$PRODUCTION_NGINX_CONFIG" ||
    ! "$PRODUCTION_NGINX_BINARY" -t; then
    rm -f "$temporary"
    return 1
  fi
  if [[ "$reload" == true ]]; then
    "$PRODUCTION_NGINX_BINARY" -s reload || return 1
  fi
}

install_nginx_configuration() {
  local candidate=$1
  local current_receipt=$2
  local previous temporary

  require_regular_file "rendered Production Nginx candidate" "$candidate"
  verify_live_nginx_against_receipt "$current_receipt"
  previous=$(mktemp)
  receipt_nginx_configuration "$current_receipt" "$previous"
  temporary=$(mktemp "$(path_parent "$PRODUCTION_NGINX_CONFIG")/.nginx-next.XXXXXX")
  install -m 0644 "$candidate" "$temporary" || {
    rm -f "$previous" "$temporary"
    fail "cannot stage the Production Nginx vhost"
  }
  mv -f "$temporary" "$PRODUCTION_NGINX_CONFIG" || {
    rm -f "$previous" "$temporary"
    fail "cannot install the Production Nginx vhost"
  }
  if ! "$PRODUCTION_NGINX_BINARY" -t; then
    restore_nginx_configuration "$previous" false ||
      fail "nginx -t failed and the previous vhost could not be restored"
    rm -f "$previous"
    fail "nginx -t failed; the previous vhost was restored without reload"
  fi
  if ! "$PRODUCTION_NGINX_BINARY" -s reload; then
    restore_nginx_configuration "$previous" true ||
      fail "Nginx reload failed and the previous vhost could not be reloaded"
    rm -f "$previous"
    fail "Nginx reload failed; the previous vhost was restored and reloaded"
  fi
  rm -f "$previous"
  cmp -s "$candidate" "$PRODUCTION_NGINX_CONFIG" ||
    fail "installed Production Nginx vhost changed after reload"
}

validate_android_bundle() {
  local bundle=$1
  local bundle_manifest="$bundle/bundle-manifest.json"
  local metadata="$bundle/downloads/android/v$RELEASE_VERSION/release.json"

  valid_absolute_path "$bundle" || fail "--bundle must be a safe absolute path"
  [[ ! -L "$bundle" && -d "$bundle" ]] ||
    fail "Android download bundle must be a real directory"
  "$android_download_manager" validate \
    --bundle "$bundle" \
    --root "$PRODUCTION_PUBLIC_ROOT" >/dev/null ||
    fail "Android download bundle validation failed"
  [[ "$(jq --raw-output '.version' "$bundle_manifest")" == "$RELEASE_VERSION" &&
    "$(jq --raw-output '.release_manifest_sha256' "$bundle_manifest")" == \
      "$RELEASE_MANIFEST_SHA256" ]] ||
    fail "Android download bundle does not match the selected release manifest"
  jq --exit-status \
    --arg version "$RELEASE_VERSION" \
    --argjson version_code "$RELEASE_VERSION_CODE" \
    --arg file "$RELEASE_PRODUCTION_APK_FILE" \
    --argjson size "$RELEASE_PRODUCTION_APK_SIZE" \
    --arg sha256 "$RELEASE_PRODUCTION_APK_SHA256" \
    --arg certificate "$RELEASE_APK_CERTIFICATE_SHA256" '
      .version == $version and
      .version_code == $version_code and
      .file_name == $file and
      .size_bytes == $size and
      .apk_sha256 == $sha256 and
      .apk_certificate_sha256 == $certificate
    ' "$metadata" >/dev/null ||
    fail "Android public metadata does not match the selected release manifest"
  ANDROID_BUNDLE_MANIFEST_SHA256=$(sha256_file "$bundle_manifest")
}

publish_android_bundle() {
  local bundle=$1

  "$android_download_manager" publish \
    --bundle "$bundle" \
    --root "$PRODUCTION_PUBLIC_ROOT" >/dev/null ||
    fail "Android download bundle publication failed"
}

write_production_receipt() {
  local manifest=$1
  local receipt=$2
  local operation=$3
  local previous_receipt_sha256=$4
  local postgres_backup_id=$5
  local portal_backup_id=$6
  local android_bundle_manifest_sha256=$7
  local rollback_target_receipt_sha256=$8
  local timestamp directory temporary nginx_sha current_manifest_sha

  current_manifest_sha=$(sha256_file "$manifest")
  [[ "$current_manifest_sha" == "$RELEASE_MANIFEST_SHA256" ]] ||
    fail "release manifest changed during the Production operation"
  validate_receipt_target "$receipt"
  timestamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  directory=$(path_parent "$receipt")
  nginx_sha=$(sha256_file "$PRODUCTION_NGINX_CONFIG")
  temporary=$(mktemp "$directory/.production-receipt.tmp.XXXXXX") ||
    fail "cannot create temporary Production receipt"
  if ! jq --null-input \
    --arg operation "$operation" \
    --arg manifest_sha256 "$RELEASE_MANIFEST_SHA256" \
    --arg version "$RELEASE_VERSION" \
    --argjson version_code "$RELEASE_VERSION_CODE" \
    --arg git_sha "$RELEASE_GIT_SHA" \
    --argjson database_schema_version "$RELEASE_SCHEMA_VERSION" \
    --arg portal_image_digest "$PORTAL_IMAGE_DIGEST" \
    --arg server_image_digest "$SERVER_IMAGE_DIGEST" \
    --arg production_apk_file "$RELEASE_PRODUCTION_APK_FILE" \
    --argjson production_apk_size_bytes "$RELEASE_PRODUCTION_APK_SIZE" \
    --arg production_apk_sha256 "$RELEASE_PRODUCTION_APK_SHA256" \
    --arg apk_certificate_sha256 "$RELEASE_APK_CERTIFICATE_SHA256" \
    --arg portal_container_id "$RUNTIME_PORTAL_CONTAINER_ID" \
    --arg server_container_id "$RUNTIME_SERVER_CONTAINER_ID" \
    --arg postgres_container_id "$RUNTIME_POSTGRES_CONTAINER_ID" \
    --arg nginx_config_sha256 "$nginx_sha" \
    --rawfile nginx_config "$PRODUCTION_NGINX_CONFIG" \
    --arg postgres_backup_id "$postgres_backup_id" \
    --arg portal_backup_id "$portal_backup_id" \
    --arg android_bundle_manifest_sha256 "$android_bundle_manifest_sha256" \
    --arg previous_receipt_sha256 "$previous_receipt_sha256" \
    --arg rollback_target_receipt_sha256 "$rollback_target_receipt_sha256" \
    --arg recorded_at_utc "$timestamp" '
      {
        receipt_version: 1,
        environment: "production",
        operation: $operation,
        manifest_sha256: $manifest_sha256,
        version: $version,
        version_code: $version_code,
        git_sha: $git_sha,
        database_schema_version: $database_schema_version,
        portal_image_digest: $portal_image_digest,
        server_image_digest: $server_image_digest,
        production_apk_file: $production_apk_file,
        production_apk_size_bytes: $production_apk_size_bytes,
        production_apk_sha256: $production_apk_sha256,
        apk_certificate_sha256: $apk_certificate_sha256,
        portal_container_id: $portal_container_id,
        server_container_id: $server_container_id,
        postgres_container_id: $postgres_container_id,
        nginx_config_sha256: $nginx_config_sha256,
        nginx_config: $nginx_config,
        postgres_backup_id:
          (if $postgres_backup_id == "" then null else $postgres_backup_id end),
        portal_backup_id:
          (if $portal_backup_id == "" then null else $portal_backup_id end),
        android_bundle_manifest_sha256:
          (if $android_bundle_manifest_sha256 == "" then null
           else $android_bundle_manifest_sha256 end),
        previous_receipt_sha256:
          (if $previous_receipt_sha256 == "" then null
           else $previous_receipt_sha256 end),
        rollback_target_receipt_sha256:
          (if $rollback_target_receipt_sha256 == "" then null
           else $rollback_target_receipt_sha256 end),
        recorded_at_utc: $recorded_at_utc
      }
    ' >"$temporary"; then
    rm -f "$temporary"
    fail "cannot render Production receipt"
  fi
  chmod 0444 "$temporary"
  validate_receipt "$temporary"
  if ! ln "$temporary" "$receipt"; then
    rm -f "$temporary"
    fail "Production receipt already exists: $receipt"
  fi
  rm -f "$temporary"
  validate_receipt "$receipt"
}

validate_current_receipt_state() {
  local receipt=$1
  local prefix=$2

  use_release_state "$prefix"
  verify_deployment
  verify_live_nginx_against_receipt "$receipt"
  validate_backup_configurations
}

baseline_production() {
  local manifest=$1
  local receipt=$2

  validate_receipt_target "$receipt"
  acquire_production_lock
  validate_all "$manifest"
  validate_receipt_target "$receipt"
  verify_deployment
  validate_backup_configurations
  verify_live_nginx_against_template
  write_production_receipt "$manifest" "$receipt" baseline "" "" "" "" ""
}

deploy_production() {
  local manifest=$1
  local bundle=$2
  local current_receipt=$3
  local receipt=$4
  local initial_current_sha candidate

  validate_receipt_target "$receipt"
  validate_android_bundle "$bundle"
  load_receipt "$current_receipt" CURRENT
  initial_current_sha=$CURRENT_RECEIPT_SHA256
  acquire_production_lock
  validate_all "$manifest"
  save_release_state SELECTED
  validate_receipt_target "$receipt"
  validate_android_bundle "$bundle"
  load_receipt "$current_receipt" CURRENT
  [[ "$CURRENT_RECEIPT_SHA256" == "$initial_current_sha" ]] ||
    fail "current Production receipt changed while waiting for the deployment lock"
  validate_current_receipt_state "$current_receipt" CURRENT
  run_predeploy_backups

  use_release_state SELECTED
  compose pull postgres migrate server portal
  compose up --pull never --detach --no-build --wait --wait-timeout 90 postgres
  compose run --pull never --rm --no-deps migrate
  verify_database_schema
  compose up --pull never --detach --no-build --wait --wait-timeout 90 portal server
  verify_deployment
  validate_android_bundle "$bundle"
  publish_android_bundle "$bundle"
  candidate=$(mktemp)
  render_nginx "$candidate"
  install_nginx_configuration "$candidate" "$current_receipt"
  rm -f "$candidate"
  verify_deployment
  update_backup_configurations
  write_production_receipt \
    "$manifest" "$receipt" deploy "$CURRENT_RECEIPT_SHA256" \
    "$POSTGRES_BACKUP_ID" "$PORTAL_BACKUP_ID" \
    "$ANDROID_BUNDLE_MANIFEST_SHA256" ""
}

rollback_production() {
  local manifest=$1
  local current_receipt=$2
  local target_receipt=$3
  local receipt=$4
  local initial_current_sha initial_target_sha candidate target_candidate

  validate_receipt_target "$receipt"
  load_receipt "$current_receipt" CURRENT
  load_receipt "$target_receipt" TARGET
  initial_current_sha=$CURRENT_RECEIPT_SHA256
  initial_target_sha=$TARGET_RECEIPT_SHA256
  validate_receipt_matches_release TARGET
  acquire_production_lock
  validate_all "$manifest"
  save_release_state SELECTED
  validate_receipt_target "$receipt"
  load_receipt "$current_receipt" CURRENT
  load_receipt "$target_receipt" TARGET
  [[ "$CURRENT_RECEIPT_SHA256" == "$initial_current_sha" &&
    "$TARGET_RECEIPT_SHA256" == "$initial_target_sha" ]] ||
    fail "a Production receipt changed while waiting for the deployment lock"
  validate_receipt_matches_release TARGET
  validate_current_receipt_state "$current_receipt" CURRENT
  [[ "$CURRENT_SCHEMA_VERSION" == "$SELECTED_SCHEMA_VERSION" ]] ||
    fail "rollback requires the current database schema to match the target release"
  acquire_backup_locks

  use_release_state SELECTED
  compose up --pull never --detach --no-build --wait --wait-timeout 90 portal server
  verify_deployment
  candidate=$(mktemp)
  target_candidate=$(mktemp)
  render_nginx "$candidate"
  receipt_nginx_configuration "$target_receipt" "$target_candidate"
  if ! cmp -s "$candidate" "$target_candidate"; then
    rm -f "$candidate" "$target_candidate"
    fail "target receipt Nginx vhost does not match the selected release configuration"
  fi
  rm -f "$target_candidate"
  install_nginx_configuration "$candidate" "$current_receipt"
  rm -f "$candidate"
  verify_deployment
  update_backup_configurations
  write_production_receipt \
    "$manifest" "$receipt" rollback "$CURRENT_RECEIPT_SHA256" "" "" "" \
    "$TARGET_RECEIPT_SHA256"
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
  local bundle=""
  local receipt=""
  local current_receipt=""
  local target_receipt=""

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
      --bundle)
        (($# >= 2)) || fail "--bundle requires a value"
        bundle=$2
        shift 2
        ;;
      --receipt)
        (($# >= 2)) || fail "--receipt requires a value"
        receipt=$2
        shift 2
        ;;
      --current-receipt)
        (($# >= 2)) || fail "--current-receipt requires a value"
        current_receipt=$2
        shift 2
        ;;
      --target-receipt)
        (($# >= 2)) || fail "--target-receipt requires a value"
        target_receipt=$2
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
      [[ -z "$bundle$receipt$current_receipt$target_receipt" ]] ||
        fail "render-nginx accepts only --env-file and --output"
      [[ -n "$output" ]] || fail "render-nginx requires --output"
      validate_configuration
      render_nginx "$output"
      printf 'rendered=%s\n' "$output"
      ;;
    validate | verify | status | baseline | deploy | rollback)
      [[ -n "$manifest" ]] || fail "$command requires --manifest"
      [[ -z "$output" ]] || fail "$command does not accept --output"
      case "$command" in
        validate | verify | status)
          [[ -z "$bundle$receipt$current_receipt$target_receipt" ]] ||
            fail "$command accepts only --manifest and --env-file"
          ;;
        baseline)
          [[ -n "$receipt" ]] || fail "baseline requires --receipt"
          [[ -z "$bundle$current_receipt$target_receipt" ]] ||
            fail "baseline does not accept bundle or input receipts"
          ;;
        deploy)
          [[ -n "$bundle" ]] || fail "deploy requires --bundle"
          [[ -n "$current_receipt" ]] || fail "deploy requires --current-receipt"
          [[ -n "$receipt" ]] || fail "deploy requires --receipt"
          [[ -z "$target_receipt" ]] || fail "deploy does not accept --target-receipt"
          ;;
        rollback)
          [[ -n "$current_receipt" ]] || fail "rollback requires --current-receipt"
          [[ -n "$target_receipt" ]] || fail "rollback requires --target-receipt"
          [[ -n "$receipt" ]] || fail "rollback requires --receipt"
          [[ -z "$bundle" ]] || fail "rollback does not accept --bundle"
          ;;
      esac
      validate_all "$manifest"
      case "$command" in
        validate)
          printf 'version=%s git_sha=%s schema=%s validated=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA" "$RELEASE_SCHEMA_VERSION"
          ;;
        verify)
          verify_deployment
          printf 'version=%s verified=true\n' "$RELEASE_VERSION"
          ;;
        status)
          compose ps
          ;;
        baseline)
          baseline_production "$manifest" "$receipt"
          printf 'version=%s receipt=%s baseline=true\n' \
            "$RELEASE_VERSION" "$receipt"
          ;;
        deploy)
          deploy_production "$manifest" "$bundle" "$current_receipt" "$receipt"
          printf 'version=%s receipt=%s deployed=true apk_activated=false\n' \
            "$RELEASE_VERSION" "$receipt"
          ;;
        rollback)
          rollback_production \
            "$manifest" "$current_receipt" "$target_receipt" "$receipt"
          printf 'version=%s receipt=%s rolled_back=true database_restored=false\n' \
            "$RELEASE_VERSION" "$receipt"
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
