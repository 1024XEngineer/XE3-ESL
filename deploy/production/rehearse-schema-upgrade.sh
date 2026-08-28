#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly production_manager="$production_directory/manage.sh"
readonly postgres_image='postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
readonly server_repository='ghcr.io/1024xengineer/xe3-esl-server'
readonly resource_label='com.xengineer.speakup.production-rehearsal'
readonly run_label='com.xengineer.speakup.production-rehearsal-run-id'
readonly source_project='xe3-speakup-production'
readonly source_service='postgres'
readonly source_volume='xe3-speakup-postgres-data'
readonly source_schema=9
readonly target_schema=15
readonly container_cpus='1.0'
readonly container_memory='512m'
readonly container_memory_bytes=536870912
readonly container_pids_limit=256
readonly container_log_max_size='10m'
readonly container_log_max_file='3'

execute=false
backup_directory=''
manifest=''
manifest_snapshot_directory=''
manifest_snapshot=''
previous_server_image=''
forward_server_image=''
validated_forward_server_image=''
server_environment=''
receipt=''
lock_timeout_seconds=''
run_id=''
network_name=''
volume_name=''
declare -a container_names=()
declare -a container_ids=()
postgres_container_id=''
last_created_container_id=''
receipt_ready=false
docker_touched=false
cleanup_verified=false
phase='preflight'
started_at_seconds=0
restore_duration_seconds=0
migration_duration_seconds=0
total_duration_seconds=0
release_version=''
release_git_sha=''
release_manifest_sha256=''
candidate_server_image=''
selected_backup_id=''
selected_backup_sha256=''
selected_database=''
selected_database_user=''
backup_deployment_version=''
backup_git_sha=''
forward_server_image_version=''
forward_server_image_revision=''
forward_hotfix_status='not_provided'
schema_verified=false
views_verified=false
user_behavior_views_verified=false
view_privileges_verified=false
schema_10_14_contracts_verified=false
constraint_verified=false
candidate_readiness_verified=false
previous_readiness_verified=false
rollback_guard_verified=false
idempotent_migration_verified=false
same_schema_candidate_redeploy_verified=false

usage() {
  cat >&2 <<'EOF'
Usage:
  rehearse-schema-upgrade.sh [--execute] \
    --backup DIRECTORY \
    --manifest FILE \
    --previous-server-image ghcr.io/1024xengineer/xe3-esl-server@sha256:DIGEST \
    [--forward-server-image ghcr.io/1024xengineer/xe3-esl-server@sha256:DIGEST] \
    --server-env-file FILE \
    --receipt FILE \
    --lock-timeout-seconds N

Without --execute this performs a read-only dry-run. It validates metadata but
does not read database.dump, inspect images, or create Docker resources.
EOF
}

fail() {
  printf 'Production schema rehearsal error: %s\n' "$*" >&2
  exit 1
}

now_seconds() {
  date -u +%s
}

write_receipt() {
  local status=$1 temporary recorded_at parent

  parent=${receipt%/*}
  temporary=$(mktemp "$parent/.production-rehearsal-receipt.XXXXXX") || return 1
  recorded_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) || {
    rm -f -- "$temporary"
    return 1
  }
  if ! jq --null-input --sort-keys \
    --arg status "$status" \
    --arg failed_step "$phase" \
    --arg run_id "$run_id" \
    --arg backup_id "$selected_backup_id" \
    --arg backup_sha256 "$selected_backup_sha256" \
    --arg manifest_sha256 "$release_manifest_sha256" \
    --arg version "$release_version" \
    --arg git_sha "$release_git_sha" \
    --arg candidate_server_image "$candidate_server_image" \
    --arg previous_server_image "$previous_server_image" \
    --arg forward_server_image "$validated_forward_server_image" \
    --arg forward_server_image_version "$forward_server_image_version" \
    --arg forward_server_image_revision "$forward_server_image_revision" \
    --arg forward_hotfix_status "$forward_hotfix_status" \
    --arg recorded_at "$recorded_at" \
    --argjson source_schema "$source_schema" \
    --argjson target_schema "$target_schema" \
    --argjson lock_timeout_seconds "$lock_timeout_seconds" \
    --argjson restore_duration_seconds "$restore_duration_seconds" \
    --argjson migration_duration_seconds "$migration_duration_seconds" \
    --argjson total_duration_seconds "$total_duration_seconds" \
    --argjson schema_verified "$schema_verified" \
    --argjson views_verified "$views_verified" \
    --argjson user_behavior_views_verified "$user_behavior_views_verified" \
    --argjson view_privileges_verified "$view_privileges_verified" \
    --argjson schema_10_14_contracts_verified \
      "$schema_10_14_contracts_verified" \
    --argjson constraint_verified "$constraint_verified" \
    --argjson candidate_readiness_verified "$candidate_readiness_verified" \
    --argjson previous_readiness_verified "$previous_readiness_verified" \
    --argjson rollback_guard_verified "$rollback_guard_verified" \
    --argjson idempotent_migration_verified "$idempotent_migration_verified" \
    --argjson same_schema_candidate_redeploy_verified \
      "$same_schema_candidate_redeploy_verified" \
    --argjson cleanup_verified "$cleanup_verified" '
      {
        receipt_version: 1,
        operation: "production_schema_upgrade_rehearsal",
        environment: "isolated",
        status: $status,
        failed_step: (if $status == "failed" then $failed_step else null end),
        run_id: $run_id,
        backup_id: (if $backup_id == "" then null else $backup_id end),
        backup_sha256:
          (if $backup_sha256 == "" then null else $backup_sha256 end),
        manifest_sha256:
          (if $manifest_sha256 == "" then null else $manifest_sha256 end),
        version: (if $version == "" then null else $version end),
        git_sha: (if $git_sha == "" then null else $git_sha end),
        candidate_server_image:
          (if $candidate_server_image == "" then null else $candidate_server_image end),
        previous_server_image:
          (if $previous_server_image == "" then null else $previous_server_image end),
        forward_server_image:
          (if $forward_server_image == "" then null else $forward_server_image end),
        forward_server_image_version:
          (if $forward_server_image_version == "" then null
           else $forward_server_image_version end),
        forward_server_image_revision:
          (if $forward_server_image_revision == "" then null
           else $forward_server_image_revision end),
        source_schema: $source_schema,
        target_schema: $target_schema,
        lock_timeout_seconds: $lock_timeout_seconds,
        restore_duration_seconds: $restore_duration_seconds,
        migration_duration_seconds: $migration_duration_seconds,
        total_duration_seconds: $total_duration_seconds,
        checks: {
          clean_target_schema: $schema_verified,
          product_health_views: $views_verified,
          user_behavior_views: $user_behavior_views_verified,
          view_public_privileges_revoked: $view_privileges_verified,
          schema_10_14_contracts: $schema_10_14_contracts_verified,
          ielts_evaluation_constraint: $constraint_verified,
          candidate_readiness: $candidate_readiness_verified,
          previous_image_readiness_only: $previous_readiness_verified,
          previous_image_profile_processing: "not_verified",
          schema9_rollback_guard: $rollback_guard_verified,
          idempotent_migration: $idempotent_migration_verified,
          same_schema_candidate_redeploy: $same_schema_candidate_redeploy_verified,
          forward_hotfix_image: $forward_hotfix_status,
          owned_resource_cleanup: $cleanup_verified
        },
        recorded_at_utc: $recorded_at
      }
    ' >"$temporary"; then
    rm -f -- "$temporary"
    return 1
  fi
  chmod 0600 "$temporary" || {
    rm -f -- "$temporary"
    return 1
  }
  if ! ln "$temporary" "$receipt"; then
    rm -f -- "$temporary"
    return 1
  fi
  rm -f -- "$temporary"
  [[ "$(path_mode "$receipt")" == 600 ]]
}

cleanup_on_exit() {
  local status=$?

  trap - EXIT INT HUP TERM
  set +e
  if [[ "$docker_touched" == true ]]; then
    if cleanup_owned_resources; then
      cleanup_verified=true
    else
      cleanup_verified=false
      status=1
      phase='cleanup'
    fi
  elif [[ "$execute" == true ]]; then
    cleanup_verified=true
  fi
  if ! cleanup_manifest_snapshot; then
    status=1
    phase='cleanup'
  fi
  if ((started_at_seconds > 0)); then
    total_duration_seconds=$(( $(now_seconds) - started_at_seconds ))
  fi
  if [[ "$execute" == true && "$receipt_ready" == true ]]; then
    if ((status == 0)); then
      if write_receipt succeeded; then
        printf 'backup_id=%s version=%s source_schema=%s target_schema=%s receipt=%s rehearsal=verified\n' \
          "$selected_backup_id" "$release_version" "$source_schema" \
          "$target_schema" "$receipt"
      else
        status=1
      fi
    else
      write_receipt failed || status=1
    fi
  fi
  exit "$status"
}

cleanup_manifest_snapshot() {
  local name

  [[ -n "$manifest_snapshot_directory" ]] || return 0
  name=${manifest_snapshot_directory##*/}
  [[ "$name" == xe3-production-rehearsal-manifest.* &&
    -d "$manifest_snapshot_directory" && ! -L "$manifest_snapshot_directory" &&
    "$(path_owner "$manifest_snapshot_directory")" == "$EUID" ]] || return 1
  if [[ -e "$manifest_snapshot" || -L "$manifest_snapshot" ]]; then
    [[ "$manifest_snapshot" == "$manifest_snapshot_directory/release-manifest.json" &&
      -f "$manifest_snapshot" && ! -L "$manifest_snapshot" &&
      "$(path_owner "$manifest_snapshot")" == "$EUID" ]] || return 1
    rm -- "$manifest_snapshot" || return 1
  fi
  rmdir -- "$manifest_snapshot_directory" || return 1
  manifest_snapshot_directory=''
  manifest_snapshot=''
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

path_mode() {
  if stat -c '%a' -- "$1" >/dev/null 2>&1; then
    stat -c '%a' -- "$1"
  else
    stat -f '%Lp' "$1"
  fi
}

path_owner() {
  if stat -c '%u' -- "$1" >/dev/null 2>&1; then
    stat -c '%u' -- "$1"
  else
    stat -f '%u' "$1"
  fi
}

file_size() {
  if stat -c '%s' -- "$1" >/dev/null 2>&1; then
    stat -c '%s' -- "$1"
  else
    stat -f '%z' "$1"
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -- "$1" | awk '{print $1}'
  else
    fail 'sha256sum or shasum is required'
  fi
}

valid_absolute_path() {
  local value=$1

  [[ "$value" =~ ^/[A-Za-z0-9._/-]+$ ]] &&
    [[ "$value" != / && "$value" != *//* ]] &&
    [[ "$value" != */../* && "$value" != */.. ]] &&
    [[ "$value" != */./* && "$value" != */. ]]
}

require_regular_file() {
  local description=$1
  local file=$2

  [[ -f "$file" && ! -L "$file" && -r "$file" && -s "$file" ]] ||
    fail "$description must be a readable non-empty regular file"
}

require_private_file() {
  local description=$1
  local file=$2
  local mode

  require_regular_file "$description" "$file"
  [[ "$(path_owner "$file")" == "$EUID" ]] ||
    fail "$description must be owned by the current user"
  mode=$(path_mode "$file") || fail "cannot inspect $description permissions"
  [[ "$mode" == 400 || "$mode" == 600 ]] ||
    fail "$description must have mode 0400 or 0600"
}

require_receipt_target() {
  local parent mode

  valid_absolute_path "$receipt" || fail '--receipt must be a safe absolute path'
  [[ ! -e "$receipt" && ! -L "$receipt" ]] ||
    fail 'rehearsal receipt already exists'
  parent=${receipt%/*}
  [[ -d "$parent" && ! -L "$parent" && -w "$parent" ]] ||
    fail 'rehearsal receipt parent must be a writable real directory'
  [[ "$(path_owner "$parent")" == "$EUID" ]] ||
    fail 'rehearsal receipt parent must be owned by the current user'
  mode=$(path_mode "$parent") || fail 'cannot inspect receipt parent permissions'
  (( (8#$mode & 0022) == 0 )) ||
    fail 'rehearsal receipt parent cannot be group or world writable'
}

validate_manifest() {
  local output mode canonical temporary_base

  require_regular_file 'release manifest' "$manifest"
  valid_absolute_path "$manifest" || fail '--manifest must be a safe absolute path'
  canonical=$(realpath "$manifest") || fail 'cannot resolve release manifest'
  [[ "$canonical" == "$manifest" ]] || fail '--manifest must be a canonical path'
  [[ "$(path_owner "$manifest")" == "$EUID" ]] ||
    fail 'release manifest must be owned by the current user'
  mode=$(path_mode "$manifest") || fail 'cannot inspect release manifest permissions'
  (( (8#$mode & 0022) == 0 )) ||
    fail 'release manifest cannot be group or world writable'

  temporary_base=${TMPDIR:-/tmp}
  temporary_base=${temporary_base%/}
  manifest_snapshot_directory=$(mktemp -d \
    "$temporary_base/xe3-production-rehearsal-manifest.XXXXXX") ||
    fail 'cannot create private manifest snapshot directory'
  chmod 0700 "$manifest_snapshot_directory" ||
    fail 'cannot protect manifest snapshot directory'
  manifest_snapshot="$manifest_snapshot_directory/release-manifest.json"
  cp "$manifest" "$manifest_snapshot" || fail 'cannot freeze release manifest snapshot'
  chmod 0600 "$manifest_snapshot" || fail 'cannot protect release manifest snapshot'

  output=$("$production_manager" validate-manifest --manifest "$manifest_snapshot") ||
    fail 'release manifest validation failed'
  [[ "$output" =~ ^version=([0-9]+\.[0-9]+\.[0-9]+)[[:space:]]git_sha=([a-f0-9]{40})[[:space:]]schema=([1-9][0-9]*)[[:space:]]manifest_sha256=([a-f0-9]{64})[[:space:]]validated=true$ ]] ||
    fail 'release manifest validator returned an invalid contract'
  [[ "${BASH_REMATCH[3]}" == "$target_schema" ]] ||
    fail "release manifest must target schema $target_schema"
  release_version=${BASH_REMATCH[1]}
  release_git_sha=${BASH_REMATCH[2]}
  release_manifest_sha256=${BASH_REMATCH[4]}
  candidate_server_image="$server_repository@$(jq --raw-output \
    '.server_image_digest' "$manifest_snapshot")"
}

validate_previous_server_image() {
  [[ "$previous_server_image" =~ ^${server_repository}@sha256:[a-f0-9]{64}$ ]] ||
    fail '--previous-server-image must be an immutable official Server digest'
  [[ "$previous_server_image" != "$candidate_server_image" ]] ||
    fail 'previous and candidate Server images must differ'
}

validate_forward_server_image() {
  [[ -n "$forward_server_image" ]] || return 0
  [[ "$forward_server_image" =~ ^${server_repository}@sha256:[a-f0-9]{64}$ ]] ||
    fail '--forward-server-image must be an immutable official Server digest'
  [[ "$forward_server_image" != "$candidate_server_image" &&
    "$forward_server_image" != "$previous_server_image" ]] ||
    fail 'forward Server image must differ from candidate and previous images'
  validated_forward_server_image=$forward_server_image
}

validate_backup_metadata() {
  local backup_id checksum_line expected_checksum expected_size actual_size mode canonical
  local entry name
  local -a entries

  [[ -d "$backup_directory" && ! -L "$backup_directory" ]] ||
    fail '--backup must be a real directory'
  valid_absolute_path "$backup_directory" || fail '--backup must be a safe absolute path'
  canonical=$(realpath "$backup_directory") || fail 'cannot resolve backup directory'
  [[ "$canonical" == "$backup_directory" ]] || fail '--backup must be a canonical path'
  [[ "$(path_owner "$backup_directory")" == "$EUID" ]] ||
    fail 'backup directory must be owned by the current user'
  mode=$(path_mode "$backup_directory") || fail 'cannot inspect backup directory permissions'
  [[ "$mode" == 700 ]] || fail 'backup directory must have mode 0700'
  backup_id=${backup_directory##*/}
  [[ "$backup_id" =~ ^[0-9]{8}T[0-9]{6}Z-(daily|predeploy)$ ]] ||
    fail 'backup directory name is not a finalized backup ID'

  shopt -s nullglob dotglob
  entries=("$backup_directory"/*)
  shopt -u nullglob dotglob
  [[ ${#entries[@]} == 3 ]] ||
    fail 'backup directory must contain exactly three contract files'
  for entry in "${entries[@]}"; do
    name=${entry##*/}
    case "$name" in
      database.dump | database.dump.sha256 | metadata.json) ;;
      *) fail 'backup directory contains an unexpected entry' ;;
    esac
    require_private_file "backup $name" "$entry"
    [[ "$(path_mode "$entry")" == 600 ]] ||
      fail "backup $name must have mode 0600"
  done

  jq --exit-status \
    --arg backup_id "$backup_id" \
    --arg postgres_image "$postgres_image" \
    --arg source_project "$source_project" \
    --arg source_service "$source_service" \
    --arg source_volume "$source_volume" \
    --argjson source_schema "$source_schema" '
      type == "object" and
      keys == [
        "backup_id", "backup_type", "created_at", "database_name",
        "database_user", "deployment_version", "format_version", "git_sha",
        "postgres_image", "postgres_version", "schema_dirty", "schema_version",
        "sha256", "size_bytes", "source_compose_project",
        "source_compose_service", "source_volume"
      ] and
      .format_version == 1 and
      .backup_id == $backup_id and
      (.backup_type == "daily" or .backup_type == "predeploy") and
      ((.created_at | gsub("[-:]"; "")) + "-" + .backup_type == .backup_id) and
      (.created_at | type == "string" and fromdateiso8601 > 0) and
      (.deployment_version | type == "string" and
        test("^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$")) and
      (.git_sha | type == "string" and test("^[a-f0-9]{40}$")) and
      .source_compose_project == $source_project and
      .source_compose_service == $source_service and
      .source_volume == $source_volume and
      .postgres_image == $postgres_image and
      (.postgres_version | type == "string" and test("^18\\.")) and
      (.database_name | type == "string" and test("^[a-z_][a-z0-9_]{0,62}$")) and
      (.database_user | type == "string" and test("^[a-z_][a-z0-9_]{0,62}$")) and
      .schema_version == $source_schema and .schema_dirty == false and
      (.size_bytes | type == "number" and floor == . and . > 0) and
      (.sha256 | type == "string" and test("^[a-f0-9]{64}$"))
    ' "$backup_directory/metadata.json" >/dev/null ||
    fail 'backup metadata is invalid or is not a clean Production schema 9 backup'

  expected_checksum=$(jq --raw-output '.sha256' "$backup_directory/metadata.json")
  expected_size=$(jq --raw-output '.size_bytes' "$backup_directory/metadata.json")
  checksum_line=$(<"$backup_directory/database.dump.sha256")
  [[ "$checksum_line" == "$expected_checksum  database.dump" ]] ||
    fail 'backup checksum descriptor does not match metadata'
  actual_size=$(file_size "$backup_directory/database.dump") ||
    fail 'cannot inspect backup dump size'
  [[ "$actual_size" == "$expected_size" ]] ||
    fail 'backup dump size does not match metadata'

  selected_backup_id=$backup_id
  selected_backup_sha256=$expected_checksum
  selected_database=$(jq --raw-output '.database_name' "$backup_directory/metadata.json")
  selected_database_user=$(jq --raw-output '.database_user' "$backup_directory/metadata.json")
  backup_deployment_version=$(jq --raw-output \
    '.deployment_version' "$backup_directory/metadata.json")
  backup_git_sha=$(jq --raw-output '.git_sha' "$backup_directory/metadata.json")
}

validate_backup_content() {
  local actual_checksum

  actual_checksum=$(sha256_file "$backup_directory/database.dump") ||
    fail 'cannot calculate backup dump SHA-256'
  [[ "$actual_checksum" == "$selected_backup_sha256" ]] ||
    fail 'backup dump SHA-256 does not match metadata'
  [[ "$(dd if="$backup_directory/database.dump" bs=5 count=1 2>/dev/null)" == PGDMP ]] ||
    fail 'backup dump is not PostgreSQL custom format'
}

image_id_for_reference() {
  local reference=$1 expected_version=$2 expected_revision=$3
  local inspection image_id repo_digest=$1

  if [[ "$reference" == postgres:*@sha256:* ]]; then
    repo_digest="postgres@${reference##*@}"
  fi

  inspection=$(docker image inspect "$reference") ||
    fail 'required immutable image is not available locally'
  jq --exit-status \
    --arg reference "$repo_digest" \
    --arg expected_version "$expected_version" \
    --arg expected_revision "$expected_revision" '
    length == 1 and
    (.[0].Id | type == "string" and test("^sha256:[a-f0-9]{64}$")) and
    ((.[0].RepoDigests // []) | index($reference) != null) and
    ($expected_version == "" or
      .[0].Config.Labels["org.opencontainers.image.version"] == $expected_version) and
    ($expected_revision == "" or
      .[0].Config.Labels["org.opencontainers.image.revision"] == $expected_revision)
  ' <<<"$inspection" >/dev/null ||
    fail 'local image identity does not match the immutable reference'
  image_id=$(jq --raw-output '.[0].Id' <<<"$inspection")
  printf '%s\n' "$image_id"
}

forward_image_identity_for_reference() {
  local reference=$1 inspection image_id version revision

  inspection=$(docker image inspect "$reference") ||
    fail 'required immutable forward image is not available locally'
  jq --exit-status --arg reference "$reference" '
    length == 1 and
    (.[0].Id | type == "string" and test("^sha256:[a-f0-9]{64}$")) and
    ((.[0].RepoDigests // []) | index($reference) != null) and
    (.[0].Config.Labels["org.opencontainers.image.version"] |
      type == "string" and
      test("^[0-9]+\\.[0-9]+\\.[0-9]+([-.+][A-Za-z0-9.-]+)?$")) and
    (.[0].Config.Labels["org.opencontainers.image.revision"] |
      type == "string" and test("^[a-f0-9]{40}$"))
  ' <<<"$inspection" >/dev/null ||
    fail 'forward image identity or OCI labels are invalid'
  image_id=$(jq --raw-output '.[0].Id' <<<"$inspection")
  version=$(jq --raw-output \
    '.[0].Config.Labels["org.opencontainers.image.version"]' <<<"$inspection")
  revision=$(jq --raw-output \
    '.[0].Config.Labels["org.opencontainers.image.revision"]' <<<"$inspection")
  printf '%s\t%s\t%s\n' "$image_id" "$version" "$revision"
}

create_isolated_resources() {
  local suffix network_id created_volume

  suffix=$run_id
  network_name="xe3-speakup-prod-rehearsal-$suffix"
  volume_name="xe3-speakup-prod-rehearsal-$suffix"
  [[ "$network_name" != *production* && "$volume_name" != "$source_volume" ]] ||
    fail 'rehearsal resource identity is unsafe'

  docker_touched=true
  network_id=$(docker network create \
    --internal \
    --label "$resource_label=true" \
    --label "$run_label=$run_id" \
    "$network_name") || fail 'cannot create isolated rehearsal network'
  [[ "$network_id" =~ ^[a-f0-9]{64}$ ]] ||
    fail 'isolated rehearsal network returned an invalid ID'
  owned_network || fail 'isolated rehearsal network identity is invalid'

  created_volume=$(docker volume create \
    --label "$resource_label=true" \
    --label "$run_label=$run_id" \
    "$volume_name") || fail 'cannot create isolated rehearsal volume'
  [[ "$created_volume" == "$volume_name" ]] ||
    fail 'isolated rehearsal volume returned an invalid identity'
  owned_volume || fail 'isolated rehearsal volume identity is invalid'
}

verify_container_isolation() {
  local name=$1 container_id=$2 inspection

  inspection=$(docker container inspect "$container_id") ||
    fail 'cannot inspect rehearsal container isolation'
  jq --exit-status \
    --arg name "$name" \
    --arg id "$container_id" \
    --arg network "$network_name" \
    --argjson memory "$container_memory_bytes" \
    --argjson pids "$container_pids_limit" \
    --arg max_size "$container_log_max_size" \
    --arg max_file "$container_log_max_file" '
    length == 1 and
    .[0].Id == $id and .[0].Name == ("/" + $name) and
    ((.[0].HostConfig.PortBindings // {}) == {}) and
    .[0].HostConfig.NetworkMode == $network and
    (.[0].NetworkSettings.Networks | keys) == [$network] and
    .[0].HostConfig.NanoCpus == 1000000000 and
    .[0].HostConfig.Memory == $memory and
    .[0].HostConfig.PidsLimit == $pids and
    .[0].HostConfig.LogConfig.Type == "local" and
    .[0].HostConfig.LogConfig.Config["max-size"] == $max_size and
    .[0].HostConfig.LogConfig.Config["max-file"] == $max_file
  ' <<<"$inspection" >/dev/null ||
    fail 'rehearsal container violates isolation or resource limits'
}

record_created_container() {
  local name=$1 container_id=$2

  [[ "$container_id" =~ ^[a-f0-9]{64}$ ]] ||
    fail 'Docker returned an invalid rehearsal container ID'
  container_names+=("$name")
  container_ids+=("$container_id")
  last_created_container_id=$container_id
  owned_container "$name" "$container_id" ||
    fail 'created rehearsal container identity is invalid'
  verify_container_isolation "$name" "$container_id"
}

start_postgres() {
  local created_id

  postgres_container="xe3-speakup-prod-rehearsal-db-$run_id"
  created_id=$(docker container create \
    --pull never \
    --name "$postgres_container" \
    --network "$network_name" \
    --cpus "$container_cpus" \
    --memory "$container_memory" \
    --pids-limit "$container_pids_limit" \
    --log-driver local \
    --log-opt "max-size=$container_log_max_size" \
    --log-opt "max-file=$container_log_max_file" \
    --label "$resource_label=true" \
    --label "$run_label=$run_id" \
    --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql,volume-nocopy" \
    --env "POSTGRES_DB=$selected_database" \
    --env "POSTGRES_USER=$selected_database_user" \
    --env POSTGRES_HOST_AUTH_METHOD=trust \
    "$postgres_image_id") ||
    fail 'cannot start isolated rehearsal PostgreSQL'
  record_created_container "$postgres_container" "$created_id"
  postgres_container_id=$created_id
  docker container start "$postgres_container_id" >/dev/null ||
    fail 'cannot start isolated rehearsal PostgreSQL'
}

wait_for_postgres() {
  local attempt state process

  for ((attempt = 1; attempt <= 60; attempt += 1)); do
    process=$(docker container exec "$postgres_container_id" cat /proc/1/comm 2>/dev/null || true)
    if [[ "$process" == postgres ]] && \
      docker container exec --user postgres "$postgres_container_id" \
      pg_isready \
        --username "$selected_database_user" \
        --dbname "$selected_database" >/dev/null 2>&1; then
      return
    fi
    state=$(docker container inspect --format '{{.State.Status}}' \
      "$postgres_container_id" 2>/dev/null || true)
    [[ "$state" != exited && "$state" != dead ]] ||
      fail 'isolated rehearsal PostgreSQL stopped before readiness'
    sleep 1
  done
  fail 'isolated rehearsal PostgreSQL did not become ready'
}

database_query() {
  local sql=$1

  docker container exec --user postgres "$postgres_container_id" \
    psql \
      --no-psqlrc \
      --set ON_ERROR_STOP=1 \
      --tuples-only \
      --no-align \
      --quiet \
      --username "$selected_database_user" \
      --dbname "$selected_database" \
      --command "$sql"
}

schema_state() {
  local state

  state=$(database_query \
    "SELECT version::text || '|' || dirty::text FROM public.schema_migrations;") ||
    fail 'cannot read isolated migration state'
  [[ "$state" =~ ^([1-9][0-9]*)\|false$ ]] ||
    fail 'isolated migration state is not one clean positive version'
  printf '%s\n' "${BASH_REMATCH[1]}"
}

restore_backup() {
  local started

  phase='restore'
  started=$(now_seconds)
  docker container exec --interactive --user postgres "$postgres_container_id" \
    pg_restore \
      --single-transaction \
      --exit-on-error \
      --no-owner \
      --no-privileges \
      --username "$selected_database_user" \
      --dbname "$selected_database" <"$backup_directory/database.dump" ||
    fail 'isolated PostgreSQL restore failed'
  [[ "$(schema_state)" == "$source_schema" ]] ||
    fail "restored backup is not clean schema $source_schema"
  database_query \
    "ALTER DATABASE $selected_database SET lock_timeout = '${lock_timeout_seconds}s';" \
    >/dev/null || fail 'cannot set finite migration lock timeout'
  restore_duration_seconds=$(( $(now_seconds) - started ))
}

run_migration() {
  local name=$1 image_id=$2 created_id

  created_id=$(docker container create \
    --name "$name" \
    --pull never \
    --network "$network_name" \
    --cpus "$container_cpus" \
    --memory "$container_memory" \
    --pids-limit "$container_pids_limit" \
    --log-driver local \
    --log-opt "max-size=$container_log_max_size" \
    --log-opt "max-file=$container_log_max_file" \
    --label "$resource_label=true" \
    --label "$run_label=$run_id" \
    --env "DATABASE_URL=postgres://$selected_database_user@$postgres_container:5432/$selected_database?sslmode=disable" \
    --entrypoint /usr/local/bin/speakup-migrate \
    "$image_id" up) ||
    fail 'cannot create isolated candidate migration container'
  record_created_container "$name" "$created_id"
  docker container start --attach "$created_id" >/dev/null ||
    fail 'candidate migration failed within the finite lock timeout'
}

verify_target_database() {
  local barrier_count constraint_count expected_views actual_views
  local expected_indexes actual_indexes public_grants object_count column_count
  local expected_constraints actual_constraints lisa_binding_count voice_count
  local expected_voice_contract voice_contract

  [[ "$(schema_state)" == "$target_schema" ]] ||
    fail "candidate migration did not produce clean schema $target_schema"
  schema_verified=true
  expected_views=$'product_health_daily_artifact_coverage\nproduct_health_daily_evaluation_health\nproduct_health_daily_practice_activity\nproduct_health_daily_scoreability\nproduct_health_daily_session_outcomes'
  actual_views=$(database_query \
    "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'v' AND c.relname LIKE 'product_health_daily_%' ORDER BY c.relname;") ||
    fail 'cannot inspect product health views'
  [[ "$actual_views" == "$expected_views" ]] ||
    fail 'schema 15 does not contain exactly the five product health views'
  barrier_count=$(database_query \
    "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'v' AND c.relname LIKE 'product_health_daily_%' AND c.reloptions @> ARRAY['security_barrier=true'];") ||
    fail 'cannot inspect product health view security barriers'
  [[ "$barrier_count" == 5 ]] ||
    fail 'product health views are missing security barriers'
  views_verified=true

  expected_views=$'user_behavior_current_nonterminal_sessions\nuser_behavior_daily_early_end\nuser_behavior_daily_feature_usage\nuser_behavior_daily_repractice\nuser_behavior_daily_retention\nuser_behavior_daily_session_funnel\nuser_behavior_daily_time_to_first_effective_turn'
  actual_views=$(database_query \
    "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'v' AND c.relname LIKE 'user_behavior_%' ORDER BY c.relname;") ||
    fail 'cannot inspect user behavior views'
  [[ "$actual_views" == "$expected_views" ]] ||
    fail 'schema 15 does not contain exactly the seven user behavior views'
  # The rehearsal pins PostgreSQL 18, so its normalized catalog definitions are
  # stable fingerprints for the table, expression, column order and predicate.
  expected_indexes=$'user_behavior_confirmed_turns_day_idx|practice_turns|true|true|true|false|false|fb14f0584e55b8235b27df1d15cf882c|301680844dfdda6cb713fd00399e02f7\nuser_behavior_nonterminal_sessions_updated_idx|practice_sessions|true|true|true|false|false|abdea30dc6f0b8ab1bacc26725251cfa|2ed9267ac3e11681d5050bf454787b63\nuser_behavior_ready_session_reports_idx|evaluations|true|true|true|false|false|5bb3c9b107d1e1c16179cac640b8f45c|ff20ea3c635e1dec160eed2b0f5803b4\nuser_behavior_sessions_created_day_idx|practice_sessions|true|true|true|false|false|acdceda1390123c40b36a123f110b2b3|d41d8cd98f00b204e9800998ecf8427e'
  actual_indexes=$(database_query \
    "SELECT index_relation.relname || '|' || table_relation.relname || '|' || index_catalog.indisvalid::text || '|' || index_catalog.indisready::text || '|' || index_catalog.indislive::text || '|' || index_catalog.indisunique::text || '|' || index_catalog.indisprimary::text || '|' || md5(pg_catalog.pg_get_indexdef(index_catalog.indexrelid)) || '|' || md5(COALESCE(pg_catalog.pg_get_expr(index_catalog.indpred, index_catalog.indrelid), '')) FROM pg_catalog.pg_index index_catalog JOIN pg_catalog.pg_class index_relation ON index_relation.oid = index_catalog.indexrelid JOIN pg_catalog.pg_namespace index_namespace ON index_namespace.oid = index_relation.relnamespace JOIN pg_catalog.pg_class table_relation ON table_relation.oid = index_catalog.indrelid JOIN pg_catalog.pg_namespace table_namespace ON table_namespace.oid = table_relation.relnamespace WHERE index_namespace.nspname = 'public' AND table_namespace.nspname = 'public' AND index_relation.relname LIKE 'user_behavior_%' ORDER BY index_relation.relname;") ||
    fail 'cannot inspect user behavior index contracts'
  [[ "$actual_indexes" == "$expected_indexes" ]] ||
    fail 'schema 15 user behavior index contracts are invalid'
  barrier_count=$(database_query \
    "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'v' AND c.relname LIKE 'user_behavior_%' AND c.reloptions @> ARRAY['security_barrier=true'];") ||
    fail 'cannot inspect user behavior view security barriers'
  [[ "$barrier_count" == 7 ]] ||
    fail 'user behavior views are missing security barriers'
  user_behavior_views_verified=true

  public_grants=$(database_query \
    "SELECT count(*) FROM (SELECT 1 FROM pg_catalog.pg_class relation JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(relation.relacl, pg_catalog.acldefault('r', relation.relowner))) public_acl WHERE namespace.nspname = 'public' AND relation.relkind = 'v' AND relation.relname IN ('product_health_daily_artifact_coverage', 'product_health_daily_evaluation_health', 'product_health_daily_practice_activity', 'product_health_daily_scoreability', 'product_health_daily_session_outcomes', 'user_behavior_current_nonterminal_sessions', 'user_behavior_daily_early_end', 'user_behavior_daily_feature_usage', 'user_behavior_daily_repractice', 'user_behavior_daily_retention', 'user_behavior_daily_session_funnel', 'user_behavior_daily_time_to_first_effective_turn') AND public_acl.grantee = 0 UNION ALL SELECT 1 FROM pg_catalog.pg_class relation JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace JOIN pg_catalog.pg_attribute attribute ON attribute.attrelid = relation.oid AND attribute.attnum > 0 AND NOT attribute.attisdropped CROSS JOIN LATERAL pg_catalog.aclexplode(attribute.attacl) public_acl WHERE namespace.nspname = 'public' AND relation.relkind = 'v' AND relation.relname IN ('product_health_daily_artifact_coverage', 'product_health_daily_evaluation_health', 'product_health_daily_practice_activity', 'product_health_daily_scoreability', 'product_health_daily_session_outcomes', 'user_behavior_current_nonterminal_sessions', 'user_behavior_daily_early_end', 'user_behavior_daily_feature_usage', 'user_behavior_daily_repractice', 'user_behavior_daily_retention', 'user_behavior_daily_session_funnel', 'user_behavior_daily_time_to_first_effective_turn') AND public_acl.grantee = 0) public_privileges;") ||
    fail 'cannot inspect aggregate view PUBLIC privileges'
  [[ "$public_grants" == 0 ]] ||
    fail 'aggregate views expose privileges to PUBLIC'
  view_privileges_verified=true

  object_count=$(database_query \
    "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname IN ('coach_avatar_options', 'coach_voice_options', 'user_coach_presentation_preferences');") ||
    fail 'cannot inspect schema 10 presentation tables'
  [[ "$object_count" == 3 ]] ||
    fail 'schema 10 presentation tables are incomplete'
  column_count=$(database_query \
    "SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND is_nullable = 'NO' AND (table_name, column_name) IN (('coach_avatar_options', 'provider_profile'), ('coach_voice_options', 'provider_profile'), ('practice_sessions', 'presentation_snapshot'));") ||
    fail 'cannot inspect schema 11 runtime columns'
  [[ "$column_count" == 3 ]] ||
    fail 'schema 11 presentation runtime columns are incomplete'
  expected_constraints=$'agent_runs|agent_runs_json_shape_check|c|true|true|f36065a83bac4c34f381540b741436c7|context_snapshot,tool_trace,model_result,usage,error\nagent_runs|agent_runs_model_configuration_check|c|true|true|16e1e11905311b9291cd6b1bcd3d826c|model_configuration\ncoach_avatar_options|coach_avatar_options_provider_binding_unique|u|true|true|5a972ed789cab55701518859c17b8314|provider,provider_profile,provider_avatar_id,binding_version\ncoach_avatar_options|coach_avatar_options_text_check|c|true|true|cacaac8ec461b7e191450e21842629a0|display_name,description,preview_asset_key,provider,provider_profile,provider_avatar_id\ncoach_voice_options|coach_voice_options_provider_binding_unique|u|true|true|7d0360aaaa9eea1997007a99e59ec519|provider,provider_profile,provider_model,provider_voice_id,binding_version\ncoach_voice_options|coach_voice_options_text_check|c|true|true|ea842603d9d2550a2b262fa6458666b6|display_name,description,locale,gender,provider,provider_profile,provider_model,provider_voice_id\npractice_sessions|practice_sessions_presentation_snapshot_check|c|true|true|75db06017f1900ed528d74da92161732|presentation_snapshot'
  actual_constraints=$(database_query \
    "SELECT relation.relname || '|' || constraint_catalog.conname || '|' || constraint_catalog.contype::text || '|' || constraint_catalog.convalidated::text || '|' || constraint_catalog.conenforced::text || '|' || md5(pg_catalog.pg_get_constraintdef(constraint_catalog.oid, false)) || '|' || COALESCE((SELECT string_agg(attribute.attname, ',' ORDER BY key_column.key_position) FROM unnest(constraint_catalog.conkey) WITH ORDINALITY AS key_column(attnum, key_position) JOIN pg_catalog.pg_attribute attribute ON attribute.attrelid = constraint_catalog.conrelid AND attribute.attnum = key_column.attnum), '') FROM pg_catalog.pg_constraint constraint_catalog JOIN pg_catalog.pg_class relation ON relation.oid = constraint_catalog.conrelid JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace WHERE namespace.nspname = 'public' AND constraint_catalog.conname IN ('coach_avatar_options_text_check', 'coach_avatar_options_provider_binding_unique', 'coach_voice_options_text_check', 'coach_voice_options_provider_binding_unique', 'practice_sessions_presentation_snapshot_check', 'agent_runs_model_configuration_check', 'agent_runs_json_shape_check') ORDER BY relation.relname, constraint_catalog.conname;") ||
    fail 'cannot inspect schema 10 to 12 constraint contracts'
  [[ "$actual_constraints" == "$expected_constraints" ]] ||
    fail 'schema 10 to 12 constraint contracts are invalid'
  lisa_binding_count=$(database_query \
    "SELECT count(*) FROM public.coach_avatar_options WHERE id = 'avatar_lisa' AND provider = 'spatialreal' AND provider_profile = 'spatialreal_default' AND provider_avatar_id = 'ca9c5c22-6dba-4b59-ae3b-d26066f8c017' AND binding_version = 2;") ||
    fail 'cannot inspect schema 13 Lisa binding'
  [[ "$lisa_binding_count" == 1 ]] ||
    fail 'schema 13 Lisa binding is invalid'
  voice_count=$(database_query \
    "SELECT count(*) FROM public.coach_voice_options;") ||
    fail 'cannot inspect schema 14 voice catalog'
  [[ "$voice_count" == 9 ]] ||
    fail 'schema 14 voice catalog size is invalid'
  expected_voice_contract=$'voice_adrian|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loongadriangao|1|true\nvoice_ivy|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loongivyhu|1|true\nvoice_james|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loongjameszhao|1|true\nvoice_luna|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loonglunawang|1|true\nvoice_mary|en-GB|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|loongmary|1|true\nvoice_nora|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loongnorahu|1|true\nvoice_olivia|en|qianwen|qianwen_default|qwen-audio-3.0-tts-flash|qwen-audio-3.0-tts-flash-loongolivialin|1|true'
  voice_contract=$(database_query \
    "SELECT id || '|' || locale || '|' || provider || '|' || provider_profile || '|' || provider_model || '|' || provider_voice_id || '|' || binding_version::text || '|' || enabled::text FROM public.coach_voice_options WHERE id IN ('voice_adrian', 'voice_ivy', 'voice_james', 'voice_luna', 'voice_mary', 'voice_nora', 'voice_olivia') ORDER BY id;") ||
    fail 'cannot inspect schema 14 voice bindings'
  [[ "$voice_contract" == "$expected_voice_contract" ]] ||
    fail 'schema 14 voice catalog is invalid'
  schema_10_14_contracts_verified=true

  constraint_count=$(database_query \
    "SELECT count(*) FROM pg_catalog.pg_constraint c WHERE c.conrelid = 'public.evaluations'::regclass AND c.conname = 'evaluations_kind_check' AND c.contype = 'c' AND c.convalidated = true AND pg_get_expr(c.conbin, c.conrelid) ~ '^\\(kind = ANY \\(ARRAY\\[' AND (SELECT array_agg((match)[1] ORDER BY (match)[1]) FROM regexp_matches(pg_get_constraintdef(c.oid), '''([^'']+)''', 'g') AS match) = ARRAY['AGENT_MESSAGE_FEEDBACK', 'IELTS_PART1_PROFILE', 'IELTS_PART2_PROFILE', 'PRACTICE_TURN_FEEDBACK', 'SESSION_REPORT']::text[];") ||
    fail 'cannot inspect IELTS evaluation constraint'
  [[ "$constraint_count" == 1 ]] ||
    fail 'schema 15 IELTS evaluation kind constraint is invalid'
  constraint_verified=true
}

start_server_and_wait() {
  local name=$1 image_id=$2 attempt state created_id

  created_id=$(docker container create \
    --pull never \
    --name "$name" \
    --network "$network_name" \
    --cpus "$container_cpus" \
    --memory "$container_memory" \
    --pids-limit "$container_pids_limit" \
    --log-driver local \
    --log-opt "max-size=$container_log_max_size" \
    --log-opt "max-file=$container_log_max_file" \
    --label "$resource_label=true" \
    --label "$run_label=$run_id" \
    --env-file "$server_environment" \
    --env "DATABASE_URL=postgres://$selected_database_user@$postgres_container:5432/$selected_database?sslmode=disable" \
    --env SERVER_HOST=0.0.0.0 \
    --env SERVER_PORT=8080 \
    --env METRICS_HOST=127.0.0.1 \
    "$image_id") || fail 'cannot create isolated Server image'
  record_created_container "$name" "$created_id"
  docker container start "$created_id" >/dev/null ||
    fail 'cannot start isolated Server image'
  for ((attempt = 1; attempt <= 60; attempt += 1)); do
    if docker container exec "$created_id" \
      wget -q -T 2 --spider http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
      return
    fi
    state=$(docker container inspect --format '{{.State.Status}}' \
      "$created_id" 2>/dev/null || true)
    [[ "$state" != exited && "$state" != dead ]] ||
      fail 'isolated Server image stopped before readiness'
    sleep 1
  done
  fail 'isolated Server image did not pass readiness'
}

stop_server() {
  docker container stop --time 5 "$1" >/dev/null ||
    fail 'cannot stop isolated Server image after readiness'
}

verify_rollback_guard() {
  local output

  if output=$("$production_manager" validate-rollback-schema \
    --current-schema "$target_schema" --target-schema "$source_schema" 2>&1); then
    fail 'Production rollback guard allowed schema 15 to schema 9'
  fi
  [[ "$output" == *'rollback requires the current database schema to match the target release'* ]] ||
    fail 'Production rollback guard failed for an unexpected reason'
  rollback_guard_verified=true
}

run_isolated_rehearsal() {
  local migration_started candidate_server previous_server
  local redeploy_migration redeploy_server forward_migration forward_server
  local forward_identity forward_server_image_id

  phase='image_identity'
  postgres_image_id=$(image_id_for_reference "$postgres_image" '' '')
  candidate_server_image_id=$(image_id_for_reference \
    "$candidate_server_image" "$release_version" "$release_git_sha")
  previous_server_image_id=$(image_id_for_reference \
    "$previous_server_image" "$backup_deployment_version" "$backup_git_sha")
  if [[ -n "$validated_forward_server_image" ]]; then
    forward_identity=$(forward_image_identity_for_reference \
      "$validated_forward_server_image")
    IFS=$'\t' read -r forward_server_image_id \
      forward_server_image_version forward_server_image_revision \
      <<<"$forward_identity"
    [[ "$forward_server_image_id" =~ ^sha256:[a-f0-9]{64}$ &&
      -n "$forward_server_image_version" &&
      "$forward_server_image_revision" =~ ^[a-f0-9]{40}$ ]] ||
      fail 'forward image identity fields are invalid'
  fi
  phase='resource_creation'
  create_isolated_resources
  start_postgres
  wait_for_postgres
  restore_backup

  phase='migration'
  migration_started=$(now_seconds)
  run_migration "xe3-speakup-prod-rehearsal-migrate-$run_id" \
    "$candidate_server_image_id"
  migration_duration_seconds=$(( $(now_seconds) - migration_started ))
  phase='schema_verification'
  verify_target_database

  phase='candidate_readiness'
  candidate_server="xe3-speakup-prod-rehearsal-candidate-$run_id"
  start_server_and_wait "$candidate_server" "$candidate_server_image_id"
  candidate_readiness_verified=true
  stop_server "$last_created_container_id"

  phase='previous_image_boundary'
  previous_server="xe3-speakup-prod-rehearsal-previous-$run_id"
  start_server_and_wait "$previous_server" "$previous_server_image_id"
  previous_readiness_verified=true
  stop_server "$last_created_container_id"

  phase='rollback_guard'
  verify_rollback_guard

  if [[ -n "$validated_forward_server_image" ]]; then
    phase='forward_hotfix'
    forward_migration="xe3-speakup-prod-rehearsal-forward-migrate-$run_id"
    run_migration "$forward_migration" "$forward_server_image_id"
    verify_target_database
    idempotent_migration_verified=true
    forward_server="xe3-speakup-prod-rehearsal-forward-$run_id"
    start_server_and_wait "$forward_server" "$forward_server_image_id"
    stop_server "$last_created_container_id"
    forward_hotfix_status='verified'
  else
    phase='same_schema_candidate_redeploy'
    redeploy_migration="xe3-speakup-prod-rehearsal-redeploy-migrate-$run_id"
    run_migration "$redeploy_migration" "$candidate_server_image_id"
    [[ "$(schema_state)" == "$target_schema" ]] ||
      fail 'same-schema candidate redeploy changed the clean target schema'
    idempotent_migration_verified=true
    redeploy_server="xe3-speakup-prod-rehearsal-redeploy-$run_id"
    start_server_and_wait "$redeploy_server" "$candidate_server_image_id"
    stop_server "$last_created_container_id"
    same_schema_candidate_redeploy_verified=true
  fi
  phase='complete'
}

owned_container() {
  local name=$1 container_id=$2 inspection

  inspection=$(docker container inspect "$container_id" 2>/dev/null) || return 1
  jq --exit-status \
    --arg name "$name" --arg container_id "$container_id" \
    --arg resource_label "$resource_label" \
    --arg run_label "$run_label" --arg run_id "$run_id" '
      length == 1 and .[0].Id == $container_id and
      .[0].Name == ("/" + $name) and
      .[0].Config.Labels[$resource_label] == "true" and
      .[0].Config.Labels[$run_label] == $run_id
    ' <<<"$inspection" >/dev/null
}

owned_volume() {
  local inspection

  inspection=$(docker volume inspect "$volume_name" 2>/dev/null) || return 1
  jq --exit-status \
    --arg name "$volume_name" --arg resource_label "$resource_label" \
    --arg run_label "$run_label" --arg run_id "$run_id" '
      length == 1 and .[0].Name == $name and
      .[0].Labels[$resource_label] == "true" and
      .[0].Labels[$run_label] == $run_id
    ' <<<"$inspection" >/dev/null
}

owned_network() {
  local inspection

  inspection=$(docker network inspect "$network_name" 2>/dev/null) || return 1
  jq --exit-status \
    --arg name "$network_name" --arg resource_label "$resource_label" \
    --arg run_label "$run_label" --arg run_id "$run_id" '
      length == 1 and .[0].Name == $name and .[0].Internal == true and
      .[0].Labels[$resource_label] == "true" and
      .[0].Labels[$run_label] == $run_id
    ' <<<"$inspection" >/dev/null
}

container_absence_proven() {
  local expected_name=$1 expected_id=$2 listing listed_id listed_name

  listing=$(docker container ls --all --no-trunc \
    --format '{{.ID}}\t{{.Names}}') || return 1
  while IFS=$'\t' read -r listed_id listed_name; do
    [[ -n "$listed_id" ]] || continue
    if [[ "$listed_id" == "$expected_id" || "$listed_name" == "$expected_name" ]]; then
      return 1
    fi
  done <<<"$listing"
  return 0
}

network_absence_proven() {
  local expected_name=$1 listing listed_id listed_name

  listing=$(docker network ls --no-trunc --format '{{.ID}}\t{{.Name}}') || return 1
  while IFS=$'\t' read -r listed_id listed_name; do
    [[ -n "$listed_id" ]] || continue
    [[ "$listed_name" != "$expected_name" ]] || return 1
  done <<<"$listing"
  return 0
}

volume_absence_proven() {
  local expected_name=$1 listing listed_name

  listing=$(docker volume ls --format '{{.Name}}') || return 1
  while IFS= read -r listed_name; do
    [[ -n "$listed_name" ]] || continue
    [[ "$listed_name" != "$expected_name" ]] || return 1
  done <<<"$listing"
  return 0
}

cleanup_owned_resources() {
  local failed=0 name container_id index

  for ((index = ${#container_names[@]} - 1; index >= 0; index -= 1)); do
    name=${container_names[index]}
    container_id=${container_ids[index]}
    if docker container inspect "$container_id" >/dev/null 2>&1; then
      if owned_container "$name" "$container_id"; then
        docker container rm --force "$container_id" >/dev/null 2>&1 || failed=1
      else
        failed=1
      fi
    elif ! container_absence_proven "$name" "$container_id"; then
      # Inspect errors and same-name replacements fail closed. Never delete them.
      failed=1
    fi
  done
  if [[ -n "$network_name" ]]; then
    if docker network inspect "$network_name" >/dev/null 2>&1; then
      if owned_network; then
        docker network rm "$network_name" >/dev/null 2>&1 || failed=1
      else
        failed=1
      fi
    elif ! network_absence_proven "$network_name"; then
      failed=1
    fi
  fi
  if [[ -n "$volume_name" ]]; then
    if docker volume inspect "$volume_name" >/dev/null 2>&1; then
      if [[ "$volume_name" != "$source_volume" ]] && owned_volume; then
        docker volume rm "$volume_name" >/dev/null 2>&1 || failed=1
      else
        failed=1
      fi
    elif ! volume_absence_proven "$volume_name"; then
      failed=1
    fi
  fi
  ((failed == 0))
}

parse_arguments() {
  while (($# > 0)); do
    case "$1" in
      --execute)
        execute=true
        shift
        ;;
      --backup | --manifest | --previous-server-image | --forward-server-image | --server-env-file | --receipt | --lock-timeout-seconds)
        (($# >= 2)) || fail "$1 requires a value"
        case "$1" in
          --backup) backup_directory=$2 ;;
          --manifest) manifest=$2 ;;
          --previous-server-image) previous_server_image=$2 ;;
          --forward-server-image) forward_server_image=$2 ;;
          --server-env-file) server_environment=$2 ;;
          --receipt) receipt=$2 ;;
          --lock-timeout-seconds) lock_timeout_seconds=$2 ;;
        esac
        shift 2
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *) fail "unknown argument: $1" ;;
    esac
  done
}

validate_inputs() {
  local name

  for name in backup_directory manifest previous_server_image server_environment receipt lock_timeout_seconds; do
    [[ -n "${!name}" ]] || fail "required argument is missing: $name"
  done
  [[ "$lock_timeout_seconds" =~ ^[1-9][0-9]*$ ]] &&
    ((lock_timeout_seconds <= 60)) ||
    fail '--lock-timeout-seconds must be an integer from 1 to 60'
  require_command jq
  require_command stat
  require_command awk
  require_receipt_target
  if [[ "$execute" == true ]]; then
    receipt_ready=true
  fi
  if [[ -n "$forward_server_image" ]]; then
    forward_hotfix_status='not_verified'
  fi
  require_command realpath
  require_command mktemp
  require_command cp
  require_regular_file 'Production manager' "$production_manager"
  phase='environment_validation'
  require_private_file 'server environment file' "$server_environment"
  phase='manifest_validation'
  validate_manifest
  validate_previous_server_image
  validate_forward_server_image
  phase='backup_metadata_validation'
  validate_backup_metadata
}

main() {
  parse_arguments "$@"
  run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$-$RANDOM"
  started_at_seconds=$(now_seconds)
  trap cleanup_on_exit EXIT
  trap 'exit 130' INT HUP TERM
  validate_inputs
  if [[ "$execute" == false ]]; then
    printf 'backup_id=%s version=%s source_schema=%s target_schema=%s forward_hotfix=%s dry_run=true docker_touched=false\n' \
      "$selected_backup_id" "$release_version" "$source_schema" "$target_schema" \
      "$forward_hotfix_status"
    return
  fi

  phase='backup_content_validation'
  validate_backup_content
  require_command docker
  require_command date
  require_command sleep
  require_command dd
  run_isolated_rehearsal
}

main "$@"
