#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly compose_file="$staging_directory/compose.yaml"
readonly nginx_template="$staging_directory/nginx.conf.template"
readonly compose_project="xe3-speakup-staging"
readonly portal_image_repository="ghcr.io/1024xengineer/xe3-esl-portal"
readonly server_image_repository="ghcr.io/1024xengineer/xe3-esl-server"
readonly postgres_image="postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108"
readonly portal_data_volume="${compose_project}_portal_data"
readonly postgres_data_volume="${compose_project}_postgres_data"
readonly staging_lock_file="${SPEAKUP_STAGING_LOCK_FILE:-/run/lock/xe3-speakup-staging/deploy.lock}"
readonly portal_health_url="http://127.0.0.1:28082/"
readonly server_health_url="http://127.0.0.1:28083/health"
readonly server_readiness_url="http://127.0.0.1:28083/readyz"

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh validate --manifest FILE --env-file FILE
  manage.sh deploy --manifest FILE --env-file FILE --receipt FILE
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

path_lstat_mode() {
  local path=$1
  local mode

  if mode=$(stat -c '%a' -- "$path" 2>/dev/null); then
    :
  elif mode=$(stat -f '%Lp' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$mode"
}

path_lstat_owner() {
  local path=$1
  local owner

  if owner=$(stat -c '%u' -- "$path" 2>/dev/null); then
    :
  elif owner=$(stat -f '%u' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$owner"
}

path_lstat_identity() {
  local path=$1
  local identity

  if identity=$(stat -c '%i' -- "$path" 2>/dev/null); then
    :
  elif identity=$(stat -f '%i' "$path" 2>/dev/null); then
    :
  else
    return 1
  fi
  printf '%s\n' "$identity"
}

safe_ancestor_mode() {
  local mode=$1
  local permissions

  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || return 1
  permissions=${mode: -3}
  if [[ "${permissions:1:1}" != [2367] &&
    "${permissions:2:1}" != [2367] ]]; then
    return 0
  fi

  # A root/current-UID sticky directory such as /tmp cannot have an existing
  # child replaced by another UID. Other group/world-writable ancestors are
  # unsafe for deployment inputs.
  [[ ${#mode} -eq 4 && "${mode:0:1}" == [1357] ]]
}

require_safe_path_ancestors() {
  local description=$1
  local path=$2
  local parent remaining component current=/ owner mode identity
  local -a components

  valid_absolute_path "$path" ||
    fail "$description must use a safe absolute path: $path"
  parent=$(path_parent "$path")
  remaining=${parent#/}
  IFS='/' read -r -a components <<< "$remaining"

  for component in "" "${components[@]}"; do
    if [[ -n "$component" ]]; then
      if [[ "$current" == / ]]; then
        current="/$component"
      else
        current="$current/$component"
      fi
    fi
    [[ ! -L "$current" ]] ||
      fail "$description has a symbolic-link ancestor: $current"
    identity=$(path_lstat_identity "$current") ||
      fail "cannot inspect identity for $description ancestor: $current"
    [[ -d "$current" && -x "$current" ]] ||
      fail "$description ancestor is not a searchable directory: $current"
    owner=$(path_lstat_owner "$current") ||
      fail "cannot inspect owner for $description ancestor: $current"
    [[ "$owner" == 0 || "$owner" == "$(id -u)" ]] ||
      fail "$description ancestor has an untrusted owner: $current"
    mode=$(path_lstat_mode "$current") ||
      fail "cannot inspect permissions for $description ancestor: $current"
    safe_ancestor_mode "$mode" ||
      fail "$description ancestor has unsafe permissions: $current"
    [[ ! -L "$current" &&
      "$(path_lstat_identity "$current")" == "$identity" ]] ||
      fail "$description ancestor changed during validation: $current"
  done
}

normalize_absolute_path() {
  local path=$1
  local component normalized=
  local -a components stack

  [[ "$path" == /* && "$path" =~ ^/[A-Za-z0-9._/-]+$ ]] || return 1
  IFS='/' read -r -a components <<< "$path"
  for component in "${components[@]}"; do
    case "$component" in
      "" | .)
        ;;
      ..)
        ((${#stack[@]} > 0)) || return 1
        unset "stack[$((${#stack[@]} - 1))]"
        ;;
      *)
        stack+=("$component")
        ;;
    esac
  done
  for component in "${stack[@]}"; do
    normalized="$normalized/$component"
  done
  printf '%s\n' "${normalized:-/}"
}

resolve_allowed_final_symlink() {
  local description=$1
  local file=$2
  local link_target resolved

  require_safe_path_ancestors "$description" "$file"
  if [[ ! -L "$file" ]]; then
    printf '%s\n' "$file"
    return
  fi

  link_target=$(readlink "$file") ||
    fail "cannot read $description symbolic link: $file"
  if [[ "$link_target" == /* ]]; then
    resolved=$(normalize_absolute_path "$link_target") ||
      fail "$description symbolic-link target is unsafe: $file"
  else
    resolved=$(normalize_absolute_path \
      "$(path_parent "$file")/$link_target") ||
      fail "$description symbolic-link target is unsafe: $file"
  fi
  require_safe_path_ancestors "$description target" "$resolved"
  [[ ! -L "$resolved" ]] ||
    fail "$description symbolic-link target must not be another link: $resolved"
  [[ -L "$file" && "$(readlink "$file")" == "$link_target" ]] ||
    fail "$description symbolic link changed during validation: $file"
  printf '%s\n' "$resolved"
}

require_current_owner() {
  local description=$1
  local path=$2
  local owner

  owner=$(path_owner "$path") ||
    fail "cannot inspect owner for $description: $path"
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current execution UID: $path"
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

  require_safe_path_ancestors "$description" "$file"
  [[ ! -L "$file" ]] || fail "$description must not be a symbolic link: $file"
  require_regular_file "$description" "$file"
  require_current_owner "$description" "$file"
  mode=$(path_mode "$file") ||
    fail "cannot inspect permissions for $description: $file"
  case "$mode" in
    400 | 600)
      ;;
    *)
      fail "$description must have mode 0400 or 0600: $file"
      ;;
  esac
}

require_private_key_file() {
  local description=$1
  local file=$2
  local target
  local mode

  # Only TLS inputs permit a final Certbot live/*.pem symlink. The declared
  # path, resolved archive target, and both ancestor chains are validated.
  target=$(resolve_allowed_final_symlink "$description" "$file")
  require_regular_file "$description" "$target"
  require_current_owner "$description target" "$target"
  mode=$(path_mode "$target") ||
    fail "cannot inspect permissions for $description target: $target"
  case "$mode" in
    400 | 600)
      ;;
    *)
      fail "$description target must have mode 0400 or 0600: $file"
      ;;
  esac
}

require_public_certificate() {
  local file=$1
  local target
  local mode

  target=$(resolve_allowed_final_symlink "TLS certificate" "$file")
  require_regular_file "TLS certificate" "$target"
  require_current_owner "TLS certificate target" "$target"
  mode=$(path_mode "$target") ||
    fail "cannot inspect permissions for TLS certificate target: $target"
  case "$mode" in
    400 | 440 | 444 | 600 | 640 | 644)
      ;;
    *)
      fail "TLS certificate must not be group- or world-writable: $file"
      ;;
  esac
}

require_owned_directory() {
  local description=$1
  local directory=$2
  local allowed_modes=$3
  local mode

  require_safe_path_ancestors "$description" "$directory"
  [[ ! -L "$directory" ]] ||
    fail "$description must not be a symbolic link: $directory"
  [[ -d "$directory" ]] || fail "$description does not exist: $directory"
  [[ -x "$directory" ]] || fail "$description is not searchable: $directory"
  require_current_owner "$description" "$directory"
  mode=$(path_mode "$directory") ||
    fail "cannot inspect permissions for $description: $directory"
  case " $allowed_modes " in
    *" $mode "*)
      ;;
    *)
      fail "$description has unsafe permissions: $directory"
      ;;
  esac
}

path_parent() {
  local path=$1
  local parent=${path%/*}

  [[ -n "$parent" ]] || parent=/
  printf '%s\n' "$parent"
}

sha256_file() {
  local file=$1
  local digest

  if command -v sha256sum >/dev/null 2>&1; then
    digest=$(sha256sum -- "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    digest=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    fail "sha256sum or shasum is required"
  fi
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || fail "cannot hash file: $file"
  printf '%s\n' "$digest"
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
  local line name value line_number=0

  require_private_file "environment file" "$file"

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
    line_number=$((line_number + 1))
    line=${line%$'\r'}
    case "$line" in
      "" | \#*)
        continue
        ;;
    esac
    [[ "$line" == *=* ]] ||
      fail "invalid environment entry at line $line_number"
    name=${line%%=*}
    value=${line#*=}
    [[ "$name" =~ ^[A-Z][A-Z0-9_]*$ ]] ||
      fail "invalid environment key at line $line_number"
    allowed_configuration_key "$name" ||
      fail "unsupported environment key at line $line_number"
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

  require_private_file "server environment file" "$STAGING_SERVER_ENV_FILE"
  require_public_certificate "$STAGING_TLS_CERTIFICATE"
  require_private_key_file "TLS certificate key" "$STAGING_TLS_CERTIFICATE_KEY"
  require_private_file "htpasswd file" "$STAGING_HTPASSWD_FILE"
  require_owned_directory "ACME root directory" "$STAGING_ACME_ROOT" \
    "700 750 755"
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
  RELEASE_MANIFEST_SHA256=$(sha256_file "$file")
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

validate_receipt_target() {
  local receipt=$1
  local directory

  valid_absolute_path "$receipt" || fail "--receipt must be a safe absolute path"
  [[ ! -e "$receipt" && ! -L "$receipt" ]] ||
    fail "deployment receipt already exists: $receipt"
  directory=$(path_parent "$receipt")
  require_owned_directory "deployment receipt directory" "$directory" \
    "700 750 755"
  [[ -w "$directory" ]] ||
    fail "deployment receipt directory is not writable: $directory"
}

require_safe_lock_file() {
  local file=$1
  local mode

  [[ ! -L "$file" ]] || fail "deployment lock must not be a symbolic link: $file"
  [[ -f "$file" ]] || fail "deployment lock is not a regular file: $file"
  [[ -r "$file" && -w "$file" ]] ||
    fail "deployment lock is not readable and writable: $file"
  require_current_owner "deployment lock" "$file"
  mode=$(path_mode "$file") ||
    fail "cannot inspect permissions for deployment lock: $file"
  [[ "$mode" == 600 ]] ||
    fail "deployment lock must have mode 0600: $file"
}

require_lock_fd_matches_path() {
  local opened_identity path_file_identity

  require_safe_lock_file "$staging_lock_file"
  opened_identity=$(path_identity /dev/fd/9) ||
    fail "cannot inspect the opened deployment lock"
  path_file_identity=$(path_identity "$staging_lock_file") ||
    fail "cannot inspect deployment lock identity: $staging_lock_file"
  [[ "$opened_identity" == "$path_file_identity" ]] ||
    fail "deployment lock changed while it was being acquired"
}

acquire_deployment_lock() {
  local directory

  require_command flock
  valid_absolute_path "$staging_lock_file" ||
    fail "deployment lock must be a safe absolute path"
  directory=$(path_parent "$staging_lock_file")
  require_owned_directory "deployment lock directory" "$directory" \
    "700 750 755"
  if [[ ! -e "$staging_lock_file" && ! -L "$staging_lock_file" ]]; then
    if ! (umask 077 && set -o noclobber && : > "$staging_lock_file") \
      2>/dev/null; then
      [[ -e "$staging_lock_file" || -L "$staging_lock_file" ]] ||
        fail "cannot create deployment lock: $staging_lock_file"
    fi
  fi
  require_safe_lock_file "$staging_lock_file"
  exec 9>> "$staging_lock_file" ||
    fail "cannot open deployment lock: $staging_lock_file"
  require_lock_fd_matches_path
  flock --nonblock 9 ||
    fail "another Staging deploy or down operation is already running"
  require_lock_fd_matches_path
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

verify_database_schema() {
  local output version

  if ! output=$(compose run --pull never --rm --no-deps migrate \
    /usr/local/bin/speakup-migrate version); then
    fail "cannot verify the Staging database schema"
  fi
  [[ "$output" =~ ^version=([1-9][0-9]*)\ dirty=false$ ]] ||
    fail "database schema output must be exactly version=N dirty=false"
  version=${BASH_REMATCH[1]}
  [[ "$version" == "$RELEASE_SCHEMA_VERSION" ]] ||
    fail "database schema version does not match the release manifest"
}

verify_project_service_set() {
  local services

  services=$(
    docker ps --all \
      --filter "label=com.docker.compose.project=$compose_project" \
      --format '{{.Label "com.docker.compose.service"}}'
  )
  services=$(printf '%s\n' "$services" | LC_ALL=C sort)
  [[ "$services" == $'portal\npostgres\nserver' ]] ||
    fail "$compose_project must contain exactly portal, postgres, and server"
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
  [[ -n "$containers" ]] || fail "$compose_project/$service is not running"
  [[ "$containers" != *$'\n'* ]] ||
    fail "$compose_project/$service has multiple running containers"
  [[ "$containers" =~ ^[0-9a-f]{12,64}$ ]] ||
    fail "$compose_project/$service returned an invalid container ID"
  printf '%s\n' "$containers"
}

verify_runtime_image() {
  local service=$1
  local expected_image=$2
  local release_labels=$3
  local inspection

  inspection=$(docker image inspect "$expected_image") ||
    fail "cannot inspect the pinned $service image"
  jq --exit-status --slurp \
    --arg expected_image "$expected_image" \
    --arg release_labels "$release_labels" \
    --arg source "https://github.com/1024XEngineer/XE3-ESL" \
    --arg revision "$RELEASE_GIT_SHA" \
    --arg version "$RELEASE_VERSION" '
      length == 1 and
      (.[0] |
        type == "array" and length == 1 and
        (.[0] |
          .Os == "linux" and
          .Architecture == "amd64" and
          ($release_labels != "true" or
            (((.RepoDigests // []) | index($expected_image)) != null and
             .Config.Labels["org.opencontainers.image.source"] == $source and
             .Config.Labels["org.opencontainers.image.revision"] == $revision and
             .Config.Labels["org.opencontainers.image.version"] == $version))
        )
      )
    ' <<< "$inspection" >/dev/null ||
    fail "$service image platform, digest, or OCI labels are invalid"
}

inspect_runtime_service() {
  local service=$1
  local expected_image=$2
  local release_labels=$3
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
          (.Id | type == "string" and
            test("^[0-9a-f]{64}$") and startswith($container_id)) and
          .Config.Labels["com.docker.compose.project"] == $project and
          .Config.Labels["com.docker.compose.service"] == $service and
          .Config.Image == $expected_image and
          .State.Status == "running" and
          .State.Running == true and
          .State.Health.Status == "healthy"
        )
      )
    ' <<< "$inspection" >/dev/null ||
    fail "$compose_project/$service identity, image, or health is invalid"
  verify_runtime_image "$service" "$expected_image" "$release_labels"
  printf '%s\n' "$inspection"
}

verify_runtime_network() {
  local logical_name=$1
  local internal=$2
  local name="${compose_project}_${logical_name}"
  local inspection

  inspection=$(docker network inspect "$name") ||
    fail "cannot inspect Staging network: $name"
  jq --exit-status --slurp \
    --arg name "$name" \
    --arg project "$compose_project" \
    --arg logical_name "$logical_name" \
    --argjson internal "$internal" '
      length == 1 and
      (.[0] |
        type == "array" and length == 1 and
        (.[0] |
          .Name == $name and
          .Internal == $internal and
          .Labels["com.docker.compose.project"] == $project and
          .Labels["com.docker.compose.network"] == $logical_name
        )
      )
    ' <<< "$inspection" >/dev/null ||
    fail "Staging network identity or isolation is invalid: $name"
}

verify_runtime_volume() {
  local logical_name=$1
  local name="${compose_project}_${logical_name}"
  local inspection

  inspection=$(docker volume inspect "$name") ||
    fail "cannot inspect Staging volume: $name"
  jq --exit-status --slurp \
    --arg name "$name" \
    --arg project "$compose_project" \
    --arg logical_name "$logical_name" '
      length == 1 and
      (.[0] |
        type == "array" and length == 1 and
        (.[0] |
          .Name == $name and
          .Labels["com.docker.compose.project"] == $project and
          .Labels["com.docker.compose.volume"] == $logical_name
        )
      )
    ' <<< "$inspection" >/dev/null ||
    fail "Staging volume identity is invalid: $name"
}

verify_runtime() {
  local portal server postgres

  verify_project_service_set
  verify_runtime_network portal_edge false
  verify_runtime_network server_edge false
  verify_runtime_network database true
  verify_runtime_volume portal_data
  verify_runtime_volume postgres_data

  portal=$(inspect_runtime_service \
    portal "$portal_image_repository@$PORTAL_IMAGE_DIGEST" true)
  jq --exit-status --arg network "${compose_project}_portal_edge" '
    def exact_env($key; $value):
      [.Config.Env[]? | select(startswith($key + "="))] ==
        [($key + "=" + $value)];
    .[0] |
    exact_env("PORTAL_ADMIN_PASSWORD"; env.PORTAL_ADMIN_PASSWORD) and
    exact_env("PORTAL_SQLITE_PATH"; "/app/.wrangler/portal.sqlite") and
    exact_env("VINEXT_TRUSTED_HOSTS"; env.STAGING_PORTAL_HOST) and
    (.Mounts | length == 1) and
    .Mounts[0].Type == "volume" and
    .Mounts[0].Name == "xe3-speakup-staging_portal_data" and
    .Mounts[0].Destination == "/app/.wrangler" and
    .Mounts[0].RW == true and
    .NetworkSettings.Ports == {
      "3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "28082"}]
    } and
    (.NetworkSettings.Networks | keys) == [$network]
  ' <<< "$portal" >/dev/null 2>&1 ||
    fail "$compose_project/portal runtime configuration is invalid"

  server=$(inspect_runtime_service \
    server "$server_image_repository@$SERVER_IMAGE_DIGEST" true)
  jq --exit-status \
    --arg database "${compose_project}_database" \
    --arg edge "${compose_project}_server_edge" '
      def exact_env($key; $value):
        [.Config.Env[]? | select(startswith($key + "="))] ==
          [($key + "=" + $value)];
      .[0] |
      exact_env("DATABASE_URL";
        "postgres://" + env.STAGING_POSTGRES_USER + ":" +
        env.STAGING_POSTGRES_PASSWORD + "@postgres:5432/" +
        env.STAGING_POSTGRES_DB + "?sslmode=disable") and
      exact_env("SERVER_HOST"; "0.0.0.0") and
      exact_env("SERVER_PORT"; "8080") and
      (.Mounts | length == 0) and
      .NetworkSettings.Ports == {
        "8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "28083"}]
      } and
      (.NetworkSettings.Networks | keys) == ([$database, $edge] | sort)
    ' <<< "$server" >/dev/null 2>&1 ||
    fail "$compose_project/server runtime configuration is invalid"

  postgres=$(inspect_runtime_service postgres "$postgres_image" false)
  jq --exit-status --arg network "${compose_project}_database" '
    def exact_env($key; $value):
      [.Config.Env[]? | select(startswith($key + "="))] ==
        [($key + "=" + $value)];
    .[0] |
    exact_env("POSTGRES_DB"; env.STAGING_POSTGRES_DB) and
    exact_env("POSTGRES_USER"; env.STAGING_POSTGRES_USER) and
    exact_env("POSTGRES_PASSWORD"; env.STAGING_POSTGRES_PASSWORD) and
    (.Mounts | length == 1) and
    .Mounts[0].Type == "volume" and
    .Mounts[0].Name == "xe3-speakup-staging_postgres_data" and
    .Mounts[0].Destination == "/var/lib/postgresql" and
    .Mounts[0].RW == true and
    ([.NetworkSettings.Ports // {} | to_entries[] | select(.value != null)] |
      length == 0) and
    (.NetworkSettings.Networks | keys) == [$network]
  ' <<< "$postgres" >/dev/null 2>&1 ||
    fail "$compose_project/postgres runtime configuration is invalid"

  RUNTIME_PORTAL_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<< "$portal")
  RUNTIME_SERVER_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<< "$server")
  RUNTIME_POSTGRES_CONTAINER_ID=$(jq --raw-output '.[0].Id' <<< "$postgres")
}

verify_deployment() {
  verify_runtime
  verify_database_schema
  verify_endpoints
}

write_deployment_receipt() {
  local manifest=$1
  local receipt=$2
  local current_manifest_sha timestamp directory temporary

  current_manifest_sha=$(sha256_file "$manifest")
  [[ "$current_manifest_sha" == "$RELEASE_MANIFEST_SHA256" ]] ||
    fail "release manifest changed during deployment"
  validate_receipt_target "$receipt"
  timestamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  directory=$(path_parent "$receipt")
  temporary=$(mktemp "$directory/.staging-receipt.tmp.XXXXXX") ||
    fail "cannot create temporary deployment receipt"
  if ! jq --null-input \
    --arg manifest_sha256 "$RELEASE_MANIFEST_SHA256" \
    --arg version "$RELEASE_VERSION" \
    --arg git_sha "$RELEASE_GIT_SHA" \
    --argjson database_schema_version "$RELEASE_SCHEMA_VERSION" \
    --arg portal_image_digest "$PORTAL_IMAGE_DIGEST" \
    --arg server_image_digest "$SERVER_IMAGE_DIGEST" \
    --arg portal_container_id "$RUNTIME_PORTAL_CONTAINER_ID" \
    --arg server_container_id "$RUNTIME_SERVER_CONTAINER_ID" \
    --arg postgres_container_id "$RUNTIME_POSTGRES_CONTAINER_ID" \
    --arg deployed_at_utc "$timestamp" '
      {
        receipt_version: 1,
        manifest_sha256: $manifest_sha256,
        version: $version,
        git_sha: $git_sha,
        database_schema_version: $database_schema_version,
        portal_image_digest: $portal_image_digest,
        server_image_digest: $server_image_digest,
        portal_container_id: $portal_container_id,
        server_container_id: $server_container_id,
        postgres_container_id: $postgres_container_id,
        deployed_at_utc: $deployed_at_utc
      }
    ' > "$temporary"; then
    rm -f "$temporary"
    fail "cannot render deployment receipt"
  fi
  chmod 0644 "$temporary"
  if ! ln "$temporary" "$receipt"; then
    rm -f "$temporary"
    fail "deployment receipt already exists: $receipt"
  fi
  rm -f "$temporary"
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
  local receipt=""

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
      --receipt)
        (($# >= 2)) || fail "--receipt requires a value"
        receipt=$2
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
      [[ -z "$receipt" ]] || fail "render-nginx does not accept --receipt"
      [[ -n "$output" ]] || fail "render-nginx requires --output"
      validate_configuration
      render_nginx "$output"
      printf 'rendered=%s\n' "$output"
      ;;
    validate | deploy | verify | status | down)
      [[ -n "$manifest" ]] || fail "$command requires --manifest"
      [[ -z "$output" ]] || fail "$command does not accept --output"
      if [[ "$command" == deploy ]]; then
        [[ -n "$receipt" ]] || fail "deploy requires --receipt"
      else
        [[ -z "$receipt" ]] || fail "$command does not accept --receipt"
      fi
      validate_all "$manifest"
      case "$command" in
        validate)
          printf 'version=%s git_sha=%s schema=%s validated=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA" "$RELEASE_SCHEMA_VERSION"
          ;;
        deploy)
          validate_receipt_target "$receipt"
          acquire_deployment_lock
          validate_all "$manifest"
          validate_receipt_target "$receipt"
          compose pull postgres migrate server portal
          compose up --pull never --detach --no-build --wait --wait-timeout 90 \
            postgres
          compose run --pull never --rm --no-deps migrate
          verify_database_schema
          compose up --pull never --detach --no-build --wait --wait-timeout 90 \
            portal server
          verify_deployment
          write_deployment_receipt "$manifest" "$receipt"
          printf 'version=%s git_sha=%s receipt=%s deployed=true\n' \
            "$RELEASE_VERSION" "$RELEASE_GIT_SHA" "$receipt"
          ;;
        verify)
          verify_deployment
          printf 'version=%s verified=true\n' "$RELEASE_VERSION"
          ;;
        status)
          compose ps
          ;;
        down)
          acquire_deployment_lock
          validate_all "$manifest"
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
