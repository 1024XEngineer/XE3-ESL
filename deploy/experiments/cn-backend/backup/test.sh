#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly backup_command="$directory/xe3-speakup-cn-experiment-postgres-backup"
readonly backup_service="$directory/xe3-speakup-cn-experiment-postgres-backup.service"
readonly backup_timer="$directory/xe3-speakup-cn-experiment-postgres-backup.timer"
readonly restore_service="$directory/xe3-speakup-cn-experiment-postgres-restore-check.service"
readonly restore_timer="$directory/xe3-speakup-cn-experiment-postgres-restore-check.timer"
readonly postgres_image="postgres@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108"
readonly postgres_image_id="sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108"
readonly test_id="$(printf '%08x%08x' "$$" "$RANDOM")"
readonly compose_project="xe3-speakup-cn-backup-test-$test_id"
readonly compose_service="postgres"
readonly database_network="${compose_project}_database"
readonly source_volume="${compose_project}_postgres_data"
readonly database_name="speakup_cn_backup_$test_id"
readonly database_user="$database_name"
readonly database_password="cn-backup-test-only-0123456789"
readonly identity_label_key="com.xengineer.speakup.cn-experiment-identity"
readonly identity_label="$identity_label_key=$test_id"
readonly test_label="com.xengineer.speakup.cn-backup-test=$test_id"
readonly source_container="${compose_project}-postgres"
readonly temporary_directory="$(mktemp -d)"
readonly backup_root="$temporary_directory/$compose_project/postgres-backups"
readonly docker_wrapper_directory="$temporary_directory/docker-wrapper"
readonly docker_wrapper_state="$temporary_directory/docker-wrapper-armed"
readonly real_docker="$(command -v docker)"

failure_number=0

fail() {
  printf 'China experiment backup contract test: %s\n' "$*" >&2
  exit 1
}

remove_owned_container() {
  local name=$1
  if ! docker container inspect "$name" >/dev/null 2>&1; then
    return 0
  fi
  docker container inspect "$name" | jq --exit-status --arg label "$test_label" '
    length == 1 and
    ((.[0].Config.Labels | to_entries | map(.key + "=" + .value)) | index($label)) != null
  ' >/dev/null || return 1
  docker container rm --force "$name" >/dev/null
}

remove_owned_volume() {
  local name=$1
  if ! docker volume inspect "$name" >/dev/null 2>&1; then
    return 0
  fi
  docker volume inspect "$name" | jq --exit-status --arg label "$test_label" '
    length == 1 and
    ((.[0].Labels | to_entries | map(.key + "=" + .value)) | index($label)) != null
  ' >/dev/null || return 1
  docker volume rm "$name" >/dev/null
}

remove_owned_network() {
  local name=$1
  if ! docker network inspect "$name" >/dev/null 2>&1; then
    return 0
  fi
  docker network inspect "$name" | jq --exit-status --arg label "$test_label" '
    length == 1 and
    ((.[0].Labels | to_entries | map(.key + "=" + .value)) | index($label)) != null
  ' >/dev/null || return 1
  docker network rm "$name" >/dev/null
}

cleanup() {
  local status=$?
  trap - EXIT
  set +e
  remove_owned_container "$source_container" || status=1
  remove_owned_volume "$source_volume" || status=1
  remove_owned_network "$database_network" || status=1
  rm -rf -- "$temporary_directory"
  exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT HUP TERM

assert_unit_directive() {
  local file=$1 section=$2 directive=$3 expected=$4 actual
  actual="$(awk -v section="[$section]" -v directive="$directive" '
    /^\[[^]]+\]$/ { current = $0; next }
    current == section && substr($0, 1, length(directive) + 1) == directive "=" {
      print substr($0, length(directive) + 2)
    }
  ' "$file")"
  [[ "$actual" == "$expected" ]] ||
    fail "$file must set [$section] $directive=$expected exactly once"
}

assert_unit_contracts() {
  local service
  for service in "$backup_service" "$restore_service"; do
    assert_unit_directive "$service" Service TimeoutStartSec 1h
    assert_unit_directive "$service" Service StateDirectoryMode 0700
    assert_unit_directive \
      "$service" Service RuntimeDirectory \
      xe3-speakup-cn-experiment-postgres-backup
    assert_unit_directive "$service" Service RuntimeDirectoryMode 0700
    assert_unit_directive \
      "$service" Service Environment \
      DOCKER_CONFIG=/run/xe3-speakup-cn-experiment-postgres-backup
    assert_unit_directive "$service" Service UMask 0077
    assert_unit_directive "$service" Service PrivateNetwork true
    assert_unit_directive "$service" Service RestrictAddressFamilies AF_UNIX
    assert_unit_directive "$service" Service ProtectSystem strict
    assert_unit_directive "$service" Service NoNewPrivileges true
  done
  assert_unit_directive \
    "$backup_service" Service StateDirectory \
    speakup-cn-experiment/postgres-backups
  assert_unit_directive \
    "$backup_service" Service ExecStart \
    '/usr/bin/flock --wait 900 /run/lock/xe3-speakup-cn-experiment-postgres-backup.lock /usr/bin/env EXPERIMENT_POSTGRES_BACKUP_ROOT=/var/lib/speakup-cn-experiment/postgres-backups /usr/local/sbin/xe3-speakup-cn-experiment-postgres-backup backup'
  assert_unit_directive \
    "$restore_service" Service StateDirectory \
    'speakup-cn-experiment/postgres-backups speakup-cn-experiment/backup-checks'
  assert_unit_directive \
    "$restore_service" Service ExecStart \
    '/usr/bin/flock --wait 900 /run/lock/xe3-speakup-cn-experiment-postgres-backup.lock /usr/bin/env EXPERIMENT_POSTGRES_BACKUP_ROOT=/var/lib/speakup-cn-experiment/postgres-backups /usr/local/sbin/xe3-speakup-cn-experiment-postgres-backup restore-check'
  assert_unit_directive \
    "$restore_service" Unit Requires \
    'docker.service xe3-speakup-cn-experiment-postgres-backup.service'
  assert_unit_directive \
    "$restore_service" Unit After \
    'docker.service xe3-speakup-cn-experiment-postgres-backup.service'
  assert_unit_directive \
    "$restore_service" Service ExecStartPre \
    '/usr/bin/rm --force -- /var/lib/speakup-cn-experiment/backup-checks/latest-restore.success'
  assert_unit_directive \
    "$restore_service" Service ExecStartPost \
    '/usr/bin/install --no-target-directory --owner=root --group=root --mode=0600 /dev/null /var/lib/speakup-cn-experiment/backup-checks/latest-restore.success'
  assert_unit_directive "$backup_timer" Timer OnCalendar '*-*-* 18:30:00 UTC'
  assert_unit_directive "$backup_timer" Timer RandomizedDelaySec 15m
  assert_unit_directive "$backup_timer" Timer Persistent true
  assert_unit_directive \
    "$backup_timer" Timer Unit xe3-speakup-cn-experiment-postgres-backup.service
  assert_unit_directive "$restore_timer" Timer OnCalendar 'Sun *-*-* 19:30:00 UTC'
  assert_unit_directive "$restore_timer" Timer RandomizedDelaySec 30m
  assert_unit_directive "$restore_timer" Timer Persistent true
  assert_unit_directive \
    "$restore_timer" Timer Unit xe3-speakup-cn-experiment-postgres-restore-check.service
}

wait_for_source() {
  local deadline=$((SECONDS + 90)) state process
  while ((SECONDS < deadline)); do
    state="$(docker container inspect --format \
      '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
      "$source_container" 2>/dev/null || true)"
    process="$(docker container exec "$source_container" cat /proc/1/comm 2>/dev/null || true)"
    [[ "$state" == "healthy" && "$process" == "postgres" ]] && return 0
    [[ "$state" == "exited" || "$state" == "dead" ]] && fail 'source PostgreSQL exited early'
    sleep 1
  done
  fail 'source PostgreSQL did not become healthy'
}

run_backup_command() {
  EXPERIMENT_POSTGRES_BACKUP_TEST_MODE=1 \
    EXPERIMENT_POSTGRES_BACKUP_TEST_ID="$test_id" \
    EXPERIMENT_POSTGRES_BACKUP_ROOT="$backup_root" \
    "$backup_command" "$@"
}

run_backup_command_with_docker_wrapper() {
  PATH="$docker_wrapper_directory:$PATH" \
    CN_BACKUP_TEST_REAL_DOCKER="$real_docker" \
    CN_BACKUP_TEST_WRAPPER_STATE="$docker_wrapper_state" \
    CN_BACKUP_TEST_COMPOSE_PROJECT="$compose_project" \
    EXPERIMENT_POSTGRES_BACKUP_TEST_MODE=1 \
    EXPERIMENT_POSTGRES_BACKUP_TEST_ID="$test_id" \
    EXPERIMENT_POSTGRES_BACKUP_ROOT="$backup_root" \
    "$backup_command" "$@"
}

expect_failure() {
  local description=$1
  shift
  failure_number=$((failure_number + 1))
  local output="$temporary_directory/failure-$failure_number.log"
  if "$@" >"$output" 2>&1; then
    fail "$description unexpectedly succeeded"
  fi
  ! grep -Fq "$database_password" "$output" || fail "$description exposed the database password"
}

assert_no_restore_resources() {
  [[ -z "$(docker container ls --all --quiet --filter "label=$identity_label_key=$compose_project")" ]] ||
    fail 'restore-check left a temporary container'
  [[ -z "$(docker volume ls --quiet --filter "label=$identity_label_key=$compose_project")" ]] ||
    fail 'restore-check left a temporary volume'
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

for command_name in docker jq awk cmp date stat sync; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

bash -n "$backup_command"
assert_unit_contracts
grep -Fq 'readonly retention_days=7' "$backup_command" || fail 'retention must remain fixed at seven days'
grep -Fq -- '--network none' "$backup_command" || fail 'restore-check must remain network-isolated'
grep -Fq 'readonly production_source_volume="xe3-speakup-cn-experiment_postgres_data"' "$backup_command" ||
  fail 'production source volume identity must remain fixed'
grep -Fq "readonly postgres_image_id=\"$postgres_image_id\"" "$backup_command" ||
  fail 'PostgreSQL image ID must remain immutable'
grep -Fq 'EXPERIMENT_POSTGRES_BACKUP_TEST_MODE' "$backup_command" ||
  fail 'test identity override must require explicit test mode'

existing_containers="$(docker container ls --all --quiet \
  --filter "label=com.docker.compose.project=$compose_project" \
  --filter "label=com.docker.compose.service=$compose_service")"
[[ -z "$existing_containers" ]] || fail 'refusing to run beside an existing experiment PostgreSQL container'
! docker volume inspect "$source_volume" >/dev/null 2>&1 || fail 'source test volume already exists'
! docker network inspect "$database_network" >/dev/null 2>&1 || fail 'database test network already exists'
[[ "$(docker image inspect --format '{{.Id}}' "$postgres_image")" == "$postgres_image_id" ]] ||
  fail 'audited PostgreSQL image is not available locally'

mkdir -p "$backup_root"
chmod 700 "$temporary_directory/$compose_project" "$backup_root"
mkdir -m 700 "$docker_wrapper_directory"
cat >"$docker_wrapper_directory/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

readonly real_docker="${CN_BACKUP_TEST_REAL_DOCKER:?}"
readonly state="${CN_BACKUP_TEST_WRAPPER_STATE:?}"
readonly project="${CN_BACKUP_TEST_COMPOSE_PROJECT:?}"

if [[ "${1:-}" == "container" && "${2:-}" == "inspect" &&
  -f "$state" && ! -e "$state.failed" ]]; then
  mv "$state" "$state.failed"
  printf 'simulated transient container inspect failure\n' >&2
  exit 1
fi

if [[ "${1:-}" == "container" && "${2:-}" == "run" ]]; then
  name=""
  previous=""
  for argument in "$@"; do
    if [[ "$previous" == "--name" ]]; then
      name=$argument
      break
    fi
    previous=$argument
  done
  "$real_docker" "$@"
  status=$?
  if [[ $status == 0 && "$name" == "$project-restore-"* ]]; then
    : >"$state"
  fi
  exit "$status"
fi

exec "$real_docker" "$@"
EOF
chmod 700 "$docker_wrapper_directory/docker"
expect_failure 'test root override without explicit test mode' \
  env EXPERIMENT_POSTGRES_BACKUP_ROOT="$backup_root" "$backup_command" verify
expect_failure 'test mode without a unique test ID' \
  env EXPERIMENT_POSTGRES_BACKUP_TEST_MODE=1 \
  EXPERIMENT_POSTGRES_BACKUP_ROOT="$backup_root" "$backup_command" verify
expect_failure 'test mode with an invalid test ID' \
  env EXPERIMENT_POSTGRES_BACKUP_TEST_MODE=1 \
  EXPERIMENT_POSTGRES_BACKUP_TEST_ID='not-unique' \
  EXPERIMENT_POSTGRES_BACKUP_ROOT="$backup_root" "$backup_command" verify
docker network create --internal --label "$test_label" "$database_network" >/dev/null
docker volume create --label "$test_label" "$source_volume" >/dev/null
docker container run --detach \
  --name "$source_container" \
  --label "$test_label" \
  --label "$identity_label" \
  --label "com.docker.compose.project=$compose_project" \
  --label "com.docker.compose.service=$compose_service" \
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
wait_for_source

docker container exec "$source_container" psql \
  --set ON_ERROR_STOP=1 \
  --username "$database_user" \
  --dbname "$database_name" \
  --command 'CREATE TABLE public.schema_migrations (version bigint NOT NULL, dirty boolean NOT NULL);' \
  --command 'INSERT INTO public.schema_migrations (version, dirty) VALUES (9, false);' \
  --command 'CREATE TABLE public.users (id bigint PRIMARY KEY, canonical_email text NOT NULL);' \
  --command "INSERT INTO public.users (id, canonical_email) VALUES (1, 'backup-test@example.com');" \
  >/dev/null

backup_output="$(run_backup_command backup 2>&1)"
! grep -Fq "$database_password" <<<"$backup_output" || fail 'backup exposed the database password'
[[ "$backup_output" =~ ^backup_id=([0-9]{8}T[0-9]{6}Z-daily)\ checksum=verified$ ]] ||
  fail 'backup did not return verified evidence'
first_backup_id=${BASH_REMATCH[1]}
first_backup="$backup_root/$first_backup_id"

jq --exit-status \
  --arg backup_id "$first_backup_id" \
  --arg image "$postgres_image_id" '
    .backup_id == $backup_id and
    .postgres_image_id == $image and
    .schema_version == 9 and
    .schema_dirty == false and
    .user_count == 1 and
    .retention_days == 7
  ' "$first_backup/metadata.json" >/dev/null || fail 'backup metadata evidence is incomplete'

verify_output="$(run_backup_command verify 2>&1)"
[[ "$verify_output" == "backup_id=$first_backup_id checksum=verified" ]] ||
  fail 'latest backup verification failed'

restore_output="$(run_backup_command restore-check 2>&1)"
[[ "$restore_output" == "backup_id=$first_backup_id restore=verified" ]] ||
  fail 'isolated restore-check failed'
assert_no_restore_resources
docker volume inspect "$source_volume" >/dev/null || fail 'restore-check removed the source volume'

old_backup_id='20000101T000000Z-daily'
old_backup="$backup_root/$old_backup_id"
cp -R "$first_backup" "$old_backup"
chmod 700 "$old_backup"
chmod 600 "$old_backup"/*
jq --arg id "$old_backup_id" --arg created_at '2000-01-01T00:00:00Z' \
  '.backup_id = $id | .created_at = $created_at' \
  "$old_backup/metadata.json" >"$temporary_directory/old-metadata.json"
mv "$temporary_directory/old-metadata.json" "$old_backup/metadata.json"
chmod 600 "$old_backup/metadata.json"

sleep 1
second_output="$(run_backup_command backup 2>&1)"
[[ "$second_output" =~ ^backup_id=([0-9]{8}T[0-9]{6}Z-daily)\ checksum=verified$ ]] ||
  fail 'second backup did not return verified evidence'
second_backup_id=${BASH_REMATCH[1]}
[[ "$second_backup_id" != "$first_backup_id" ]] || fail 'backup IDs must be unique'
[[ ! -e "$old_backup" ]] || fail 'expired backup was not pruned'
[[ -d "$first_backup" && -d "$backup_root/$second_backup_id" ]] ||
  fail 'retention pruned a non-expired backup'

second_dump="$backup_root/$second_backup_id/database.dump"
cp "$second_dump" "$temporary_directory/original.dump"
printf '%s\n' 'tamper' >>"$second_dump"
expect_failure 'tampered latest backup' run_backup_command verify "$second_backup_id"
cp "$temporary_directory/original.dump" "$second_dump"
chmod 600 "$second_dump"
verify_output="$(run_backup_command verify "$second_backup_id")"
[[ "$verify_output" == "backup_id=$second_backup_id checksum=verified" ]] ||
  fail 'valid backup did not recover after tamper test'

second_checksum="$backup_root/$second_backup_id/database.dump.sha256"
second_metadata="$backup_root/$second_backup_id/metadata.json"
cp "$second_dump" "$temporary_directory/restore-original.dump"
cp "$second_checksum" "$temporary_directory/restore-original.sha256"
cp "$second_metadata" "$temporary_directory/restore-original-metadata.json"
printf '%s\n' 'PGDMP-invalid-restore-test-payload' >"$second_dump"
corrupt_sha="$(sha256_file "$second_dump")"
corrupt_size="$(wc -c <"$second_dump" | tr -d '[:space:]')"
printf '%s  database.dump\n' "$corrupt_sha" >"$second_checksum"
jq --arg sha256 "$corrupt_sha" --argjson size_bytes "$corrupt_size" \
  '.sha256 = $sha256 | .size_bytes = $size_bytes' \
  "$temporary_directory/restore-original-metadata.json" >"$second_metadata"
chmod 600 "$second_dump" "$second_checksum" "$second_metadata"
expect_failure 'forced isolated restore failure' \
  run_backup_command restore-check "$second_backup_id"
assert_no_restore_resources
cp "$temporary_directory/restore-original.dump" "$second_dump"
cp "$temporary_directory/restore-original.sha256" "$second_checksum"
cp "$temporary_directory/restore-original-metadata.json" "$second_metadata"
chmod 600 "$second_dump" "$second_checksum" "$second_metadata"
verify_output="$(run_backup_command verify "$second_backup_id")"
[[ "$verify_output" == "backup_id=$second_backup_id checksum=verified" ]] ||
  fail 'valid backup did not recover after forced restore failure'

rm -f -- "$docker_wrapper_state" "$docker_wrapper_state.failed"
transient_output="$temporary_directory/transient-inspect.log"
if run_backup_command_with_docker_wrapper restore-check "$second_backup_id" \
  >"$transient_output" 2>&1; then
  fail 'transient cleanup inspect failure unexpectedly succeeded'
fi
[[ -f "$docker_wrapper_state.failed" ]] ||
  fail 'transient cleanup inspect failure was not exercised'
! grep -Fq 'restore=verified' "$transient_output" ||
  fail 'cleanup failure incorrectly reported a verified restore'
! grep -Fq "$database_password" "$transient_output" ||
  fail 'cleanup failure exposed the database password'
assert_no_restore_resources

source_state="$(docker container exec "$source_container" psql \
  --tuples-only --no-align --set ON_ERROR_STOP=1 \
  --username "$database_user" --dbname "$database_name" \
  --command "SELECT version::text || '|' || dirty::text || '|' || (SELECT count(*)::text FROM public.users) FROM public.schema_migrations;")"
[[ "$source_state" == '9|false|1' ]] || fail 'backup workflow modified source data'
assert_no_restore_resources

printf 'China experiment PostgreSQL backup contract tests passed\n'
