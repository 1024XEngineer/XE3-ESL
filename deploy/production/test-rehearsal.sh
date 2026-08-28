#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly rehearsal="$production_directory/rehearse-schema-upgrade.sh"
readonly postgres_image='postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
readonly candidate_digest="sha256:$(printf 'b%.0s' {1..64})"
readonly previous_image="ghcr.io/1024xengineer/xe3-esl-server@sha256:$(printf 'a%.0s' {1..64})"
readonly forward_image="ghcr.io/1024xengineer/xe3-esl-server@sha256:$(printf 'e%.0s' {1..64})"
readonly secret_value='rehearsal-test-secret-must-not-leak'

fail() {
  printf 'Production schema rehearsal test: %s\n' "$*" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  else
    shasum -a 256 -- "$1" | awk '{print $1}'
  fi
}

file_mode() {
  if stat -c '%a' -- "$1" >/dev/null 2>&1; then
    stat -c '%a' -- "$1"
  else
    stat -f '%Lp' "$1"
  fi
}

expect_failure() {
  local name=$1
  shift
  if "$@" >"$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
  ! grep -Fq "$secret_value" "$temporary_directory/failure.out" ||
    fail "$name exposed the server environment secret"
}

verify_failed_receipt() {
  local selected_receipt=$1 expected_step=$2

  [[ -f "$selected_receipt" && ! -L "$selected_receipt" ]] ||
    fail "$expected_step did not write a failure receipt"
  [[ "$(file_mode "$selected_receipt")" == 600 ]] ||
    fail "$expected_step receipt is not mode 0600"
  jq --exit-status --arg expected_step "$expected_step" '
    .receipt_version == 1 and
    .operation == "production_schema_upgrade_rehearsal" and
    .environment == "isolated" and .status == "failed" and
    .failed_step == $expected_step and
    .server_readiness_profile == "core_database_external_integrations_disabled" and
    .production_provider_readiness == "not_verified" and
    .checks.owned_resource_cleanup == true
  ' "$selected_receipt" >/dev/null ||
    fail "$expected_step receipt is incomplete"
  ! grep -Fq "$secret_value" "$selected_receipt" ||
    fail "$expected_step receipt exposed a server secret"
}

assert_no_rehearsal_resources() {
  local selected_run=$1

  [[ -z "$(docker container ls --all --quiet \
    --filter "label=com.xengineer.speakup.production-rehearsal-run-id=$selected_run")" ]] ||
    fail "$selected_run leaked a container"
  ! docker network inspect "xe3-speakup-prod-rehearsal-$selected_run" \
    >/dev/null 2>&1 || fail "$selected_run leaked its network"
  ! docker volume inspect "xe3-speakup-prod-rehearsal-$selected_run" \
    >/dev/null 2>&1 || fail "$selected_run leaked its volume"
  [[ -z "$(find "$temporary_directory" -maxdepth 1 -type d \
    -name 'xe3-production-rehearsal-inputs.*' -print -quit)" ]] ||
    fail "$selected_run leaked a private input snapshot"
}

selected_background_container=''
selected_background_run_id=''

run_id_belongs_to_pid() {
  local selected_run=$1 selected_pid=$2

  [[ "$selected_pid" =~ ^[1-9][0-9]*$ &&
    "$selected_run" =~ ^[0-9]{8}T[0-9]{6}Z-${selected_pid}-[0-9]+$ ]]
}

verify_pid_rehearsal_container() {
  local selected_pid=$1 role=$2 selected_name=$3 selected_run=$4
  local inspection expected_name

  run_id_belongs_to_pid "$selected_run" "$selected_pid" || return 1
  expected_name="xe3-speakup-prod-rehearsal-$role-$selected_run"
  [[ "$selected_name" == "$expected_name" ]] || return 1
  inspection=$(docker container inspect "$selected_name") || return 1
  jq --exit-status \
    --arg name "$selected_name" \
    --arg run "$selected_run" '
      length == 1 and .[0].Name == ("/" + $name) and
      .[0].Config.Labels["com.xengineer.speakup.production-rehearsal"] == "true" and
      .[0].Config.Labels["com.xengineer.speakup.production-rehearsal-run-id"] == $run
    ' <<<"$inspection" >/dev/null
}

wait_for_pid_rehearsal_container() {
  local selected_pid=$1 role=$2 attempt name selected_run matches

  selected_background_container=''
  selected_background_run_id=''
  for ((attempt = 1; attempt <= 300; attempt += 1)); do
    matches=0
    while IFS=$'\t' read -r name selected_run; do
      [[ -n "$name" && -n "$selected_run" ]] || continue
      if run_id_belongs_to_pid "$selected_run" "$selected_pid" &&
        [[ "$name" == "xe3-speakup-prod-rehearsal-$role-$selected_run" ]]; then
        selected_background_container=$name
        selected_background_run_id=$selected_run
        matches=$((matches + 1))
      fi
    done < <(docker container ls --all \
      --filter 'label=com.xengineer.speakup.production-rehearsal=true' \
      --format '{{.Names}}\t{{.Label "com.xengineer.speakup.production-rehearsal-run-id"}}')
    ((matches <= 1)) || return 1
    if ((matches == 1)); then
      verify_pid_rehearsal_container "$selected_pid" "$role" \
        "$selected_background_container" "$selected_background_run_id"
      return
    fi
    sleep 0.1
  done
  return 1
}

wait_for_postgres() {
  local container=$1 attempt process

  for ((attempt = 1; attempt <= 60; attempt += 1)); do
    process=$(docker container exec "$container" cat /proc/1/comm 2>/dev/null || true)
    if [[ "$process" == postgres ]] && docker container exec "$container" pg_isready \
      --username speakup --dbname speakup >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  fail "$container did not become ready"
}

remove_test_container() {
  local container=$1

  if docker container inspect "$container" >/dev/null 2>&1; then
    docker container inspect "$container" | jq --exit-status \
      --arg container "$container" --arg test_id "$test_id" '
        length == 1 and .[0].Name == ("/" + $container) and
        .[0].Config.Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker container rm --force "$container" >/dev/null
  fi
}

remove_test_volume() {
  local volume=$1

  if docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume inspect "$volume" | jq --exit-status \
      --arg volume "$volume" --arg test_id "$test_id" '
        length == 1 and .[0].Name == $volume and
        .[0].Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker volume rm "$volume" >/dev/null
  fi
}

remove_test_image() {
  local image_id

  for image_id in \
    "${forward_fixture_image_id:-}" \
    "${invalid_voice_fixture_image_id:-}" \
    "${invalid_schema_contract_fixture_image_id:-}" \
    "${invalid_index_fixture_image_id:-}" \
    "${public_user_grant_fixture_image_id:-}" \
    "${migration_failure_fixture_image_id:-}" \
    "${public_grant_fixture_image_id:-}" \
    "${missing_barrier_fixture_image_id:-}" \
    "${negative_constraint_fixture_image_id:-}" \
    "${interrupt_fixture_image_id:-}" \
    "${invalid_constraint_fixture_image_id:-}" \
    "${missing_constraint_fixture_image_id:-}" \
    "${missing_view_fixture_image_id:-}" \
    "${previous_fixture_image_id:-}" \
    "${fixture_image_id:-}"; do
    [[ -n "$image_id" ]] || continue
    docker image inspect "$image_id" >/dev/null 2>&1 || continue
    docker image inspect "$image_id" | jq --exit-status \
      --arg test_id "$test_id" '
        length == 1 and
        .[0].Config.Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker image rm --force "$image_id" >/dev/null
  done
}

remove_rehearsal_run() {
  local selected_run=$1 resource name

  [[ -n "$selected_run" ]] || return 0
  while IFS= read -r resource; do
    [[ -n "$resource" ]] || continue
    docker container inspect "$resource" | jq --exit-status \
      --arg run "$selected_run" '
        length == 1 and
        .[0].Config.Labels["com.xengineer.speakup.production-rehearsal"] == "true" and
        .[0].Config.Labels["com.xengineer.speakup.production-rehearsal-run-id"] == $run
      ' >/dev/null || return 1
    docker container rm --force "$resource" >/dev/null
  done < <(docker container ls --all --quiet \
    --filter 'label=com.xengineer.speakup.production-rehearsal=true' \
    --filter "label=com.xengineer.speakup.production-rehearsal-run-id=$selected_run")
  name="xe3-speakup-prod-rehearsal-$selected_run"
  if docker network inspect "$name" >/dev/null 2>&1; then
    docker network inspect "$name" | jq --exit-status --arg run "$selected_run" '
      length == 1 and .[0].Internal == true and
      .[0].Labels["com.xengineer.speakup.production-rehearsal"] == "true" and
      .[0].Labels["com.xengineer.speakup.production-rehearsal-run-id"] == $run
    ' >/dev/null || return 1
    docker network rm "$name" >/dev/null
  fi
  if docker volume inspect "$name" >/dev/null 2>&1; then
    docker volume inspect "$name" | jq --exit-status --arg run "$selected_run" '
      length == 1 and
      .[0].Labels["com.xengineer.speakup.production-rehearsal"] == "true" and
      .[0].Labels["com.xengineer.speakup.production-rehearsal-run-id"] == $run
    ' >/dev/null || return 1
    docker volume rm "$name" >/dev/null
  fi
}

write_manifest() {
  cat >"$manifest" <<EOF
{
  "manifest_version": 1,
  "version": "0.1.6",
  "version_code": 7,
  "git_sha": "cccccccccccccccccccccccccccccccccccccccc",
  "portal_image": "ghcr.io/1024xengineer/xe3-esl-portal",
  "portal_image_digest": "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
  "server_image": "ghcr.io/1024xengineer/xe3-esl-server",
  "server_image_digest": "$candidate_digest",
  "staging_apk_file": "speakup-v0.1.6-staging-arm64.apk",
  "staging_apk_sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
  "production_apk_file": "speakup-v0.1.6-production-arm64.apk",
  "production_apk_size_bytes": 123,
  "production_apk_sha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
  "application_id": "com.xengineer.speakup",
  "minimum_android_api": 24,
  "abis": ["arm64-v8a"],
  "apk_certificate_sha256": "1111111111111111111111111111111111111111111111111111111111111111",
  "database_schema_version": 15,
  "quality_run_url": "https://github.com/1024XEngineer/XE3-ESL/actions/runs/123456"
}
EOF
}

temporary_base=${TMPDIR:-/tmp}
temporary_base=${temporary_base%/}
temporary_directory="$(realpath \
  "$(mktemp -d "$temporary_base/xe3-production-rehearsal-test.XXXXXX")")"
readonly temporary_directory
export TMPDIR="$temporary_directory"
readonly backup_directory="$temporary_directory/20260825T010203Z-predeploy"
readonly manifest="$temporary_directory/release-manifest.json"
readonly server_environment="$temporary_directory/server.env"
readonly receipt="$temporary_directory/receipt.json"
readonly dump="$backup_directory/database.dump"
readonly docker_marker="$temporary_directory/docker-called"
readonly dump_hash_marker="$temporary_directory/dump-hashed"
readonly wrapper_directory="$temporary_directory/bin"
readonly test_id="$(basename "$temporary_directory")"
readonly decoy_run_id="20260825T000000Z-1-$$"
readonly decoy_candidate="xe3-speakup-prod-rehearsal-candidate-$decoy_run_id"
readonly decoy_database="xe3-speakup-prod-rehearsal-db-$decoy_run_id"
readonly source_container="$test_id-source"
readonly source_volume="$test_id-source-data"
readonly fixture_builder="$test_id-image-builder"
readonly fixture_smoke="$test_id-image-smoke"
readonly previous_fixture_builder="$test_id-previous-image-builder"
readonly forward_fixture_builder="$test_id-forward-image-builder"
readonly missing_view_fixture_builder="$test_id-missing-view-image-builder"
readonly missing_barrier_fixture_builder="$test_id-missing-barrier-image-builder"
readonly public_grant_fixture_builder="$test_id-public-grant-image-builder"
readonly public_user_grant_fixture_builder="$test_id-public-user-grant-image-builder"
readonly invalid_index_fixture_builder="$test_id-invalid-index-image-builder"
readonly invalid_schema_contract_fixture_builder="$test_id-invalid-schema-contract-image-builder"
readonly invalid_voice_fixture_builder="$test_id-invalid-voice-image-builder"
readonly migration_failure_fixture_builder="$test_id-migration-failure-image-builder"
readonly missing_constraint_fixture_builder="$test_id-missing-constraint-image-builder"
readonly invalid_constraint_fixture_builder="$test_id-invalid-constraint-image-builder"
readonly negative_constraint_fixture_builder="$test_id-negative-constraint-image-builder"
readonly interrupt_fixture_builder="$test_id-interrupt-image-builder"
readonly fixture_image="xe3-speakup-prod-rehearsal-fixture:$test_id"
readonly previous_fixture_image="xe3-speakup-prod-rehearsal-previous-fixture:$test_id"
readonly forward_fixture_image="xe3-speakup-prod-rehearsal-forward-fixture:$test_id"
readonly missing_view_fixture_image="xe3-speakup-prod-rehearsal-missing-view:$test_id"
readonly missing_barrier_fixture_image="xe3-speakup-prod-rehearsal-missing-barrier:$test_id"
readonly public_grant_fixture_image="xe3-speakup-prod-rehearsal-public-grant:$test_id"
readonly public_user_grant_fixture_image="xe3-speakup-prod-rehearsal-public-user-grant:$test_id"
readonly invalid_index_fixture_image="xe3-speakup-prod-rehearsal-invalid-index:$test_id"
readonly invalid_schema_contract_fixture_image="xe3-speakup-prod-rehearsal-invalid-schema-contract:$test_id"
readonly invalid_voice_fixture_image="xe3-speakup-prod-rehearsal-invalid-voice:$test_id"
readonly migration_failure_fixture_image="xe3-speakup-prod-rehearsal-migration-failure:$test_id"
readonly missing_constraint_fixture_image="xe3-speakup-prod-rehearsal-missing-constraint:$test_id"
readonly invalid_constraint_fixture_image="xe3-speakup-prod-rehearsal-invalid-constraint:$test_id"
readonly negative_constraint_fixture_image="xe3-speakup-prod-rehearsal-negative-constraint:$test_id"
readonly interrupt_fixture_image="xe3-speakup-prod-rehearsal-interrupt:$test_id"
fixture_image_id=''
previous_fixture_image_id=''
forward_fixture_image_id=''
missing_view_fixture_image_id=''
missing_barrier_fixture_image_id=''
public_grant_fixture_image_id=''
public_user_grant_fixture_image_id=''
invalid_index_fixture_image_id=''
invalid_schema_contract_fixture_image_id=''
invalid_voice_fixture_image_id=''
migration_failure_fixture_image_id=''
missing_constraint_fixture_image_id=''
invalid_constraint_fixture_image_id=''
negative_constraint_fixture_image_id=''
interrupt_fixture_image_id=''
declare -a observed_rehearsal_runs=()

cleanup() {
  local status=$?
  local observed_run
  trap - EXIT INT TERM
  set +e
  set +u
  for observed_run in "${observed_rehearsal_runs[@]}"; do
    remove_rehearsal_run "$observed_run" >/dev/null 2>&1 || status=1
  done
  remove_test_container "$source_container" >/dev/null 2>&1 || status=1
  remove_test_container "$fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$fixture_smoke" >/dev/null 2>&1 || status=1
  remove_test_container "$previous_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$forward_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$missing_view_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$missing_barrier_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$public_grant_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$public_user_grant_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$invalid_index_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$invalid_schema_contract_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$invalid_voice_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$migration_failure_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$missing_constraint_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$invalid_constraint_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$negative_constraint_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$interrupt_fixture_builder" >/dev/null 2>&1 || status=1
  remove_test_container "$decoy_candidate" >/dev/null 2>&1 || status=1
  remove_test_container "$decoy_database" >/dev/null 2>&1 || status=1
  remove_test_volume "$source_volume" >/dev/null 2>&1 || status=1
  remove_test_image >/dev/null 2>&1 || status=1
  if [[ "$temporary_directory" == */xe3-production-rehearsal-test.* ]]; then
    rm -rf -- "$temporary_directory"
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

mkdir -m 0700 "$backup_directory" "$wrapper_directory"
write_manifest
printf 'DASHSCOPE_API_KEY=%s\n' "$secret_value" >"$server_environment"
printf 'OSS_ENABLED=1\n' >>"$server_environment"
printf 'RESUME_OCR_ENABLED=1\n' >>"$server_environment"
printf 'APPID=%s\n' "$secret_value" >>"$server_environment"
printf 'APIKey=%s\n' "$secret_value" >>"$server_environment"
printf 'APISecret=%s\n' "$secret_value" >>"$server_environment"
for key in HTTP_PROXY HTTPS_PROXY FTP_PROXY ALL_PROXY NO_PROXY \
  http_proxy https_proxy ftp_proxy all_proxy no_proxy; do
  printf '%s=%s\n' "$key" "$secret_value" >>"$server_environment"
done
printf 'PGDMP synthetic dry-run fixture\n' >"$dump"
dump_sha256=$(sha256_file "$dump")
dump_size=$(wc -c <"$dump" | tr -d '[:space:]')
printf '%s  database.dump\n' "$dump_sha256" >"$backup_directory/database.dump.sha256"
jq --null-input \
  --arg sha256 "$dump_sha256" \
  --arg postgres_image "$postgres_image" \
  --argjson size_bytes "$dump_size" '
    {
      format_version: 1,
      backup_id: "20260825T010203Z-predeploy",
      created_at: "2026-08-25T01:02:03Z",
      backup_type: "predeploy",
      deployment_version: "0.1.4",
      git_sha: "2222222222222222222222222222222222222222",
      source_compose_project: "xe3-speakup-production",
      source_compose_service: "postgres",
      source_volume: "xe3-speakup-postgres-data",
      postgres_image: $postgres_image,
      postgres_version: "18.0",
      database_name: "speakup",
      database_user: "speakup",
      schema_version: 9,
      schema_dirty: false,
      size_bytes: $size_bytes,
      sha256: $sha256
    }
  ' >"$backup_directory/metadata.json"
chmod 0600 \
  "$manifest" "$server_environment" "$dump" \
  "$backup_directory/database.dump.sha256" \
  "$backup_directory/metadata.json"

real_sha256sum=$(command -v sha256sum || true)
real_shasum=$(command -v shasum || true)
cat >"$wrapper_directory/docker" <<'EOF'
#!/usr/bin/env bash
: >"$TEST_DOCKER_MARKER"
exit 97
EOF
cat >"$wrapper_directory/sha256sum" <<'EOF'
#!/usr/bin/env bash
for argument in "$@"; do
  if [[ "$argument" == */database.dump ]]; then
    : >"$TEST_DUMP_HASH_MARKER"
    exit 96
  fi
done
exec "$TEST_REAL_SHA256SUM" "$@"
EOF
cat >"$wrapper_directory/shasum" <<'EOF'
#!/usr/bin/env bash
for argument in "$@"; do
  if [[ "$argument" == */database.dump ]]; then
    : >"$TEST_DUMP_HASH_MARKER"
    exit 96
  fi
done
exec "$TEST_REAL_SHASUM" "$@"
EOF
chmod 0700 "$wrapper_directory/docker" "$wrapper_directory/sha256sum" "$wrapper_directory/shasum"

if [[ -n "$real_sha256sum" ]]; then
  export TEST_REAL_SHA256SUM=$real_sha256sum
else
  rm "$wrapper_directory/sha256sum"
fi
if [[ -n "$real_shasum" ]]; then
  export TEST_REAL_SHASUM=$real_shasum
else
  rm "$wrapper_directory/shasum"
fi
export TEST_DOCKER_MARKER=$docker_marker
export TEST_DUMP_HASH_MARKER=$dump_hash_marker

invalid_environment="$temporary_directory/invalid-server.env"
invalid_environment_receipt="$temporary_directory/invalid-env-receipt.json"
printf 'DASHSCOPE_API_KEY=invalid-mode\n' >"$invalid_environment"
chmod 0644 "$invalid_environment"
expect_failure 'invalid environment execute gate' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" --execute \
    --backup "$backup_directory" \
    --manifest "$manifest" \
    --previous-server-image "$previous_image" \
    --server-env-file "$invalid_environment" \
    --receipt "$invalid_environment_receipt" \
    --lock-timeout-seconds 3
verify_failed_receipt "$invalid_environment_receipt" environment_validation
[[ ! -e "$docker_marker" ]] || fail 'invalid environment called Docker'

dry_run_output="$(PATH="$wrapper_directory:$PATH" "$rehearsal" \
  --backup "$backup_directory" \
  --manifest "$manifest" \
  --previous-server-image "$previous_image" \
  --server-env-file "$server_environment" \
  --receipt "$receipt" \
  --lock-timeout-seconds 3)"
[[ "$dry_run_output" == \
  'backup_id=20260825T010203Z-predeploy version=0.1.6 source_schema=9 target_schema=15 forward_hotfix=not_provided readiness_profile=core_database_external_integrations_disabled production_provider_readiness=not_verified dry_run=true docker_touched=false' ]] ||
  fail 'dry-run returned an unexpected contract'
[[ -z "$(find "$temporary_directory" -maxdepth 1 -type d \
  -name 'xe3-production-rehearsal-inputs.*' -print -quit)" ]] ||
  fail 'dry-run leaked a private input snapshot'
[[ ! -e "$docker_marker" ]] || fail 'dry-run called Docker'
[[ ! -e "$dump_hash_marker" ]] || fail 'dry-run read database.dump'
[[ ! -e "$receipt" ]] || fail 'dry-run wrote a receipt'
! grep -Fq "$secret_value" <<<"$dry_run_output" ||
  fail 'dry-run exposed the server environment secret'

jq '.database_schema_version = 14' "$manifest" >"$temporary_directory/schema14.json"
chmod 0600 "$temporary_directory/schema14.json"
expect_failure 'wrong target schema' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" \
    --backup "$backup_directory" \
    --manifest "$temporary_directory/schema14.json" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$receipt" \
    --lock-timeout-seconds 3
[[ ! -e "$docker_marker" ]] || fail 'invalid manifest called Docker'

invalid_manifest_receipt="$temporary_directory/invalid-manifest-receipt.json"
expect_failure 'wrong target schema execute gate' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" --execute \
    --backup "$backup_directory" \
    --manifest "$temporary_directory/schema14.json" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$invalid_manifest_receipt" \
    --lock-timeout-seconds 3
verify_failed_receipt "$invalid_manifest_receipt" manifest_validation

invalid_backup_directory="$temporary_directory/20260825T010204Z-predeploy"
invalid_metadata_receipt="$temporary_directory/invalid-metadata-receipt.json"
mkdir -m 0700 "$invalid_backup_directory"
cp "$dump" "$backup_directory/database.dump.sha256" \
  "$invalid_backup_directory/"
jq '
  .backup_id = "20260825T010204Z-predeploy" |
  .created_at = "2026-08-25T01:02:04Z" |
  .schema_version = 8
' "$backup_directory/metadata.json" >"$invalid_backup_directory/metadata.json"
chmod 0600 "$invalid_backup_directory"/*
expect_failure 'invalid backup metadata execute gate' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" --execute \
    --backup "$invalid_backup_directory" \
    --manifest "$manifest" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$invalid_metadata_receipt" \
    --lock-timeout-seconds 3
verify_failed_receipt "$invalid_metadata_receipt" backup_metadata_validation
[[ ! -e "$docker_marker" ]] || fail 'invalid backup metadata called Docker'

printf 'X' | dd of="$dump" bs=1 seek=6 conv=notrunc 2>/dev/null
expect_failure 'corrupt dump execute gate' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" --execute \
    --backup "$backup_directory" \
    --manifest "$manifest" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$receipt" \
    --lock-timeout-seconds 3
[[ -e "$dump_hash_marker" ]] || fail 'execute did not verify database.dump'
[[ ! -e "$docker_marker" ]] || fail 'invalid backup called Docker'
[[ -f "$receipt" && ! -L "$receipt" ]] ||
  fail 'failed execute did not write an audit receipt'
[[ "$(file_mode "$receipt")" == 600 ]] ||
  fail 'failed execute receipt is not mode 0600'
jq --exit-status '
  .receipt_version == 1 and
  .operation == "production_schema_upgrade_rehearsal" and
  .environment == "isolated" and
  .status == "failed" and
  .failed_step == "backup_content_validation" and
  .source_schema == 9 and .target_schema == 15 and
  .checks.owned_resource_cleanup == true
' "$receipt" >/dev/null || fail 'failed execute receipt is incomplete'
! grep -Fq "$secret_value" "$receipt" || fail 'receipt exposed a server secret'
! grep -Fq "$backup_directory" "$receipt" || fail 'receipt exposed the backup path'

[[ "$("$production_directory/manage.sh" validate-rollback-schema \
  --current-schema 15 --target-schema 15)" == \
  'current_schema=15 target_schema=15 rollback_allowed=true' ]] ||
  fail 'Production rollback schema guard rejected an equal schema'
expect_failure 'Production rollback schema guard mismatch' \
  "$production_directory/manage.sh" validate-rollback-schema \
    --current-schema 15 --target-schema 9
rm -f "$wrapper_directory/sha256sum" "$wrapper_directory/shasum"

# The runtime fixture uses only the already-required PostgreSQL 18 image. Its
# migration entrypoint applies the repository's real schema 10 through 15 SQL,
# while
# its HTTP process exposes only a local readiness endpoint.
fixture_directory="$temporary_directory/fixture-image"
mkdir -m 0700 "$fixture_directory"
for migration in \
  "$production_directory"/../../server/migrations/00001[0-5]*.up.sql; do
  migration_version=${migration##*/}
  migration_version=${migration_version%%_*}
  cp "$migration" "$fixture_directory/$migration_version.up.sql"
done
cat >"$fixture_directory/speakup-migrate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == up ]] || exit 2
[[ "${REHEARSAL_FIXTURE_FAULT:-}" != migration-failure ]] || exit 5
sleep 2
version="$(psql "$DATABASE_URL" --no-psqlrc --tuples-only --no-align --quiet \
  --set ON_ERROR_STOP=1 --command 'SELECT version FROM public.schema_migrations;')"
[[ "$version" =~ ^[0-9]+$ ]] && ((version >= 9 && version <= 15)) || exit 3
for ((next_version = version + 1; next_version <= 15; next_version += 1)); do
  migration_file=$(printf '/fixture/%06d.up.sql' "$next_version")
  psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
    --file "$migration_file" >/dev/null
  psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
    --command "UPDATE public.schema_migrations SET version = $next_version;" \
    >/dev/null
done
case "${REHEARSAL_FIXTURE_FAULT:-}" in
  '' | server-unready) ;;
  missing-view)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'DROP VIEW public.user_behavior_daily_feature_usage;' >/dev/null
    ;;
  missing-barrier)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'ALTER VIEW public.user_behavior_daily_feature_usage SET (security_barrier = false);' \
      >/dev/null
    ;;
  public-grant)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'GRANT SELECT (day_utc) ON public.product_health_daily_artifact_coverage TO PUBLIC;' \
      >/dev/null
    ;;
  public-user-grant)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'GRANT SELECT ON public.user_behavior_daily_feature_usage TO PUBLIC;' \
      >/dev/null
    ;;
  invalid-index)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'DROP INDEX public.user_behavior_nonterminal_sessions_updated_idx;' \
      --command "CREATE INDEX user_behavior_nonterminal_sessions_updated_idx ON public.practice_sessions (created_at) WHERE status IN ('starting', 'in_progress', 'paused');" \
      >/dev/null
    ;;
  invalid-schema-contract)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'ALTER TABLE public.practice_sessions DROP CONSTRAINT practice_sessions_presentation_snapshot_check;' \
      --command 'ALTER TABLE public.practice_sessions ADD CONSTRAINT practice_sessions_presentation_snapshot_check CHECK (true);' \
      >/dev/null
    ;;
  invalid-voice)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command "UPDATE public.coach_voice_options SET provider_model = 'qwen-audio-3.0-tts-flash-bad' WHERE id = 'voice_mary';" \
      >/dev/null
    ;;
  missing-constraint)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'ALTER TABLE public.evaluations DROP CONSTRAINT evaluations_kind_check;' \
      >/dev/null
    ;;
  invalid-constraint)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'ALTER TABLE public.evaluations DROP CONSTRAINT evaluations_kind_check;' \
      --command "ALTER TABLE public.evaluations ADD CONSTRAINT evaluations_kind_check CHECK (kind IN ('SESSION_REPORT', 'PRACTICE_TURN_FEEDBACK', 'AGENT_MESSAGE_FEEDBACK', 'IELTS_PART1_PROFILE', 'IELTS_PART2_PROFILE', 'UNREVIEWED_EXTRA'));" \
      >/dev/null
    ;;
  negative-constraint)
    psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 \
      --command 'ALTER TABLE public.evaluations DROP CONSTRAINT evaluations_kind_check;' \
      --command "ALTER TABLE public.evaluations ADD CONSTRAINT evaluations_kind_check CHECK (kind NOT IN ('SESSION_REPORT', 'PRACTICE_TURN_FEEDBACK', 'AGENT_MESSAGE_FEEDBACK', 'IELTS_PART1_PROFILE', 'IELTS_PART2_PROFILE'));" \
      >/dev/null
    ;;
  *) exit 4 ;;
esac
EOF
cat >"$fixture_directory/speakup-server" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${DATABASE_URL:-}" ]]; then
  [[ "${OSS_ENABLED:-}" == 0 && "${RESUME_OCR_ENABLED:-}" == 0 ]] || exit 6
  for key in OSS_ENABLED RESUME_OCR_ENABLED APPID APIKey APISecret \
    HTTP_PROXY HTTPS_PROXY FTP_PROXY ALL_PROXY NO_PROXY \
    http_proxy https_proxy ftp_proxy all_proxy no_proxy; do
    [[ "$(env | grep -c "^${key}=")" == 1 ]] || exit 8
  done
  for key in APPID APIKey APISecret HTTP_PROXY HTTPS_PROXY FTP_PROXY \
    ALL_PROXY NO_PROXY http_proxy https_proxy ftp_proxy all_proxy no_proxy; do
    [[ -v "$key" && -z "${!key}" ]] || exit 7
  done
fi
if [[ "${REHEARSAL_FIXTURE_FAULT:-}" == server-unready ]]; then
  sleep 300
  exit 5
fi
exec perl -MIO::Socket::INET -e '
  my $server = IO::Socket::INET->new(
    LocalAddr => "127.0.0.1", LocalPort => 8080, Listen => 5,
    ReuseAddr => 1, Proto => "tcp"
  ) or die "listen failed";
  while (my $client = $server->accept()) {
    scalar <$client>;
    print $client "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok";
    close $client;
  }
'
EOF
cat >"$fixture_directory/wget" <<'EOF'
#!/usr/bin/env bash
exec perl -MIO::Socket::INET -e '
  my $client = IO::Socket::INET->new(
    PeerAddr => "127.0.0.1", PeerPort => 8080, Proto => "tcp", Timeout => 2
  );
  exit($client ? 0 : 1);
'
EOF
chmod 0755 \
  "$fixture_directory/speakup-migrate" \
  "$fixture_directory/speakup-server" \
  "$fixture_directory/wget"
docker container create \
  --name "$fixture_builder" \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --entrypoint /bin/true \
  "$postgres_image" >/dev/null
docker container cp "$fixture_directory" "$fixture_builder:/fixture"
docker container cp \
  "$fixture_directory/speakup-migrate" \
  "$fixture_builder:/usr/local/bin/speakup-migrate"
docker container cp \
  "$fixture_directory/speakup-server" \
  "$fixture_builder:/usr/local/bin/speakup-server"
docker container cp \
  "$fixture_directory/wget" \
  "$fixture_builder:/usr/local/bin/wget"
docker container commit \
  --change 'ENTRYPOINT ["/usr/local/bin/speakup-server"]' \
  --change 'CMD []' \
  --change 'LABEL org.opencontainers.image.version=0.1.6' \
  --change 'LABEL org.opencontainers.image.revision=cccccccccccccccccccccccccccccccccccccccc' \
  --change "LABEL com.xengineer.speakup.test-run=$test_id" \
  "$fixture_builder" "$fixture_image" >/dev/null
remove_test_container "$fixture_builder" || fail 'cannot remove fixture image builder'
fixture_image_id=$(docker image inspect --format '{{.Id}}' "$fixture_image")
[[ "$fixture_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'fixture image ID is invalid'
docker container run --detach --pull never \
  --name "$fixture_smoke" --network none \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$fixture_image_id" >/dev/null
sleep 1
if ! docker container exec "$fixture_smoke" \
  wget -q -T 2 --spider http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
  docker container inspect "$fixture_smoke" --format \
    'fixture state={{.State.Status}} exit={{.State.ExitCode}} error={{.State.Error}}' >&2 || true
  docker container logs "$fixture_smoke" >&2 || true
  fail 'fixture Server image is not runnable'
fi
remove_test_container "$fixture_smoke" || fail 'cannot remove fixture smoke container'

docker container create \
  --name "$previous_fixture_builder" \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --entrypoint /bin/true \
  "$fixture_image_id" >/dev/null
docker container commit \
  --change 'ENTRYPOINT ["/usr/local/bin/speakup-server"]' \
  --change 'CMD []' \
  --change 'LABEL org.opencontainers.image.version=0.1.4' \
  --change 'LABEL org.opencontainers.image.revision=2222222222222222222222222222222222222222' \
  --change "LABEL com.xengineer.speakup.test-run=$test_id" \
  "$previous_fixture_builder" "$previous_fixture_image" >/dev/null
previous_fixture_image_id=$(docker image inspect --format '{{.Id}}' \
  "$previous_fixture_image")
[[ "$previous_fixture_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'previous fixture image ID is invalid'
remove_test_container "$previous_fixture_builder" ||
  fail 'cannot remove previous fixture image builder'

docker container create \
  --name "$forward_fixture_builder" \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --entrypoint /bin/true \
  "$fixture_image_id" >/dev/null
docker container commit \
  --change 'ENTRYPOINT ["/usr/local/bin/speakup-server"]' \
  --change 'CMD []' \
  --change 'LABEL org.opencontainers.image.version=0.1.7' \
  --change 'LABEL org.opencontainers.image.revision=ffffffffffffffffffffffffffffffffffffffff' \
  --change "LABEL com.xengineer.speakup.test-run=$test_id" \
  "$forward_fixture_builder" "$forward_fixture_image" >/dev/null
forward_fixture_image_id=$(docker image inspect --format '{{.Id}}' \
  "$forward_fixture_image")
[[ "$forward_fixture_image_id" =~ ^sha256:[a-f0-9]{64}$ &&
  "$forward_fixture_image_id" != "$fixture_image_id" &&
  "$forward_fixture_image_id" != "$previous_fixture_image_id" ]] ||
  fail 'forward fixture must have a distinct immutable image ID'
remove_test_container "$forward_fixture_builder" ||
  fail 'cannot remove forward fixture image builder'

create_fixture_variant() {
  local fault=$1 builder=$2 image=$3 image_id

  docker container create \
    --name "$builder" \
    --label "com.xengineer.speakup.test-run=$test_id" \
    --entrypoint /bin/true \
    "$fixture_image_id" >/dev/null
  docker container commit \
    --change "ENV REHEARSAL_FIXTURE_FAULT=$fault" \
    --change "LABEL com.xengineer.speakup.test-run=$test_id" \
    "$builder" "$image" >/dev/null
  image_id=$(docker image inspect --format '{{.Id}}' "$image")
  [[ "$image_id" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    fail "$fault fixture image ID is invalid"
  remove_test_container "$builder" || fail "cannot remove $fault fixture builder"
  printf '%s\n' "$image_id"
}

missing_view_fixture_image_id=$(create_fixture_variant \
  missing-view "$missing_view_fixture_builder" "$missing_view_fixture_image")
missing_barrier_fixture_image_id=$(create_fixture_variant \
  missing-barrier "$missing_barrier_fixture_builder" \
  "$missing_barrier_fixture_image")
public_grant_fixture_image_id=$(create_fixture_variant \
  public-grant "$public_grant_fixture_builder" "$public_grant_fixture_image")
public_user_grant_fixture_image_id=$(create_fixture_variant \
  public-user-grant "$public_user_grant_fixture_builder" \
  "$public_user_grant_fixture_image")
invalid_index_fixture_image_id=$(create_fixture_variant \
  invalid-index "$invalid_index_fixture_builder" "$invalid_index_fixture_image")
invalid_schema_contract_fixture_image_id=$(create_fixture_variant \
  invalid-schema-contract "$invalid_schema_contract_fixture_builder" \
  "$invalid_schema_contract_fixture_image")
invalid_voice_fixture_image_id=$(create_fixture_variant \
  invalid-voice "$invalid_voice_fixture_builder" "$invalid_voice_fixture_image")
migration_failure_fixture_image_id=$(create_fixture_variant \
  migration-failure "$migration_failure_fixture_builder" \
  "$migration_failure_fixture_image")
missing_constraint_fixture_image_id=$(create_fixture_variant \
  missing-constraint "$missing_constraint_fixture_builder" \
  "$missing_constraint_fixture_image")
invalid_constraint_fixture_image_id=$(create_fixture_variant \
  invalid-constraint "$invalid_constraint_fixture_builder" \
  "$invalid_constraint_fixture_image")
negative_constraint_fixture_image_id=$(create_fixture_variant \
  negative-constraint "$negative_constraint_fixture_builder" \
  "$negative_constraint_fixture_image")
interrupt_fixture_image_id=$(create_fixture_variant \
  server-unready "$interrupt_fixture_builder" "$interrupt_fixture_image")

docker volume create \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$source_volume" >/dev/null
docker container run \
  --detach \
  --pull never \
  --name "$source_container" \
  --network none \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --mount "type=volume,src=$source_volume,dst=/var/lib/postgresql,volume-nocopy" \
  --env POSTGRES_DB=speakup \
  --env POSTGRES_USER=speakup \
  --env POSTGRES_HOST_AUTH_METHOD=trust \
  "$postgres_image" >/dev/null
wait_for_postgres "$source_container"
for migration in \
  "$production_directory"/../../server/migrations/00000[1-9]*.up.sql; do
  docker container exec --interactive "$source_container" \
    psql --no-psqlrc --set ON_ERROR_STOP=1 \
      --username speakup --dbname speakup <"$migration" >/dev/null
done
docker container exec "$source_container" \
  psql --no-psqlrc --set ON_ERROR_STOP=1 \
    --username speakup --dbname speakup \
    --command 'CREATE TABLE public.schema_migrations (version bigint NOT NULL, dirty boolean NOT NULL);' \
    --command 'INSERT INTO public.schema_migrations (version, dirty) VALUES (9, false);' \
    >/dev/null
docker container exec "$source_container" \
  pg_dump --format custom --no-owner --no-privileges \
    --username speakup --dbname speakup >"$dump"
dump_sha256=$(sha256_file "$dump")
dump_size=$(wc -c <"$dump" | tr -d '[:space:]')
printf '%s  database.dump\n' "$dump_sha256" >"$backup_directory/database.dump.sha256"
jq \
  --arg sha256 "$dump_sha256" \
  --argjson size_bytes "$dump_size" \
  '.sha256 = $sha256 | .size_bytes = $size_bytes' \
  "$backup_directory/metadata.json" >"$temporary_directory/metadata.updated.json"
mv "$temporary_directory/metadata.updated.json" "$backup_directory/metadata.json"
chmod 0600 "$dump" "$backup_directory/database.dump.sha256" \
  "$backup_directory/metadata.json"
remove_test_container "$source_container" || fail 'cannot remove source fixture container'
remove_test_volume "$source_volume" || fail 'cannot remove source fixture volume'

command_log="$temporary_directory/docker-runtime.log"
cat >"$wrapper_directory/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
arguments=("$@")
if [[ "$#" == 3 && "$1" == container && "$2" == inspect &&
  -s "$TEST_INSPECT_ERROR_TARGET_FILE" &&
  "$3" == "$(<"$TEST_INSPECT_ERROR_TARGET_FILE")" ]]; then
  if [[ ! -e "$TEST_INSPECT_ERROR_INJECTED_FILE" ]]; then
    (umask 077 && printf '%s\n' "$3" >"$TEST_INSPECT_ERROR_INJECTED_FILE")
  fi
  exit 74
fi
if [[ "$#" == 3 && "$1" == image && "$2" == inspect &&
  ("$3" == "$TEST_CANDIDATE_IMAGE" || "$3" == "$TEST_PREVIOUS_IMAGE" ||
   "$3" == "$TEST_FORWARD_IMAGE") ]]; then
  selected_image=$TEST_FIXTURE_IMAGE_ID
  if [[ "$3" == "$TEST_CANDIDATE_IMAGE" ]]; then
    selected_image=$TEST_CANDIDATE_FIXTURE_IMAGE_ID
  fi
  if [[ "$3" == "$TEST_PREVIOUS_IMAGE" ]]; then
    selected_image=$TEST_PREVIOUS_FIXTURE_IMAGE_ID
  fi
  if [[ "$3" == "$TEST_FORWARD_IMAGE" ]]; then
    selected_image=$TEST_FORWARD_FIXTURE_IMAGE_ID
  fi
  "$TEST_REAL_DOCKER" image inspect "$selected_image" |
    jq --arg reference "$3" '.[0].RepoDigests = [$reference]'
  exit 0
fi
{
  printf '%q' "$1"
  shift
  printf ' %q' "$@"
  printf '\n'
} >>"$TEST_DOCKER_COMMAND_LOG"
exec "$TEST_REAL_DOCKER" "${arguments[@]}"
EOF
chmod 0700 "$wrapper_directory/docker"
export TEST_REAL_DOCKER="$(command -v docker)"
export TEST_CANDIDATE_IMAGE="ghcr.io/1024xengineer/xe3-esl-server@$candidate_digest"
export TEST_PREVIOUS_IMAGE="$previous_image"
export TEST_FORWARD_IMAGE="$forward_image"
export TEST_FIXTURE_IMAGE_ID="$fixture_image_id"
export TEST_CANDIDATE_FIXTURE_IMAGE_ID="$fixture_image_id"
export TEST_PREVIOUS_FIXTURE_IMAGE_ID="$previous_fixture_image_id"
export TEST_FORWARD_FIXTURE_IMAGE_ID="$forward_fixture_image_id"
export TEST_DOCKER_COMMAND_LOG="$command_log"
export TEST_INSPECT_ERROR_TARGET_FILE="$temporary_directory/inspect-error-target"
export TEST_INSPECT_ERROR_INJECTED_FILE="$temporary_directory/inspect-error-injected"

migration_failure_receipt="$temporary_directory/migration-failure-receipt.json"
export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$migration_failure_fixture_image_id
expect_failure 'candidate migration failure' env PATH="$wrapper_directory:$PATH" \
  "$rehearsal" --execute \
    --backup "$backup_directory" \
    --manifest "$manifest" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$migration_failure_receipt" \
    --lock-timeout-seconds 2
verify_failed_receipt "$migration_failure_receipt" migration
jq --exit-status '
  .source_schema == 9 and .target_schema == 15 and
  .checks.clean_target_schema == false and
  .checks.idempotent_migration == false
' "$migration_failure_receipt" >/dev/null ||
  fail 'migration failure receipt has wrong checks'
migration_failure_run_id=$(jq --raw-output '.run_id' \
  "$migration_failure_receipt")
observed_rehearsal_runs+=("$migration_failure_run_id")
assert_no_rehearsal_resources "$migration_failure_run_id"
export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$fixture_image_id

success_receipt="$temporary_directory/success-receipt.json"
success_output="$(PATH="$wrapper_directory:$PATH" "$rehearsal" --execute \
  --backup "$backup_directory" \
  --manifest "$manifest" \
  --previous-server-image "$previous_image" \
  --forward-server-image "$forward_image" \
  --server-env-file "$server_environment" \
  --receipt "$success_receipt" \
  --lock-timeout-seconds 2)"
success_run_id=$(jq --raw-output '.run_id' "$success_receipt")
observed_rehearsal_runs+=("$success_run_id")
[[ "$success_output" == *'rehearsal=verified' ]] ||
  fail 'successful rehearsal returned an invalid contract'
jq --exit-status '
  .status == "succeeded" and .failed_step == null and
  .server_readiness_profile == "core_database_external_integrations_disabled" and
  .production_provider_readiness == "not_verified" and
  .source_schema == 9 and .target_schema == 15 and
  .checks.clean_target_schema == true and
  .checks.product_health_views == true and
  .checks.user_behavior_views == true and
  .checks.view_public_privileges_revoked == true and
  .checks.schema_10_14_contracts == true and
  .checks.ielts_evaluation_constraint == true and
  .checks.candidate_readiness == true and
  .checks.previous_image_readiness_only == true and
  .checks.previous_image_profile_processing == "not_verified" and
  .checks.schema9_rollback_guard == true and
  .checks.idempotent_migration == true and
  .checks.same_schema_candidate_redeploy == false and
  .checks.forward_hotfix_image == "verified" and
  .forward_server_image == "ghcr.io/1024xengineer/xe3-esl-server@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" and
  .forward_server_image_version == "0.1.7" and
  .forward_server_image_revision == "ffffffffffffffffffffffffffffffffffffffff" and
  .checks.owned_resource_cleanup == true
' "$success_receipt" >/dev/null || fail 'successful rehearsal receipt is incomplete'
[[ "$(file_mode "$success_receipt")" == 600 ]] ||
  fail 'successful rehearsal receipt is not mode 0600'
! grep -Fq "$secret_value" "$success_receipt" ||
  fail 'successful receipt exposed the server secret'
! grep -Fq "$backup_directory" "$success_receipt" ||
  fail 'successful receipt exposed the backup path'
[[ -z "$(docker container ls --all --quiet \
  --filter "label=com.xengineer.speakup.production-rehearsal-run-id=$success_run_id")" ]] ||
  fail 'successful rehearsal leaked a container'
! docker network inspect "xe3-speakup-prod-rehearsal-$success_run_id" >/dev/null 2>&1 ||
  fail 'successful rehearsal leaked its network'
! docker volume inspect "xe3-speakup-prod-rehearsal-$success_run_id" >/dev/null 2>&1 ||
  fail 'successful rehearsal leaked its volume'
grep -Eq '^network create --internal ' "$command_log" ||
  fail 'rehearsal did not create an internal network'
if grep -Eq -- '--publish|(^|[[:space:]])-p([[:space:]]|$)|xe3-speakup-production_database' \
  "$command_log"; then
  fail 'rehearsal used a host port or Production network'
fi
container_create_count=$(grep -Ec '^container create ' "$command_log")
((container_create_count >= 6)) || fail 'rehearsal did not create all isolated containers'
while IFS= read -r create_command; do
  [[ "$create_command" == *'--cpus 1.0'* &&
    "$create_command" == *'--memory 512m'* &&
    "$create_command" == *'--pids-limit 256'* &&
    "$create_command" == *'--log-driver local'* &&
    "$create_command" == *'--log-opt max-size=10m'* &&
    "$create_command" == *'--log-opt max-file=3'* ]] ||
    fail 'an isolated container is missing shared-server resource limits'
done < <(grep -E '^container create ' "$command_log")

run_schema_fault_test() {
  local fault=$1 fixture_id=$2 expected_product_views=$3
  local expected_user_views=$4 expected_privileges=$5
  local expected_schema_contracts=$6
  local fault_receipt="$temporary_directory/$fault-receipt.json"
  local fault_run_id

  export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$fixture_id
  expect_failure "$fault schema verification" env PATH="$wrapper_directory:$PATH" \
    "$rehearsal" --execute \
      --backup "$backup_directory" \
      --manifest "$manifest" \
      --previous-server-image "$previous_image" \
      --server-env-file "$server_environment" \
      --receipt "$fault_receipt" \
      --lock-timeout-seconds 2
  verify_failed_receipt "$fault_receipt" schema_verification
  jq --exit-status \
    --argjson expected_product_views "$expected_product_views" \
    --argjson expected_user_views "$expected_user_views" \
    --argjson expected_privileges "$expected_privileges" \
    --argjson expected_schema_contracts "$expected_schema_contracts" '
    .checks.clean_target_schema == true and
    .checks.product_health_views == $expected_product_views and
    .checks.user_behavior_views == $expected_user_views and
    .checks.view_public_privileges_revoked == $expected_privileges and
    .checks.schema_10_14_contracts == $expected_schema_contracts and
    .checks.ielts_evaluation_constraint == false
  ' "$fault_receipt" >/dev/null || fail "$fault failure receipt has wrong checks"
  fault_run_id=$(jq --raw-output '.run_id' "$fault_receipt")
  observed_rehearsal_runs+=("$fault_run_id")
  assert_no_rehearsal_resources "$fault_run_id"
}

run_schema_fault_test missing-view "$missing_view_fixture_image_id" true false false false
run_schema_fault_test missing-barrier "$missing_barrier_fixture_image_id" true false false false
run_schema_fault_test invalid-index "$invalid_index_fixture_image_id" true false false false
run_schema_fault_test public-grant "$public_grant_fixture_image_id" true true false false
run_schema_fault_test public-user-grant "$public_user_grant_fixture_image_id" true true false false
run_schema_fault_test invalid-schema-contract "$invalid_schema_contract_fixture_image_id" true true true false
run_schema_fault_test invalid-voice "$invalid_voice_fixture_image_id" true true true false
run_schema_fault_test missing-constraint "$missing_constraint_fixture_image_id" true true true true
run_schema_fault_test invalid-constraint "$invalid_constraint_fixture_image_id" true true true true
run_schema_fault_test negative-constraint "$negative_constraint_fixture_image_id" true true true true
export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$fixture_image_id

decoy_candidate_id=$(docker container run --detach --pull never \
  --name "$decoy_candidate" --network none \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --label 'com.xengineer.speakup.production-rehearsal=true' \
  --label "com.xengineer.speakup.production-rehearsal-run-id=$decoy_run_id" \
  "$fixture_image_id")
decoy_database_id=$(docker container run --detach --pull never \
  --name "$decoy_database" --network none \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --label 'com.xengineer.speakup.production-rehearsal=true' \
  --label "com.xengineer.speakup.production-rehearsal-run-id=$decoy_run_id" \
  "$fixture_image_id")

lock_receipt="$temporary_directory/lock-timeout-receipt.json"
lock_output="$temporary_directory/lock-timeout.out"
PATH="$wrapper_directory:$PATH" "$rehearsal" --execute \
  --backup "$backup_directory" \
  --manifest "$manifest" \
  --previous-server-image "$previous_image" \
  --server-env-file "$server_environment" \
  --receipt "$lock_receipt" \
  --lock-timeout-seconds 1 >"$lock_output" 2>&1 &
rehearsal_pid=$!
if ! wait_for_pid_rehearsal_container "$rehearsal_pid" db; then
  wait "$rehearsal_pid" || true
  fail 'lock-timeout rehearsal did not create one PID-owned database'
fi
lock_database=$selected_background_container
lock_run_id=$selected_background_run_id
observed_rehearsal_runs+=("$lock_run_id")

schema9_ready=false
for ((attempt = 1; attempt <= 150; attempt += 1)); do
  if [[ "$(docker container exec --user postgres "$lock_database" \
    psql --no-psqlrc --tuples-only --no-align --quiet \
      --username speakup --dbname speakup \
      --command "SELECT count(*) FROM public.schema_migrations WHERE version = 9 AND dirty = false AND to_regclass('public.evaluations') IS NOT NULL;" \
      2>/dev/null || true)" == 1 ]]; then
    schema9_ready=true
    break
  fi
  sleep 0.1
done
[[ "$schema9_ready" == true ]] || {
  wait "$rehearsal_pid" || true
  fail 'lock-timeout rehearsal did not restore clean schema 9'
}

verify_pid_rehearsal_container "$rehearsal_pid" db \
  "$lock_database" "$lock_run_id" ||
  fail 'lock-timeout database ownership changed before acquiring the test lock'

docker container exec --user postgres "$lock_database" \
  psql --no-psqlrc --set ON_ERROR_STOP=1 \
    --username speakup --dbname speakup \
    --command 'BEGIN; LOCK TABLE public.evaluations IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(20);' \
    >"$temporary_directory/lock-holder.out" 2>&1 &
lock_holder_pid=$!
lock_granted=false
for ((attempt = 1; attempt <= 50; attempt += 1)); do
  if [[ "$(docker container exec --user postgres "$lock_database" \
    psql --no-psqlrc --tuples-only --no-align --quiet \
      --username speakup --dbname speakup \
      --command "SELECT count(*) FROM pg_catalog.pg_locks l JOIN pg_catalog.pg_class c ON c.oid = l.relation WHERE c.relname = 'evaluations' AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
      2>/dev/null || true)" == 1 ]]; then
    lock_granted=true
    break
  fi
  sleep 0.1
done
[[ "$lock_granted" == true ]] || {
  kill "$lock_holder_pid" >/dev/null 2>&1 || true
  wait "$rehearsal_pid" || true
  fail 'test could not acquire the migration-blocking lock'
}
if wait "$rehearsal_pid"; then
  kill "$lock_holder_pid" >/dev/null 2>&1 || true
  wait "$lock_holder_pid" >/dev/null 2>&1 || true
  fail 'lock-timeout rehearsal unexpectedly succeeded'
fi
wait "$lock_holder_pid" >/dev/null 2>&1 || true
[[ -f "$lock_receipt" && "$(file_mode "$lock_receipt")" == 600 ]] ||
  fail 'lock-timeout failure did not write a mode 0600 receipt'
jq --exit-status '
  .status == "failed" and .failed_step == "migration" and
  .lock_timeout_seconds == 1 and
  .checks.clean_target_schema == false and
  .checks.owned_resource_cleanup == true
' "$lock_receipt" >/dev/null || fail 'lock-timeout receipt is incomplete'
[[ -z "$(docker container ls --all --quiet \
  --filter "label=com.xengineer.speakup.production-rehearsal-run-id=$lock_run_id")" ]] ||
  fail 'lock-timeout rehearsal leaked a container'
! docker network inspect "xe3-speakup-prod-rehearsal-$lock_run_id" >/dev/null 2>&1 ||
  fail 'lock-timeout rehearsal leaked its network'
! docker volume inspect "xe3-speakup-prod-rehearsal-$lock_run_id" >/dev/null 2>&1 ||
  fail 'lock-timeout rehearsal leaked its volume'

command -v perl >/dev/null 2>&1 || fail 'perl is required for signal tests'
interruption_pid=''
interruption_candidate=''
interruption_run_id=''

start_interruptible_rehearsal() {
  local selected_receipt=$1 selected_output=$2

  PATH="$wrapper_directory:$PATH" perl -e '
    $SIG{INT} = "DEFAULT";
    $SIG{TERM} = "DEFAULT";
    exec @ARGV or die "exec failed";
  ' "$rehearsal" --execute \
    --backup "$backup_directory" \
    --manifest "$manifest" \
    --previous-server-image "$previous_image" \
    --server-env-file "$server_environment" \
    --receipt "$selected_receipt" \
    --lock-timeout-seconds 2 >"$selected_output" 2>&1 &
  interruption_pid=$!
}

wait_for_candidate_container() {
  wait_for_pid_rehearsal_container "$interruption_pid" candidate || return 1
  interruption_candidate=$selected_background_container
  interruption_run_id=$selected_background_run_id
}

run_signal_cleanup_test() {
  local signal=$1 suffix=$2
  local signal_receipt="$temporary_directory/$suffix-receipt.json"
  local signal_output="$temporary_directory/$suffix.out"
  local signal_run_id

  start_interruptible_rehearsal "$signal_receipt" "$signal_output"
  wait_for_candidate_container || {
    wait "$interruption_pid" >/dev/null 2>&1 || true
    fail "$signal test did not reach one PID-owned candidate"
  }
  signal_run_id=$interruption_run_id
  observed_rehearsal_runs+=("$signal_run_id")
  verify_pid_rehearsal_container "$interruption_pid" candidate \
    "$interruption_candidate" "$signal_run_id" ||
    fail "$signal candidate ownership changed before $signal"
  kill -"$signal" "$interruption_pid"
  if wait "$interruption_pid"; then
    fail "$signal interruption unexpectedly succeeded"
  fi
  verify_failed_receipt "$signal_receipt" candidate_readiness
  assert_no_rehearsal_resources "$signal_run_id"
}

export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$interrupt_fixture_image_id
run_signal_cleanup_test TERM sigterm
run_signal_cleanup_test INT sigint

replacement_receipt="$temporary_directory/replacement-receipt.json"
replacement_output="$temporary_directory/replacement.out"
start_interruptible_rehearsal "$replacement_receipt" "$replacement_output"
wait_for_candidate_container || {
  wait "$interruption_pid" >/dev/null 2>&1 || true
  fail 'replacement test did not reach one PID-owned candidate'
}
replacement_run_id=$interruption_run_id
observed_rehearsal_runs+=("$replacement_run_id")
verify_pid_rehearsal_container "$interruption_pid" candidate \
  "$interruption_candidate" "$replacement_run_id" ||
  fail 'replacement candidate ownership changed before stopping the rehearsal'
kill -STOP "$interruption_pid"
verify_pid_rehearsal_container "$interruption_pid" candidate \
  "$interruption_candidate" "$replacement_run_id" ||
  fail 'replacement candidate ownership changed before queuing termination'
kill -TERM "$interruption_pid"
verify_pid_rehearsal_container "$interruption_pid" candidate \
  "$interruption_candidate" "$replacement_run_id" ||
  fail 'replacement candidate ownership changed before removal'
original_candidate_id=$(docker container inspect --format '{{.Id}}' \
  "$interruption_candidate")
docker container rm --force "$original_candidate_id" >/dev/null
replacement_id=$(docker container create \
  --name "$interruption_candidate" \
  --network none \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --entrypoint /bin/true \
  "$fixture_image_id")
kill -CONT "$interruption_pid"
if wait "$interruption_pid"; then
  fail 'replacement interruption unexpectedly succeeded'
fi
[[ -f "$replacement_receipt" && "$(file_mode "$replacement_receipt")" == 600 ]] ||
  fail 'replacement failure did not write a mode 0600 receipt'
jq --exit-status '
  .status == "failed" and .failed_step == "cleanup" and
  .checks.owned_resource_cleanup == false
' "$replacement_receipt" >/dev/null ||
  fail 'replacement failure receipt did not fail closed'
[[ "$(docker container inspect --format '{{.Id}}' "$replacement_id")" == \
  "$replacement_id" ]] || fail 'cleanup deleted the replacement container'
remove_test_container "$interruption_candidate" ||
  fail 'test could not remove its preserved replacement container'
assert_no_rehearsal_resources "$replacement_run_id"

inspect_error_receipt="$temporary_directory/inspect-error-receipt.json"
inspect_error_output="$temporary_directory/inspect-error.out"
start_interruptible_rehearsal "$inspect_error_receipt" "$inspect_error_output"
wait_for_candidate_container || {
  wait "$interruption_pid" >/dev/null 2>&1 || true
  fail 'inspect-error test did not reach one PID-owned candidate'
}
inspect_error_run_id=$interruption_run_id
observed_rehearsal_runs+=("$inspect_error_run_id")
verify_pid_rehearsal_container "$interruption_pid" candidate \
  "$interruption_candidate" "$inspect_error_run_id" ||
  fail 'inspect-error candidate ownership changed before fault injection'
inspect_error_candidate_inspection=$(docker container inspect \
  "$interruption_candidate")
inspect_error_candidate_id=$(jq --exit-status --raw-output \
  'if length == 1 then .[0].Id else empty end' \
  <<<"$inspect_error_candidate_inspection") ||
  fail 'inspect-error candidate has no immutable ID'
jq --exit-status \
  --arg id "$inspect_error_candidate_id" \
  --arg name "$interruption_candidate" \
  --arg run "$inspect_error_run_id" '
    length == 1 and .[0].Id == $id and .[0].Name == ("/" + $name) and
    .[0].Config.Labels["com.xengineer.speakup.production-rehearsal"] == "true" and
    .[0].Config.Labels["com.xengineer.speakup.production-rehearsal-run-id"] == $run
  ' <<<"$inspect_error_candidate_inspection" >/dev/null ||
  fail 'inspect-error target is not the current PID-owned candidate'
printf '%s\n' "$inspect_error_candidate_id" >"$TEST_INSPECT_ERROR_TARGET_FILE"
chmod 0600 "$TEST_INSPECT_ERROR_TARGET_FILE"
docker container stop --time 2 "$inspect_error_candidate_id" >/dev/null ||
  fail 'inspect-error test could not stop its verified candidate'
if wait "$interruption_pid"; then
  fail 'inspect-error rehearsal unexpectedly succeeded'
else
  inspect_error_status=$?
fi
[[ "$inspect_error_status" == 1 ]] ||
  fail "inspect-error rehearsal returned unexpected status $inspect_error_status"
[[ -f "$TEST_INSPECT_ERROR_INJECTED_FILE" &&
  ! -L "$TEST_INSPECT_ERROR_INJECTED_FILE" &&
  "$(file_mode "$TEST_INSPECT_ERROR_INJECTED_FILE")" == 600 &&
  "$(<"$TEST_INSPECT_ERROR_INJECTED_FILE")" == "$inspect_error_candidate_id" ]] ||
  fail 'inspect-error wrapper did not inject against the verified candidate ID'
grep -Fxq \
  'Production schema rehearsal error: isolated Server image stopped before readiness' \
  "$inspect_error_output" ||
  fail 'inspect-error rehearsal did not report the expected natural readiness failure'
[[ -e "$inspect_error_receipt" || -L "$inspect_error_receipt" ]] ||
  fail 'inspect-error failure receipt is missing after confirmed injection'
[[ -f "$inspect_error_receipt" && ! -L "$inspect_error_receipt" ]] ||
  fail 'inspect-error failure receipt is not a regular file'
inspect_error_receipt_mode=$(file_mode "$inspect_error_receipt") ||
  fail 'inspect-error failure receipt mode cannot be inspected'
[[ "$inspect_error_receipt_mode" == 600 ]] ||
  fail "inspect-error failure receipt mode is $inspect_error_receipt_mode, expected 600"
jq --exit-status '
  .status == "failed" and .failed_step == "cleanup" and
  .checks.owned_resource_cleanup == false
' "$inspect_error_receipt" >/dev/null ||
  fail 'inspect-error cleanup did not fail closed'
[[ "$(docker container inspect --format '{{.Id}}' \
  "$inspect_error_candidate_id")" == "$inspect_error_candidate_id" ]] ||
  fail 'inspect error caused cleanup to delete an unverified container'
rm "$TEST_INSPECT_ERROR_TARGET_FILE"
rm "$TEST_INSPECT_ERROR_INJECTED_FILE"
remove_rehearsal_run "$inspect_error_run_id" ||
  fail 'test could not remove inspect-error resources after verification'
assert_no_rehearsal_resources "$inspect_error_run_id"
[[ "$(docker container inspect --format '{{.Id}}' "$decoy_candidate")" == \
  "$decoy_candidate_id" ]] || fail 'candidate selection modified the concurrent decoy'
[[ "$(docker container inspect --format '{{.Id}}' "$decoy_database")" == \
  "$decoy_database_id" ]] || fail 'database selection modified the concurrent decoy'
remove_test_container "$decoy_candidate" || fail 'cannot remove candidate decoy'
remove_test_container "$decoy_database" || fail 'cannot remove database decoy'
export TEST_CANDIDATE_FIXTURE_IMAGE_ID=$fixture_image_id

printf '%s\n' 'Production schema rehearsal tests passed'
