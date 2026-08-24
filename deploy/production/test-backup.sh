#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly backup_command="$production_directory/xe3-postgres-backup"
readonly backup_service="$production_directory/xe3-postgres-backup.service"
readonly restore_check_service="$production_directory/xe3-postgres-restore-check.service"
readonly backup_timer="$production_directory/xe3-postgres-backup.timer"
readonly backup_environment_example="$production_directory/postgres-backup.env.example"
readonly postgres_image='postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
readonly compose_project='xe3-speakup-production'
readonly compose_service='postgres'
readonly database_network='xe3-speakup-production_database'
readonly source_volume='xe3-speakup-postgres-data'
readonly restore_label='com.xengineer.speakup.postgres-restore-check=true'
readonly database_name='speakup_backup_test'
readonly database_user='speakup_backup_test'
readonly deployment_version='v0.1.1-backup-test'
readonly git_sha='0123456789abcdef0123456789abcdef01234567'

fail() {
  printf 'PostgreSQL backup contract test: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || fail "$1 is required"
}

sha256_file() {
  if command -v sha256sum >/dev/null; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

assert_unit_directive() {
  local unit_file=$1
  local section=$2
  local directive=$3
  local expected=$4
  local actual
  [[ -f "$unit_file" ]] || fail "$unit_file is required"
  actual="$(
    awk \
      -v section="[$section]" \
      -v directive="$directive" '
        /^\[[^]]+\]$/ { current_section = $0; next }
        current_section == section &&
          substr($0, 1, length(directive) + 1) == directive "=" {
            print substr($0, length(directive) + 2)
          }
      ' "$unit_file"
  )"
  [[ "$actual" == "$expected" ]] || \
    fail "$unit_file must set [$section] $directive=$expected exactly once"
}

assert_unit_contracts() {
  local service command state_directories
  for service in "$backup_service" "$restore_check_service"; do
    if [[ "$service" == "$backup_service" ]]; then
      command='backup daily'
      state_directories='speakup/postgres-backups'
    else
      command='check'
      state_directories='speakup/postgres-backups speakup/safety-checks'
    fi
    assert_unit_directive "$service" Service TimeoutStartSec 2h
    assert_unit_directive \
      "$service" Service EnvironmentFile /etc/speakup/postgres-backup.env
    assert_unit_directive "$service" Service StateDirectory "$state_directories"
    assert_unit_directive "$service" Service StateDirectoryMode 0700
    assert_unit_directive "$service" Service UMask 0077
    assert_unit_directive \
      "$service" Service ExecStart \
      "/usr/bin/flock --nonblock /run/lock/xe3-postgres-backup.lock /usr/bin/env POSTGRES_BACKUP_ROOT=/var/lib/speakup/postgres-backups /usr/local/sbin/xe3-postgres-backup $command"
    assert_unit_directive "$service" Service PrivateNetwork true
    assert_unit_directive "$service" Service RestrictAddressFamilies AF_UNIX
  done

  assert_unit_directive \
    "$restore_check_service" Service ExecStartPre \
    '/usr/bin/rm --force -- /var/lib/speakup/safety-checks/postgres-restore-check.success'
  assert_unit_directive \
    "$restore_check_service" Service ExecStartPost \
    '/usr/bin/install --no-target-directory --owner=root --group=root --mode=0600 /dev/null /var/lib/speakup/safety-checks/postgres-restore-check.success'
  ! grep -Fq 'safety-checks' "$backup_service" ||
    fail 'the backup unit must not write the restore-check success marker'

  assert_unit_directive "$backup_timer" Timer OnCalendar daily
  assert_unit_directive "$backup_timer" Timer Persistent true
  assert_unit_directive "$backup_timer" Timer Unit xe3-postgres-backup.service
}

assert_environment_contract() {
  local actual_keys expected_keys
  [[ -f "$backup_environment_example" ]] || \
    fail "$backup_environment_example is required"
  actual_keys="$(
    awk '
      /^[[:space:]]*($|#)/ { next }
      /^[A-Z][A-Z0-9_]*=/ {
        key = $0
        sub(/=.*/, "", key)
        print key
        next
      }
      { print "__INVALID_LINE__" NR }
    ' "$backup_environment_example" | LC_ALL=C sort
  )"
  expected_keys="$(
    printf '%s\n' \
      POSTGRES_BACKUP_DATABASE \
      POSTGRES_BACKUP_DEPLOYMENT_VERSION \
      POSTGRES_BACKUP_GIT_SHA \
      POSTGRES_BACKUP_IMAGE \
      POSTGRES_BACKUP_MAX_AGE_SECONDS \
      POSTGRES_BACKUP_RETENTION_DAYS \
      POSTGRES_BACKUP_SOURCE_VOLUME \
      POSTGRES_BACKUP_USER |
      LC_ALL=C sort
  )"
  [[ "$actual_keys" == "$expected_keys" ]] || \
    fail "$backup_environment_example has unexpected, missing, duplicate, or malformed keys"
}

expect_failure() {
  local name=$1
  shift
  failure_number=$((failure_number + 1))
  local output="$temporary_directory/failure-$failure_number.log"
  if "$@" >"$output" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
  if grep -Fq "$database_password" "$output"; then
    fail "$name exposed the PostgreSQL password"
  fi
}

wait_for_healthy() {
  local container=$1
  local deadline=$((SECONDS + 90))
  local process status
  while ((SECONDS < deadline)); do
    status="$(
      docker container inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
        "$container" 2>/dev/null || true
    )"
    process="$(docker exec "$container" cat /proc/1/comm 2>/dev/null || true)"
    case "$status" in
      healthy)
        if [[ "$process" == postgres ]]; then
          return
        fi
        ;;
      exited | dead)
        docker logs "$container" >&2 || true
        fail "$container stopped before becoming healthy"
        ;;
    esac
    sleep 1
  done
  docker logs "$container" >&2 || true
  fail "$container did not become healthy"
}

start_source() {
  local project=$1
  docker run --detach \
    --name "$source_container" \
    --label "com.docker.compose.project=$project" \
    --label "com.docker.compose.service=$compose_service" \
    --label "com.xengineer.speakup.test-run=$test_id" \
    --network "$database_network" \
    --mount "type=volume,src=$source_volume,dst=/var/lib/postgresql,volume-nocopy" \
    --env "POSTGRES_DB=$database_name" \
    --env "POSTGRES_USER=$database_user" \
    --env "POSTGRES_PASSWORD=$database_password" \
    --health-cmd "pg_isready --username=$database_user --dbname=$database_name" \
    --health-interval 1s \
    --health-timeout 5s \
    --health-retries 30 \
    --health-start-period 5s \
    "$postgres_image" >/dev/null
  wait_for_healthy "$source_container"
}

stop_source() {
  remove_test_container "$source_container"
}

run_backup_command() {
  env \
    PATH="${TEST_COMMAND_PATH:-$PATH}" \
    TEST_REAL_DOCKER="${TEST_REAL_DOCKER:-}" \
    TEST_DOCKER_WRAPPER_MODE="${TEST_DOCKER_WRAPPER_MODE:-}" \
    TEST_DOCKER_EVENT_MARKER="${TEST_DOCKER_EVENT_MARKER:-}" \
    TEST_HISTORICAL_IMAGE_REFERENCE="${TEST_HISTORICAL_IMAGE_REFERENCE:-}" \
    TEST_HISTORICAL_IMAGE_ID="${TEST_HISTORICAL_IMAGE_ID:-}" \
    TEST_SOURCE_CONTAINER="${TEST_SOURCE_CONTAINER:-}" \
    TEST_DATABASE_USER="${TEST_DATABASE_USER:-}" \
    TEST_DATABASE_NAME="${TEST_DATABASE_NAME:-}" \
    POSTGRES_BACKUP_ROOT="$backup_root" \
    POSTGRES_BACKUP_IMAGE="${TEST_POSTGRES_BACKUP_IMAGE:-$postgres_image}" \
    POSTGRES_BACKUP_SOURCE_VOLUME="${TEST_POSTGRES_BACKUP_SOURCE_VOLUME:-$source_volume}" \
    POSTGRES_BACKUP_DATABASE="$database_name" \
    POSTGRES_BACKUP_USER="$database_user" \
    POSTGRES_BACKUP_DEPLOYMENT_VERSION="$deployment_version" \
    POSTGRES_BACKUP_GIT_SHA="$git_sha" \
    POSTGRES_BACKUP_RETENTION_DAYS=7 \
    POSTGRES_BACKUP_MAX_AGE_SECONDS=300 \
    "$backup_command" "$@"
}

partial_backup_entries() {
  local entry
  local -a entries

  shopt -s nullglob dotglob
  entries=("$backup_root"/.partial-*)
  shopt -u nullglob dotglob
  for entry in "${entries[@]}"; do
    printf '%s\n' "${entry##*/}"
  done | LC_ALL=C sort
}

finalized_backup_ids() {
  local entry name
  local -a entries

  shopt -s nullglob dotglob
  entries=("$backup_root"/*)
  shopt -u nullglob dotglob
  for entry in "${entries[@]}"; do
    name=${entry##*/}
    if [[ "$name" =~ ^[0-9]{8}T[0-9]{6}Z-(daily|predeploy)$ ]]; then
      printf '%s\n' "$name"
    fi
  done | LC_ALL=C sort
}

copy_backup_with_identity() {
  local source_directory=$1
  local target_id=$2
  local created_at=$3
  local target_directory="$backup_root/$target_id"
  local backup_type=${target_id##*-}

  [[ ! -e "$target_directory" ]] || fail "test backup $target_id already exists"
  mkdir -m 0700 "$target_directory"
  cp "$source_directory/database.dump" "$target_directory/database.dump"
  cp "$source_directory/database.dump.sha256" \
    "$target_directory/database.dump.sha256"
  jq \
    --arg backup_id "$target_id" \
    --arg backup_type "$backup_type" \
    --arg created_at "$created_at" \
    '.backup_id = $backup_id |
     .backup_type = $backup_type |
     .created_at = $created_at' \
    "$source_directory/metadata.json" >"$target_directory/metadata.json"
  chmod 0600 \
    "$target_directory/database.dump" \
    "$target_directory/database.dump.sha256" \
    "$target_directory/metadata.json"
}

restore_resources() {
  local selected_backup_id=$1
  {
    docker container ls --all --quiet \
      --filter "label=$restore_label" \
      --filter "label=com.xengineer.speakup.postgres-restore-backup-id=$selected_backup_id" \
      | sed 's/^/container:/'
    docker volume ls --quiet \
      --filter "label=$restore_label" \
      --filter "label=com.xengineer.speakup.postgres-restore-backup-id=$selected_backup_id" \
      | sed 's/^/volume:/'
  } | sort
}

assert_no_restore_resources() {
  local selected_backup_id=$1
  local leaked
  leaked="$(restore_resources "$selected_backup_id")"
  [[ -z "$leaked" ]] || fail "restore check leaked Docker resources: $leaked"
}

remove_test_container() {
  local container=$1
  if docker container inspect "$container" >/dev/null 2>&1; then
    docker container inspect "$container" | jq --exit-status \
      --arg name "$container" \
      --arg test_id "$test_id" '
        length == 1 and
        .[0].Name == ("/" + $name) and
        .[0].Config.Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker container rm --force "$container" >/dev/null
  fi
}

remove_test_volume() {
  local volume=$1
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume inspect "$volume" | jq --exit-status \
      --arg name "$volume" \
      --arg test_id "$test_id" '
        length == 1 and
        .[0].Name == $name and
        .[0].Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker volume rm "$volume" >/dev/null
  fi
}

remove_test_database_network() {
  if docker network inspect "$database_network" >/dev/null 2>&1; then
    docker network inspect "$database_network" | jq --exit-status \
      --arg name "$database_network" \
      --arg test_id "$test_id" '
        length == 1 and
        .[0].Name == $name and
        .[0].Internal == true and
        .[0].Labels["com.xengineer.speakup.test-run"] == $test_id
      ' >/dev/null || return 1
    docker network rm "$database_network" >/dev/null
  fi
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  remove_test_container "$source_container" >/dev/null 2>&1 || status=1
  remove_test_container "$ambiguous_container" >/dev/null 2>&1 || status=1
  remove_test_container "$evidence_container" >/dev/null 2>&1 || status=1
  remove_test_volume "$source_volume" >/dev/null 2>&1 || status=1
  remove_test_volume "$wrong_volume" >/dev/null 2>&1 || status=1
  remove_test_volume "$evidence_volume" >/dev/null 2>&1 || status=1
  remove_test_database_network >/dev/null 2>&1 || status=1
  if [[ "$temporary_directory" == */xe3-postgres-backup-test.* ]]; then
    rm -rf -- "$temporary_directory"
  fi
  exit "$status"
}

require_command docker
require_command jq
require_command awk
require_command sed
require_command sort
readonly real_docker="$(command -v docker)"
[[ -x "$backup_command" ]] || fail "$backup_command must exist and be executable"
assert_unit_contracts
assert_environment_contract

temporary_base=${TMPDIR:-/tmp}
temporary_base=${temporary_base%/}
temporary_directory="$(mktemp -d "$temporary_base/xe3-postgres-backup-test.XXXXXX")"
readonly temporary_directory
readonly test_id="$(basename "$temporary_directory")"
readonly backup_root="$temporary_directory/postgres-backups"
readonly source_container="$test_id-source"
readonly ambiguous_container="$test_id-ambiguous"
readonly evidence_container="$test_id-evidence"
readonly wrong_volume="$test_id-wrong"
readonly evidence_volume="$test_id-evidence"
readonly database_password="backup-test-secret-$test_id"
readonly docker_wrapper_directory="$temporary_directory/docker-wrapper"
readonly docker_wrapper="$docker_wrapper_directory/docker"
backup_id=''
postgres_image_id=''
failure_number=0
trap cleanup EXIT INT TERM

mkdir -m 0700 "$backup_root"
mkdir -m 0700 "$docker_wrapper_directory"
cat >"$docker_wrapper" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

if [[ "${TEST_DOCKER_WRAPPER_MODE:-}" == migration-change ]]; then
  for argument in "$@"; do
    if [[ "$argument" == pg_dump ]]; then
      "$TEST_REAL_DOCKER" "$@"
      "$TEST_REAL_DOCKER" container exec --user postgres "$TEST_SOURCE_CONTAINER" \
        psql \
          --no-psqlrc \
          --set ON_ERROR_STOP=1 \
          --username "$TEST_DATABASE_USER" \
          --dbname "$TEST_DATABASE_NAME" \
          --command 'UPDATE public.schema_migrations SET version = version + 1;' \
        >/dev/null
      : >"$TEST_DOCKER_EVENT_MARKER"
      exit 0
    fi
  done
fi

if [[ "${TEST_DOCKER_WRAPPER_MODE:-}" == restore-inspect-failure &&
  "$#" == 3 && "$1" == container && "$2" == inspect ]]; then
  inspect_output="$("$TEST_REAL_DOCKER" "$@")"
  if grep -Fq \
    '"com.xengineer.speakup.postgres-restore-check": "true"' \
    <<<"$inspect_output" && [[ ! -e "$TEST_DOCKER_EVENT_MARKER" ]]; then
    : >"$TEST_DOCKER_EVENT_MARKER"
    exit 75
  fi
  printf '%s\n' "$inspect_output"
  exit 0
fi

if [[ "${TEST_DOCKER_WRAPPER_MODE:-}" == historical-image &&
  "$#" == 5 && "$1" == image && "$2" == inspect &&
  "$3" == --format && "$4" == '{{.Id}}' &&
  "$5" == "$TEST_HISTORICAL_IMAGE_REFERENCE" ]]; then
  "$TEST_REAL_DOCKER" image inspect "$TEST_HISTORICAL_IMAGE_ID" >/dev/null
  printf '%s\n' "$TEST_HISTORICAL_IMAGE_ID"
  : >"$TEST_DOCKER_EVENT_MARKER"
  exit 0
fi

exec "$TEST_REAL_DOCKER" "$@"
EOF
chmod 0700 "$docker_wrapper"

existing_production_containers="$(
  docker container ls --all --quiet \
    --filter "label=com.docker.compose.project=$compose_project" \
    --filter "label=com.docker.compose.service=$compose_service"
)"
[[ -z "$existing_production_containers" ]] || \
  fail 'refusing to run while a Production PostgreSQL container exists'
if docker volume inspect "$source_volume" >/dev/null 2>&1; then
  fail 'refusing to run while the Production PostgreSQL volume exists'
fi
if docker network inspect "$database_network" >/dev/null 2>&1; then
  fail 'refusing to run while the Production database network exists'
fi
for container in "$source_container" "$ambiguous_container" "$evidence_container"; do
  if docker container inspect "$container" >/dev/null 2>&1; then
    fail "refusing to reuse existing test container $container"
  fi
done
for volume in "$wrong_volume" "$evidence_volume"; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    fail "refusing to reuse existing test volume $volume"
  fi
done

docker network create \
  --internal \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$database_network" >/dev/null
docker volume create \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$source_volume" >/dev/null
docker volume create \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$wrong_volume" >/dev/null
docker volume create \
  --label "com.xengineer.speakup.test-run=$test_id" \
  "$evidence_volume" >/dev/null

# A healthy PostgreSQL process with the wrong Compose identity must never be selected.
start_source 'xe3-speakup-not-production'
expect_failure 'wrong Compose project identity' run_backup_command backup daily
stop_source
postgres_image_id="$(docker image inspect --format '{{.Id}}' "$postgres_image")"
[[ "$postgres_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'PostgreSQL test image must have a content-addressed local ID'
readonly postgres_image_id

start_source "$compose_project"

docker container inspect "$source_container" | jq --exit-status \
  --arg project "$compose_project" \
  --arg service "$compose_service" \
  --arg volume "$source_volume" \
  --arg network "$database_network" '
    length == 1 and
    .[0].Config.Labels["com.docker.compose.project"] == $project and
    .[0].Config.Labels["com.docker.compose.service"] == $service and
    .[0].State.Health.Status == "healthy" and
    ([.[0].Mounts[] |
      select(.Type == "volume" and
             .Name == $volume and
             .Destination == "/var/lib/postgresql" and
             .RW == true)] | length) == 1 and
    ((.[0].HostConfig.PortBindings // {}) == {}) and
    (.[0].NetworkSettings.Networks | keys) == [$network]
  ' >/dev/null || fail 'source PostgreSQL does not satisfy the isolated Production identity'

docker exec "$source_container" psql \
  --set ON_ERROR_STOP=1 \
  --username "$database_user" \
  --dbname "$database_name" \
  --command 'CREATE TABLE schema_migrations (version bigint NOT NULL, dirty boolean NOT NULL);' \
  --command 'INSERT INTO schema_migrations (version, dirty) VALUES (7, false);' \
  --command 'CREATE TABLE users (id uuid PRIMARY KEY, canonical_email text NOT NULL);' \
  --command "INSERT INTO users (id, canonical_email) VALUES ('00000000-0000-4000-8000-000000000861', 'backup-e2e@example.com');" \
  >/dev/null

# Interrupted output is never eligible as a finalized backup.
mkdir "$backup_root/.partial-20260822T010203Z-daily-interrupted"
printf '%s\n' '{"format_version":1}' \
  >"$backup_root/.partial-20260822T010203Z-daily-interrupted/metadata.json"
expect_failure 'partial output without a finalized backup' run_backup_command check

docker exec "$source_container" psql \
  --set ON_ERROR_STOP=1 \
  --username "$database_user" \
  --dbname "$database_name" \
  --command 'UPDATE schema_migrations SET dirty = true;' >/dev/null
expect_failure 'dirty source schema' run_backup_command backup daily
docker exec "$source_container" psql \
  --set ON_ERROR_STOP=1 \
  --username "$database_user" \
  --dbname "$database_name" \
  --command 'UPDATE schema_migrations SET dirty = false;' >/dev/null

TEST_POSTGRES_BACKUP_SOURCE_VOLUME="$wrong_volume" \
  expect_failure 'configured source volume mismatch' run_backup_command backup daily

TEST_POSTGRES_BACKUP_IMAGE='postgres:18-bookworm@sha256:0000000000000000000000000000000000000000000000000000000000000001' \
  expect_failure 'configured PostgreSQL image mismatch' run_backup_command backup daily

docker run --detach \
  --name "$ambiguous_container" \
  --label "com.docker.compose.project=$compose_project" \
  --label "com.docker.compose.service=$compose_service" \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --network "$database_network" \
  --entrypoint sleep \
  "$postgres_image" 300 >/dev/null
expect_failure 'ambiguous Production PostgreSQL identity' run_backup_command backup daily
remove_test_container "$ambiguous_container" || \
  fail 'refusing to remove an ambiguous container not owned by this test run'

expect_failure 'unsupported backup type' run_backup_command backup weekly

# A migration that changes after pg_dump starts must fail and remove only the
# partial directory created by that run.
partial_entries_before="$(partial_backup_entries)"
migration_marker="$temporary_directory/migration-change.marker"
TEST_COMMAND_PATH="$docker_wrapper_directory:$PATH"
TEST_REAL_DOCKER="$real_docker"
TEST_DOCKER_WRAPPER_MODE='migration-change'
TEST_DOCKER_EVENT_MARKER="$migration_marker"
TEST_SOURCE_CONTAINER="$source_container"
TEST_DATABASE_USER="$database_user"
TEST_DATABASE_NAME="$database_name"
expect_failure 'migration version changed during pg_dump' run_backup_command backup daily
unset \
  TEST_COMMAND_PATH \
  TEST_REAL_DOCKER \
  TEST_DOCKER_WRAPPER_MODE \
  TEST_DOCKER_EVENT_MARKER \
  TEST_SOURCE_CONTAINER \
  TEST_DATABASE_USER \
  TEST_DATABASE_NAME
[[ -f "$migration_marker" ]] || fail 'migration change injection did not run'
grep -Fq 'migration state changed during pg_dump' \
  "$temporary_directory/failure-$failure_number.log" || \
  fail 'migration change did not fail at the post-dump schema check'
partial_entries_after="$(partial_backup_entries)"
[[ "$partial_entries_after" == "$partial_entries_before" ]] || \
  fail 'failed pg_dump run left a new partial backup behind'
docker exec "$source_container" psql \
  --set ON_ERROR_STOP=1 \
  --username "$database_user" \
  --dbname "$database_name" \
  --command 'UPDATE schema_migrations SET version = 7;' >/dev/null

backup_output="$(run_backup_command backup daily)"
backup_id="$(
  printf '%s\n' "$backup_output" |
    sed -nE 's/^backup_id=([0-9]{8}T[0-9]{6}Z-daily) restore=verified$/\1/p'
)"
[[ -n "$backup_id" ]] || fail 'backup did not return the canonical daily backup ID'
[[ "$backup_output" == "backup_id=$backup_id restore=verified" ]] || \
  fail 'backup emitted an unexpected success contract'
assert_no_restore_resources "$backup_id"

readonly finalized_directory="$backup_root/$backup_id"
readonly dump_file="$finalized_directory/database.dump"
readonly checksum_file="$finalized_directory/database.dump.sha256"
readonly metadata_file="$finalized_directory/metadata.json"
[[ -f "$dump_file" && -f "$checksum_file" && -f "$metadata_file" ]] || \
  fail 'finalized backup is incomplete'
[[ "$(dd if="$dump_file" bs=5 count=1 2>/dev/null)" == PGDMP ]] || \
  fail 'database.dump is not PostgreSQL custom format'

readonly dump_sha256="$(sha256_file "$dump_file")"
readonly dump_size="$(wc -c <"$dump_file" | tr -d '[:space:]')"
[[ "$(<"$checksum_file")" == "$dump_sha256  database.dump" ]] || \
  fail 'database.dump.sha256 is not canonical'
jq --exit-status \
  --arg backup_id "$backup_id" \
  --arg deployment_version "$deployment_version" \
  --arg git_sha "$git_sha" \
  --arg project "$compose_project" \
  --arg service "$compose_service" \
  --arg source_volume "$source_volume" \
  --arg postgres_image "$postgres_image" \
  --arg database_name "$database_name" \
  --arg database_user "$database_user" \
  --arg sha256 "$dump_sha256" \
  --argjson size_bytes "$dump_size" '
    (keys | sort) == ([
      "backup_id", "backup_type", "created_at", "database_name",
      "database_user", "deployment_version", "format_version", "git_sha",
      "postgres_image", "postgres_version", "schema_dirty", "schema_version",
      "sha256", "size_bytes", "source_compose_project",
      "source_compose_service", "source_volume"
    ] | sort) and
    .format_version == 1 and
    .backup_id == $backup_id and
    (.created_at | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
    .backup_type == "daily" and
    .deployment_version == $deployment_version and
    .git_sha == $git_sha and
    .source_compose_project == $project and
    .source_compose_service == $service and
    .source_volume == $source_volume and
    .postgres_image == $postgres_image and
    (.postgres_version | type == "string" and startswith("18.")) and
    .database_name == $database_name and
    .database_user == $database_user and
    .schema_version == 7 and
    .schema_dirty == false and
    .size_bytes == $size_bytes and
    .sha256 == $sha256
  ' "$metadata_file" >/dev/null || fail 'metadata.json violates the canonical backup contract'

# Historical backups keep the image they were created with. The wrapper maps
# the safe historical digest reference to the locally present test image ID and
# records that check used metadata rather than the current config.
readonly historical_image='postgres:18-bookworm@sha256:1111111111111111111111111111111111111111111111111111111111111111'
[[ "$historical_image" != "$postgres_image" ]] || \
  fail 'historical PostgreSQL image fixture must differ from the current digest'
cp "$metadata_file" "$temporary_directory/current-metadata.json"
jq --arg postgres_image "$historical_image" \
  '.postgres_image = $postgres_image' \
  "$temporary_directory/current-metadata.json" >"$metadata_file"
historical_image_marker="$temporary_directory/historical-image.marker"
TEST_COMMAND_PATH="$docker_wrapper_directory:$PATH"
TEST_REAL_DOCKER="$real_docker"
TEST_DOCKER_WRAPPER_MODE='historical-image'
TEST_DOCKER_EVENT_MARKER="$historical_image_marker"
TEST_HISTORICAL_IMAGE_REFERENCE="$historical_image"
TEST_HISTORICAL_IMAGE_ID="$postgres_image_id"
historical_check_output="$(run_backup_command check "$backup_id")"
unset \
  TEST_COMMAND_PATH \
  TEST_REAL_DOCKER \
  TEST_DOCKER_WRAPPER_MODE \
  TEST_DOCKER_EVENT_MARKER \
  TEST_HISTORICAL_IMAGE_REFERENCE \
  TEST_HISTORICAL_IMAGE_ID
[[ "$historical_check_output" == "backup_id=$backup_id restore=verified" ]] || \
  fail 'historical image restore check did not succeed'
[[ -f "$historical_image_marker" ]] || \
  fail 'restore check did not inspect the PostgreSQL image recorded in metadata'
assert_no_restore_resources "$backup_id"
cp "$temporary_directory/current-metadata.json" "$metadata_file"

# A Docker inspect failure during restore cleanup must make the whole check
# fail. The one-shot injection allows the EXIT trap to remove the exact
# randomly owned resources on its second attempt.
inspect_failure_marker="$temporary_directory/restore-inspect-failure.marker"
TEST_COMMAND_PATH="$docker_wrapper_directory:$PATH"
TEST_REAL_DOCKER="$real_docker"
TEST_DOCKER_WRAPPER_MODE='restore-inspect-failure'
TEST_DOCKER_EVENT_MARKER="$inspect_failure_marker"
expect_failure 'restore cleanup inspect failure' run_backup_command check "$backup_id"
unset \
  TEST_COMMAND_PATH \
  TEST_REAL_DOCKER \
  TEST_DOCKER_WRAPPER_MODE \
  TEST_DOCKER_EVENT_MARKER
[[ -f "$inspect_failure_marker" ]] || \
  fail 'restore cleanup inspect failure injection did not run'
if grep -Fq 'restore=verified' "$temporary_directory/failure-$failure_number.log"; then
  fail 'restore cleanup inspect failure reported success'
fi
assert_no_restore_resources "$backup_id"

# Retention accepts a valid backup made by an older PostgreSQL image and
# removes its three contract files plus the now-empty directory.
readonly expired_exact_id='20000101T000000Z-daily'
readonly expired_exact_directory="$backup_root/$expired_exact_id"
copy_backup_with_identity \
  "$finalized_directory" \
  "$expired_exact_id" \
  '2000-01-01T00:00:00Z'
jq --arg postgres_image "$historical_image" \
  '.postgres_image = $postgres_image' \
  "$expired_exact_directory/metadata.json" \
  >"$temporary_directory/expired-exact-metadata.json"
mv "$temporary_directory/expired-exact-metadata.json" \
  "$expired_exact_directory/metadata.json"
chmod 0600 "$expired_exact_directory/metadata.json"
retention_output="$(run_backup_command backup predeploy)"
retention_backup_id="$(
  printf '%s\n' "$retention_output" |
    sed -nE 's/^backup_id=([0-9]{8}T[0-9]{6}Z-predeploy) restore=verified$/\1/p'
)"
[[ -n "$retention_backup_id" ]] || \
  fail 'retention trigger backup did not return a canonical backup ID'
[[ ! -e "$expired_exact_directory" ]] || \
  fail 'expired exact three-file backup was not pruned'

# An extra entry makes an otherwise valid expired backup ineligible for any
# deletion. The directory and nested sentinel must remain byte-for-byte.
readonly expired_extra_id='20000102T000000Z-daily'
readonly expired_extra_directory="$backup_root/$expired_extra_id"
copy_backup_with_identity \
  "$finalized_directory" \
  "$expired_extra_id" \
  '2000-01-02T00:00:00Z'
mkdir -m 0700 "$expired_extra_directory/unexpected"
printf '%s\n' 'retention-must-not-delete' \
  >"$expired_extra_directory/unexpected/sentinel.txt"
partial_entries_before="$(partial_backup_entries)"
sleep 1
expect_failure 'retention backup with an extra entry' run_backup_command backup daily
if grep -Fq 'restore=verified' "$temporary_directory/failure-$failure_number.log"; then
  fail 'retention failure with an extra entry reported success'
fi
[[ -d "$expired_extra_directory" &&
  -f "$expired_extra_directory/database.dump" &&
  -f "$expired_extra_directory/database.dump.sha256" &&
  -f "$expired_extra_directory/metadata.json" &&
  "$(<"$expired_extra_directory/unexpected/sentinel.txt")" == \
    'retention-must-not-delete' ]] || \
  fail 'retention modified or deleted a backup containing an extra entry'
partial_entries_after="$(partial_backup_entries)"
[[ "$partial_entries_after" == "$partial_entries_before" ]] || \
  fail 'failed retention run left a new partial backup behind'
rm "$expired_extra_directory/unexpected/sentinel.txt"
rmdir "$expired_extra_directory/unexpected"
rm \
  "$expired_extra_directory/database.dump" \
  "$expired_extra_directory/database.dump.sha256" \
  "$expired_extra_directory/metadata.json"
rmdir "$expired_extra_directory"

latest_finalized_backup_id="$(
  finalized_backup_ids | awk 'NF { latest = $0 } END { print latest }'
)"
[[ -n "$latest_finalized_backup_id" ]] || fail 'no finalized backup remains'
# From this point on, the backup is the only available copy of the test data.
stop_source
remove_test_volume "$source_volume" || \
  fail 'refusing to remove a source volume not owned by this test run'

# Independently restore the artifact and query the unique business row. This is
# test-owned evidence and does not depend on the implementation's check output.
docker run --detach \
  --name "$evidence_container" \
  --label "com.xengineer.speakup.test-run=$test_id" \
  --network none \
  --mount "type=volume,src=$evidence_volume,dst=/var/lib/postgresql,volume-nocopy" \
  --env "POSTGRES_DB=$database_name" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --health-cmd "pg_isready --username=$database_user --dbname=$database_name" \
  --health-interval 1s \
  --health-timeout 5s \
  --health-retries 30 \
  --health-start-period 5s \
  "$postgres_image" >/dev/null
wait_for_healthy "$evidence_container"
docker cp "$dump_file" "$evidence_container:/tmp/database.dump"
docker exec "$evidence_container" pg_restore \
  --single-transaction \
  --exit-on-error \
  --username "$database_user" \
  --dbname "$database_name" \
  /tmp/database.dump >/dev/null
restored_user="$(
  docker exec "$evidence_container" psql \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --username "$database_user" \
    --dbname "$database_name" \
    --command "SELECT id::text || '|' || canonical_email FROM users WHERE id = '00000000-0000-4000-8000-000000000861';"
)"
[[ "$restored_user" == '00000000-0000-4000-8000-000000000861|backup-e2e@example.com' ]] || \
  fail 'independent restore did not recover the seeded user'
restored_schema="$(
  docker exec "$evidence_container" psql \
    --tuples-only \
    --no-align \
    --set ON_ERROR_STOP=1 \
    --username "$database_user" \
    --dbname "$database_name" \
    --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;"
)"
[[ "$restored_schema" == '7|false' ]] || \
  fail 'independent restore did not recover the clean schema state'
remove_test_container "$evidence_container" || \
  fail 'refusing to remove an evidence container not owned by this test run'
remove_test_volume "$evidence_volume" || \
  fail 'refusing to remove an evidence volume not owned by this test run'

check_output="$(run_backup_command check "$backup_id")"
[[ "$check_output" == "backup_id=$backup_id restore=verified" ]] || \
  fail 'explicit backup restore check did not succeed'
assert_no_restore_resources "$backup_id"

check_output="$(run_backup_command check)"
[[ "$check_output" == \
  "backup_id=$latest_finalized_backup_id restore=verified" ]] || \
  fail 'latest backup restore check did not select the newest finalized backup'
assert_no_restore_resources "$latest_finalized_backup_id"

cp "$checksum_file" "$temporary_directory/original.sha256"
printf '%064d  database.dump\n' 0 >"$checksum_file"
expect_failure 'tampered checksum' run_backup_command check "$backup_id"
assert_no_restore_resources "$backup_id"
cp "$temporary_directory/original.sha256" "$checksum_file"

cp "$metadata_file" "$temporary_directory/original-metadata.json"
jq '.created_at = "2000-01-01T00:00:00Z"' "$metadata_file" \
  >"$temporary_directory/stale-metadata.json"
mv "$temporary_directory/stale-metadata.json" "$metadata_file"
expect_failure 'stale finalized backup' run_backup_command check "$backup_id"
assert_no_restore_resources "$backup_id"
cp "$temporary_directory/original-metadata.json" "$metadata_file"

# Force a failure after isolated restoration, then prove the temporary resources are gone.
jq '.schema_version = 8' "$metadata_file" >"$temporary_directory/wrong-schema-metadata.json"
mv "$temporary_directory/wrong-schema-metadata.json" "$metadata_file"
expect_failure 'restored schema version mismatch' run_backup_command check "$backup_id"
assert_no_restore_resources "$backup_id"
cp "$temporary_directory/original-metadata.json" "$metadata_file"

# A corrupt payload with internally consistent metadata must reach pg_restore and fail closed.
cp "$dump_file" "$temporary_directory/original.dump"
printf '%s\n' 'PGDMP-invalid-test-payload' >"$dump_file"
corrupt_sha256="$(sha256_file "$dump_file")"
corrupt_size="$(wc -c <"$dump_file" | tr -d '[:space:]')"
printf '%s  database.dump\n' "$corrupt_sha256" >"$checksum_file"
jq \
  --arg sha256 "$corrupt_sha256" \
  --argjson size_bytes "$corrupt_size" \
  '.sha256 = $sha256 | .size_bytes = $size_bytes' \
  "$temporary_directory/original-metadata.json" >"$metadata_file"
expect_failure 'corrupt custom-format payload' run_backup_command check "$backup_id"
assert_no_restore_resources "$backup_id"

cp "$temporary_directory/original.dump" "$dump_file"
cp "$temporary_directory/original.sha256" "$checksum_file"
cp "$temporary_directory/original-metadata.json" "$metadata_file"
check_output="$(run_backup_command check "$backup_id")"
[[ "$check_output" == "backup_id=$backup_id restore=verified" ]] || \
  fail 'valid backup no longer restores after fail-closed checks'
assert_no_restore_resources "$backup_id"

printf '%s\n' 'PostgreSQL backup contract tests passed'
