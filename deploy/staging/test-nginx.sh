#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$staging_directory/manage.sh"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
readonly runtime_container="xe3-staging-nginx-test-${PPID}-$$"

fail() {
  printf 'staging Nginx test: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  docker rm --force "$runtime_container" >/dev/null 2>&1 || true
  rm -rf "$temporary_directory"
}

write_edge_environment() {
  local destination=$1
  local certificate=${2:-$temporary_directory/fullchain.pem}
  local certificate_key=${3:-$temporary_directory/privkey.pem}
  local htpasswd=${4:-$temporary_directory/staging.htpasswd}
  local acme_root=${5:-$temporary_directory/acme}
  local public_root=${6:-$temporary_directory/public}

  printf '%s\n' \
    "STAGING_TLS_CERTIFICATE=$certificate" \
    "STAGING_TLS_CERTIFICATE_KEY=$certificate_key" \
    "STAGING_HTPASSWD_FILE=$htpasswd" \
    "STAGING_ACME_ROOT=$acme_root" \
    "STAGING_PUBLIC_ROOT=$public_root" \
    >"$destination"
  chmod 0600 "$destination"
}

failure_index=0
LAST_FAILURE_OUTPUT=

expect_failure_without_compose() {
  local name=$1
  shift

  failure_index=$((failure_index + 1))
  LAST_FAILURE_OUTPUT="$temporary_directory/failure-$failure_index.out"
  rm -f "$compose_marker"
  if env \
    TEST_COMPOSE_MARKER="$compose_marker" \
    PATH="$temporary_directory/fake-bin:$PATH" \
    "$@" >"$LAST_FAILURE_OUTPUT" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
  [[ ! -e "$compose_marker" ]] ||
    fail "$name invoked Docker Compose before failing"
}

expect_edge_failure() {
  local name=$1
  local environment_file=$2
  local output="$temporary_directory/rejected-$failure_index.conf"

  expect_failure_without_compose "$name" \
    "$manage" render-nginx \
      --edge-env-file "$environment_file" \
      --output "$output"
  [[ ! -e "$output" ]] || fail "$name wrote a rejected Nginx configuration"
}

render_edge_without_compose() {
  local environment_file=$1
  local output=$2

  rm -f "$compose_marker"
  env \
    TEST_COMPOSE_MARKER="$compose_marker" \
    PATH="$temporary_directory/fake-bin:$PATH" \
    "$manage" render-nginx \
      --edge-env-file "$environment_file" \
      --output "$output" \
      >/dev/null
  [[ ! -e "$compose_marker" ]] || fail 'render-nginx invoked Docker Compose'
}

assert_contains() {
  local expected=$1
  grep -Fq -- "$expected" "$temporary_directory/default.conf" || {
    printf 'missing expected Nginx directive: %s\n' "$expected" >&2
    exit 1
  }
}

assert_pcre_capture_name_lengths() {
  local match name

  while IFS= read -r match; do
    name=${match:3:${#match}-4}
    ((${#name} <= 32)) || {
      printf 'PCRE capture name exceeds 32 characters: %s\n' "$name" >&2
      exit 1
    }
  done < <(grep -oE '\(\?<[^>]+>' "$temporary_directory/default.conf" || true)
}

assert_response_header() {
  local response_file=$1
  local expected=$2
  grep -Fqi -- "$expected" "$response_file" || {
    printf 'missing expected response header: %s\n' "$expected" >&2
    exit 1
  }
}

assert_curl_succeeds() {
  local description=$1
  shift

  curl --fail --show-error "$@" || {
    printf 'failed HTTP check: %s\n' "$description" >&2
    docker logs "$runtime_container" >&2
    exit 1
  }
}

command -v docker >/dev/null 2>&1 || {
  fail 'docker is required for the Nginx configuration check'
}
command -v openssl >/dev/null 2>&1 || {
  fail 'openssl is required for the Nginx configuration check'
}

temporary_directory=$(mktemp -d)
temporary_directory=$(cd "$temporary_directory" && pwd -P)
readonly temporary_directory
trap cleanup EXIT

mkdir -p "$temporary_directory/acme" \
  "$temporary_directory/fake-bin" \
  "$temporary_directory/logs" \
  "$temporary_directory/public/downloads/android/v0.1.0"
password_hash=$(openssl passwd -apr1 'test-password')
printf 'staging:%s\n' "$password_hash" >"$temporary_directory/staging.htpasswd"
chmod 0644 "$temporary_directory/staging.htpasswd"
printf '%s\n' 'signed-production-apk-fixture' > \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk"
apk_sha=$(sha256sum \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk" |
  awk '{print $1}')
apk_size=$(stat -c '%s' \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk" 2>/dev/null ||
  stat -f '%z' \
    "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk")
printf '%s  %s\n' "$apk_sha" 'speakup-v0.1.0-production-arm64.apk' > \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256"
jq --null-input --arg sha "$apk_sha" --argjson size "$apk_size" '
  {
    metadata_version: 1,
    version: "0.1.0",
    version_code: 1,
    published_at: "2026-08-23T12:34:56Z",
    file_name: "speakup-v0.1.0-production-arm64.apk",
    download_path:
      "/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk",
    size_bytes: $size,
    minimum_android_api: 24,
    abis: ["arm64-v8a"],
    apk_sha256: $sha,
    apk_certificate_sha256: ("e" * 64)
  }
' >"$temporary_directory/public/downloads/android/v0.1.0/release.json"
cp \
  "$temporary_directory/public/downloads/android/v0.1.0/release.json" \
  "$temporary_directory/public/downloads/android/release.json"
cp \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk" \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.1-production-arm64.apk"
openssl req \
  -x509 \
  -newkey rsa:2048 \
  -nodes \
  -subj '/CN=staging.speak-up.top' \
  -days 1 \
  -keyout "$temporary_directory/privkey.pem" \
  -out "$temporary_directory/fullchain.pem" \
  >/dev/null 2>&1
chmod 0600 \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/privkey.pem"

printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  ': > "$TEST_COMPOSE_MARKER"' \
  'exit 97' \
  >"$temporary_directory/fake-bin/docker"
chmod 0700 "$temporary_directory/fake-bin/docker"
readonly compose_marker="$temporary_directory/compose-called"

readonly edge_environment="$temporary_directory/staging-edge.env"
write_edge_environment "$edge_environment"

for forbidden_input in \
  "$temporary_directory/staging-runtime.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/release-manifest.json"; do
  [[ ! -e "$forbidden_input" && ! -L "$forbidden_input" ]] ||
    fail "edge fixture unexpectedly created runtime input: $forbidden_input"
done

cross_secret='runtime-secret-must-not-leak'
printf '%s\n%s\n' \
  "$(<"$edge_environment")" \
  "STAGING_POSTGRES_PASSWORD=$cross_secret" \
  >"$temporary_directory/cross-runtime-key.env"
chmod 0600 "$temporary_directory/cross-runtime-key.env"
expect_edge_failure \
  'edge environment containing a runtime key' \
  "$temporary_directory/cross-runtime-key.env"
if grep -Fq "$cross_secret" "$LAST_FAILURE_OUTPUT"; then
  fail 'rejected runtime key leaked its Secret value'
fi

legacy_output="$temporary_directory/legacy.conf"
expect_failure_without_compose 'legacy --env-file' \
  "$manage" render-nginx \
    --env-file "$edge_environment" \
    --output "$legacy_output"
[[ ! -e "$legacy_output" ]] || fail 'legacy --env-file wrote Nginx configuration'
grep -Fq -- '--edge-env-file' "$LAST_FAILURE_OUTPUT" ||
  fail 'legacy --env-file rejection did not provide the edge migration flag'

runtime_argument_index=0
for runtime_argument in --runtime-env-file --manifest --receipt; do
  runtime_argument_index=$((runtime_argument_index + 1))
  runtime_argument_output="$temporary_directory/runtime-argument-$runtime_argument_index.conf"
  expect_failure_without_compose "render-nginx with $runtime_argument" \
    "$manage" render-nginx \
      --edge-env-file "$edge_environment" \
      "$runtime_argument" "$temporary_directory/does-not-exist" \
      --output "$runtime_argument_output"
  [[ ! -e "$runtime_argument_output" ]] ||
    fail "render-nginx with $runtime_argument wrote Nginx configuration"
done

empty_runtime_output="$temporary_directory/empty-runtime-argument.conf"
expect_failure_without_compose 'render-nginx with empty runtime argument' \
  "$manage" render-nginx \
    --edge-env-file "$edge_environment" \
    --runtime-env-file '' \
    --output "$empty_runtime_output"
[[ ! -e "$empty_runtime_output" ]] ||
  fail 'render-nginx with empty runtime argument wrote Nginx configuration'

for empty_edge_argument in --edge-env-file --output; do
  empty_edge_argument_output="$temporary_directory/empty-${empty_edge_argument#--}.conf"
  case "$empty_edge_argument" in
    --edge-env-file)
      expect_failure_without_compose 'render-nginx with empty edge env argument' \
        "$manage" render-nginx \
          --edge-env-file '' \
          --output "$empty_edge_argument_output"
      ;;
    --output)
      expect_failure_without_compose 'render-nginx with empty output argument' \
        "$manage" render-nginx \
          --edge-env-file "$edge_environment" \
          --output ''
      ;;
  esac
  [[ ! -e "$empty_edge_argument_output" ]] ||
    fail "empty required edge argument wrote Nginx configuration: $empty_edge_argument"
done

duplicate_edge_argument_index=0
for duplicate_edge_argument in --edge-env-file --output; do
  duplicate_edge_argument_index=$((duplicate_edge_argument_index + 1))
  duplicate_edge_argument_output="$temporary_directory/duplicate-edge-argument-$duplicate_edge_argument_index.conf"
  case "$duplicate_edge_argument" in
    --edge-env-file)
      duplicate_edge_argument_value="$edge_environment"
      ;;
    --output)
      duplicate_edge_argument_value="$temporary_directory/other-edge-output.conf"
      ;;
  esac
  expect_failure_without_compose \
    "render-nginx with duplicate $duplicate_edge_argument" \
    "$manage" render-nginx \
      --edge-env-file "$edge_environment" \
      --output "$duplicate_edge_argument_output" \
      "$duplicate_edge_argument" "$duplicate_edge_argument_value"
  [[ ! -e "$duplicate_edge_argument_output" ]] ||
    fail "duplicate edge argument wrote Nginx configuration: $duplicate_edge_argument"
done

edge_keys=$(sed -n 's/=.*//p' "$edge_environment" | LC_ALL=C sort)
[[ "$edge_keys" == $'STAGING_ACME_ROOT\nSTAGING_HTPASSWD_FILE\nSTAGING_PUBLIC_ROOT\nSTAGING_TLS_CERTIFICATE\nSTAGING_TLS_CERTIFICATE_KEY' ]] ||
  fail 'edge fixture does not contain the exact edge key set'

grep -v '^STAGING_HTPASSWD_FILE=' \
  "$edge_environment" >"$temporary_directory/missing-edge-key.env"
chmod 0600 "$temporary_directory/missing-edge-key.env"
missing_edge_output="$temporary_directory/missing-edge-key.conf"
expect_failure_without_compose 'ambient edge value replacing a missing key' \
  env STAGING_HTPASSWD_FILE="$temporary_directory/staging.htpasswd" \
  "$manage" render-nginx \
    --edge-env-file "$temporary_directory/missing-edge-key.env" \
    --output "$missing_edge_output"
[[ ! -e "$missing_edge_output" ]] ||
  fail 'ambient edge value wrote Nginx configuration'

printf '%s\n%s\n' \
  "$(<"$edge_environment")" \
  "STAGING_ACME_ROOT=$temporary_directory/acme" \
  >"$temporary_directory/duplicate-edge-key.env"
chmod 0600 "$temporary_directory/duplicate-edge-key.env"
expect_edge_failure \
  'duplicate edge key' \
  "$temporary_directory/duplicate-edge-key.env"

edge_secret_sentinel='edge-secret-must-not-leak'
printf '%s\n%s\n' \
  "$(<"$edge_environment")" \
  "INVALID-KEY=$edge_secret_sentinel" \
  >"$temporary_directory/malformed-edge.env"
chmod 0600 "$temporary_directory/malformed-edge.env"
expect_edge_failure \
  'malformed edge key' \
  "$temporary_directory/malformed-edge.env"
if grep -Fq "$edge_secret_sentinel" "$LAST_FAILURE_OUTPUT"; then
  fail 'malformed edge entry leaked its raw content'
fi
grep -Eq 'at line [1-9][0-9]*' "$LAST_FAILURE_OUTPUT" ||
  fail 'malformed edge entry did not report a safe line number'

cp "$edge_environment" "$temporary_directory/insecure-edge.env"
chmod 0640 "$temporary_directory/insecure-edge.env"
expect_edge_failure \
  'group-readable edge environment' \
  "$temporary_directory/insecure-edge.env"

ln -s "$edge_environment" "$temporary_directory/edge-link.env"
expect_edge_failure \
  'symbolic-link edge environment' \
  "$temporary_directory/edge-link.env"

mkdir "$temporary_directory/edge-env-ancestor-target"
cp "$edge_environment" \
  "$temporary_directory/edge-env-ancestor-target/staging-edge.env"
chmod 0600 "$temporary_directory/edge-env-ancestor-target/staging-edge.env"
ln -s "$temporary_directory/edge-env-ancestor-target" \
  "$temporary_directory/edge-env-ancestor-link"
expect_edge_failure \
  'edge environment with a symbolic-link ancestor' \
  "$temporary_directory/edge-env-ancestor-link/staging-edge.env"

mkdir -p \
  "$temporary_directory/input-ancestor-target/nested/acme" \
  "$temporary_directory/input-ancestor-target/nested/public" \
  "$temporary_directory/unsafe-input-ancestor/nested"
cp \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/input-ancestor-target/nested/"
chmod 0600 \
  "$temporary_directory/input-ancestor-target/nested/privkey.pem" \
  "$temporary_directory/input-ancestor-target/nested/staging.htpasswd"
ln -s "$temporary_directory/input-ancestor-target" \
  "$temporary_directory/input-ancestor-link"

write_edge_environment \
  "$temporary_directory/certificate-ancestor-link.env" \
  "$temporary_directory/input-ancestor-link/nested/fullchain.pem"
expect_edge_failure \
  'TLS certificate with a symbolic-link ancestor' \
  "$temporary_directory/certificate-ancestor-link.env"

write_edge_environment \
  "$temporary_directory/key-ancestor-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/input-ancestor-link/nested/privkey.pem"
expect_edge_failure \
  'TLS private key with a symbolic-link ancestor' \
  "$temporary_directory/key-ancestor-link.env"

write_edge_environment \
  "$temporary_directory/htpasswd-ancestor-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/input-ancestor-link/nested/staging.htpasswd"
expect_edge_failure \
  'htpasswd with a symbolic-link ancestor' \
  "$temporary_directory/htpasswd-ancestor-link.env"

write_edge_environment \
  "$temporary_directory/acme-ancestor-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/input-ancestor-link/nested/acme"
expect_edge_failure \
  'ACME root with a symbolic-link ancestor' \
  "$temporary_directory/acme-ancestor-link.env"

write_edge_environment \
  "$temporary_directory/public-ancestor-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/acme" \
  "$temporary_directory/input-ancestor-link/nested/public"
expect_edge_failure \
  'public root with a symbolic-link ancestor' \
  "$temporary_directory/public-ancestor-link.env"

chmod 0777 "$temporary_directory/unsafe-input-ancestor"
cp "$temporary_directory/fullchain.pem" \
  "$temporary_directory/unsafe-input-ancestor/nested/fullchain.pem"
write_edge_environment \
  "$temporary_directory/unsafe-certificate-ancestor.env" \
  "$temporary_directory/unsafe-input-ancestor/nested/fullchain.pem"
expect_edge_failure \
  'TLS certificate with an unsafe writable ancestor' \
  "$temporary_directory/unsafe-certificate-ancestor.env"

cp "$temporary_directory/privkey.pem" "$temporary_directory/insecure-key.pem"
chmod 0644 "$temporary_directory/insecure-key.pem"
write_edge_environment \
  "$temporary_directory/insecure-key.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-key.pem"
expect_edge_failure \
  'world-readable TLS private key' \
  "$temporary_directory/insecure-key.env"

ln -s "$temporary_directory/privkey.pem" "$temporary_directory/key-link.pem"
write_edge_environment \
  "$temporary_directory/key-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/key-link.pem"
render_edge_without_compose \
  "$temporary_directory/key-link.env" \
  "$temporary_directory/key-link.conf"

mkdir -p \
  "$temporary_directory/certbot/live/staging.speak-up.top" \
  "$temporary_directory/certbot/archive/staging.speak-up.top"
cp "$temporary_directory/fullchain.pem" \
  "$temporary_directory/certbot/archive/staging.speak-up.top/fullchain1.pem"
cp "$temporary_directory/privkey.pem" \
  "$temporary_directory/certbot/archive/staging.speak-up.top/privkey1.pem"
chmod 0600 \
  "$temporary_directory/certbot/archive/staging.speak-up.top/privkey1.pem"
ln -s ../../archive/staging.speak-up.top/fullchain1.pem \
  "$temporary_directory/certbot/live/staging.speak-up.top/fullchain.pem"
ln -s ../../archive/staging.speak-up.top/privkey1.pem \
  "$temporary_directory/certbot/live/staging.speak-up.top/privkey.pem"
write_edge_environment \
  "$temporary_directory/certbot-links.env" \
  "$temporary_directory/certbot/live/staging.speak-up.top/fullchain.pem" \
  "$temporary_directory/certbot/live/staging.speak-up.top/privkey.pem"
render_edge_without_compose \
  "$temporary_directory/certbot-links.env" \
  "$temporary_directory/certbot-links.conf"

ln -s "$temporary_directory/not-there-key.pem" \
  "$temporary_directory/broken-key-link.pem"
write_edge_environment \
  "$temporary_directory/broken-key-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/broken-key-link.pem"
expect_edge_failure \
  'broken TLS private-key symlink' \
  "$temporary_directory/broken-key-link.env"

ln -s "$temporary_directory/insecure-key.pem" \
  "$temporary_directory/insecure-key-link.pem"
write_edge_environment \
  "$temporary_directory/insecure-key-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-key-link.pem"
expect_edge_failure \
  'TLS private-key symlink with an insecure target' \
  "$temporary_directory/insecure-key-link.env"

cp "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/insecure.htpasswd"
chmod 0644 "$temporary_directory/insecure.htpasswd"
write_edge_environment \
  "$temporary_directory/insecure-htpasswd.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/insecure.htpasswd"
expect_edge_failure \
  'world-readable htpasswd' \
  "$temporary_directory/insecure-htpasswd.env"

ln -s "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/htpasswd-link"
write_edge_environment \
  "$temporary_directory/htpasswd-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/htpasswd-link"
expect_edge_failure \
  'symbolic-link htpasswd' \
  "$temporary_directory/htpasswd-link.env"

cp "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-certificate.pem"
chmod 0664 "$temporary_directory/insecure-certificate.pem"
write_edge_environment \
  "$temporary_directory/insecure-certificate.env" \
  "$temporary_directory/insecure-certificate.pem"
expect_edge_failure \
  'group-writable public certificate' \
  "$temporary_directory/insecure-certificate.env"

mkdir "$temporary_directory/insecure-acme"
chmod 0777 "$temporary_directory/insecure-acme"
write_edge_environment \
  "$temporary_directory/insecure-acme.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/insecure-acme"
expect_edge_failure \
  'world-writable ACME root' \
  "$temporary_directory/insecure-acme.env"

mkdir "$temporary_directory/acme-target"
ln -s "$temporary_directory/acme-target" "$temporary_directory/acme-link"
write_edge_environment \
  "$temporary_directory/acme-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/acme-link"
expect_edge_failure \
  'symbolic-link ACME root' \
  "$temporary_directory/acme-link.env"

chmod 0777 "$temporary_directory/public"
expect_edge_failure \
  'world-writable public root' \
  "$edge_environment"
chmod 0755 "$temporary_directory/public"

chmod 0777 "$temporary_directory/public/downloads/android"
expect_edge_failure \
  'world-writable Android public directory' \
  "$edge_environment"
chmod 0755 "$temporary_directory/public/downloads/android"

mkdir "$temporary_directory/public-target"
ln -s "$temporary_directory/public-target" "$temporary_directory/public-link"
write_edge_environment \
  "$temporary_directory/public-link.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/acme" \
  "$temporary_directory/public-link"
expect_edge_failure \
  'symbolic-link public root' \
  "$temporary_directory/public-link.env"

real_id=$(command -v id)
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'if [[ "${1:-}" == -u ]]; then' \
  '  printf "%s\\n" 999999' \
  '  exit' \
  'fi' \
  'exec "$TEST_REAL_ID" "$@"' \
  >"$temporary_directory/fake-bin/id"
chmod 0700 "$temporary_directory/fake-bin/id"
wrong_edge_owner_output="$temporary_directory/wrong-edge-owner.conf"
expect_failure_without_compose 'edge environment owned by another UID' \
  env \
    TEST_REAL_ID="$real_id" \
    PATH="$temporary_directory/fake-bin:$PATH" \
  "$manage" render-nginx \
    --edge-env-file "$edge_environment" \
    --output "$wrong_edge_owner_output"
[[ ! -e "$wrong_edge_owner_output" ]] ||
  fail 'wrongly owned edge environment wrote Nginx configuration'
rm "$temporary_directory/fake-bin/id"

mkdir "$temporary_directory/fake-owner-bin"
real_stat=$(command -v stat)
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'path=${!#}' \
  'if [[ "$path" == "${TEST_WRONG_OWNER_PATH:-}" ]]; then' \
  '  for argument in "$@"; do' \
  '    if [[ "$argument" == "%u" ]]; then' \
  '      printf "%s\\n" 999999' \
  '      exit' \
  '    fi' \
  '  done' \
  'fi' \
  'exec "$TEST_REAL_STAT" "$@"' \
  >"$temporary_directory/fake-owner-bin/stat"
chmod 0700 "$temporary_directory/fake-owner-bin/stat"
owner_case_index=0
for owner_case in \
  "$temporary_directory/fullchain.pem|public certificate" \
  "$temporary_directory/staging.htpasswd|htpasswd" \
  "$temporary_directory/acme|ACME root" \
  "$temporary_directory/public|public root"; do
  owner_case_index=$((owner_case_index + 1))
  owner_path=${owner_case%%|*}
  owner_description=${owner_case#*|}
  owner_output="$temporary_directory/wrong-edge-owner-$owner_case_index.conf"
  expect_failure_without_compose "$owner_description owned by another UID" \
    env \
      TEST_REAL_STAT="$real_stat" \
      TEST_WRONG_OWNER_PATH="$owner_path" \
      PATH="$temporary_directory/fake-owner-bin:$temporary_directory/fake-bin:$PATH" \
    "$manage" render-nginx \
      --edge-env-file "$edge_environment" \
      --output "$owner_output"
  [[ ! -e "$owner_output" ]] ||
    fail "$owner_description owned by another UID wrote Nginx configuration"
done

wrong_key_owner_output="$temporary_directory/wrong-key-owner.conf"
expect_failure_without_compose \
  'TLS private-key symlink target owned by another UID' \
  env \
    TEST_REAL_STAT="$real_stat" \
    TEST_WRONG_OWNER_PATH="$temporary_directory/privkey.pem" \
    PATH="$temporary_directory/fake-owner-bin:$temporary_directory/fake-bin:$PATH" \
  "$manage" render-nginx \
    --edge-env-file "$temporary_directory/key-link.env" \
    --output "$wrong_key_owner_output"
[[ ! -e "$wrong_key_owner_output" ]] ||
  fail 'wrongly owned TLS private-key target wrote Nginx configuration'

render_edge_without_compose \
  "$edge_environment" \
  "$temporary_directory/default.conf"

for expected in \
  'access_log logs/xe3-speakup-staging-portal.access.log;' \
  'error_log logs/xe3-speakup-staging-portal.error.log warn;' \
  'access_log logs/xe3-speakup-staging-api.access.log;' \
  'error_log logs/xe3-speakup-staging-api.error.log warn;'; do
  grep -Fq -- "$expected" "$temporary_directory/default.conf" || {
    printf 'missing expected Nginx log directive: %s\n' "$expected" >&2
    exit 1
  }
done

assert_pcre_capture_name_lengths

assert_contains "root $temporary_directory/public;"
assert_contains 'default_type application/vnd.android.package-archive;'
assert_contains 'add_header Cache-Control "no-store" always;'
assert_contains 'add_header Cache-Control "public, max-age=31536000, immutable" always;'
api_configuration="$temporary_directory/api.conf"
sed -n '/server_name staging-api.speak-up.top;/,$p' \
  "$temporary_directory/default.conf" >"$api_configuration"
if grep -Fq "root $temporary_directory/public;" "$api_configuration"; then
  printf '%s\n' 'Staging API host exposes the Android public root' >&2
  exit 1
fi
assert_contains 'location ^~ /downloads/android/'

docker run --rm \
  --volume "$temporary_directory:$temporary_directory:ro" \
  --volume "$temporary_directory/logs:/etc/nginx/logs" \
  --volume "$temporary_directory/default.conf:/etc/nginx/conf.d/default.conf:ro" \
  "$nginx_image" \
  nginx -t

printf '%s\n' \
  'server {' \
  '    listen 28082;' \
  '    location / { return 200 "portal\n"; }' \
  '}' \
  'server {' \
  '    listen 28083;' \
  '    location / { return 200 "api\n"; }' \
  '}' \
  >"$temporary_directory/upstream.conf"

# The production host explicitly runs Nginx workers as root, which is required
# to read the deployment contract's owner-only htpasswd file.
printf '%s\n' \
  'user root;' \
  'worker_processes 1;' \
  'error_log /dev/stderr notice;' \
  'pid /tmp/nginx.pid;' \
  'events { worker_connections 1024; }' \
  'http {' \
  '    include /etc/nginx/mime.types;' \
  '    default_type application/octet-stream;' \
  '    access_log /dev/stdout;' \
  '    sendfile on;' \
  '    keepalive_timeout 65;' \
  '    include /etc/nginx/conf.d/*.conf;' \
  '}' \
  >"$temporary_directory/nginx.conf"

docker run --detach \
  --name "$runtime_container" \
  --publish 127.0.0.1::443 \
  --volume "$temporary_directory:$temporary_directory:ro" \
  --volume "$temporary_directory/logs:/etc/nginx/logs" \
  --volume "$temporary_directory/nginx.conf:/etc/nginx/nginx.conf:ro" \
  --volume "$temporary_directory/default.conf:/etc/nginx/conf.d/default.conf:ro" \
  --volume "$temporary_directory/upstream.conf:/etc/nginx/conf.d/upstream.conf:ro" \
  "$nginx_image" \
  >/dev/null

https_port=$(docker port "$runtime_container" 443/tcp | sed -n 's/.*://p')
[[ "$https_port" =~ ^[0-9]+$ ]] || {
  printf '%s\n' 'failed to resolve Staging Nginx HTTPS port' >&2
  exit 1
}

runtime_ready=false
for _ in {1..30}; do
  if ! status=$(curl \
      --insecure \
      --silent \
      --output /dev/null \
      --write-out '%{http_code}' \
      --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
      "https://staging.speak-up.top:$https_port/"); then
    status=000
  fi
  if [[ "$status" == 401 ]]; then
    runtime_ready=true
    break
  fi
  sleep 0.2
done
[[ "$runtime_ready" == true ]] || {
  docker logs "$runtime_container" >&2
  printf '%s\n' 'Staging Nginx did not become ready' >&2
  exit 1
}

unauthenticated_status=$(curl \
  --insecure \
  --silent \
  --output /dev/null \
  --write-out '%{http_code}' \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/release.json")
[[ "$unauthenticated_status" == 401 ]] || {
  printf 'Staging download without Basic Auth returned %s\n' \
    "$unauthenticated_status" >&2
  exit 1
}

current_headers="$temporary_directory/current.headers"
assert_curl_succeeds 'authenticated current release metadata request' \
  --insecure \
  --silent \
  --user 'staging:test-password' \
  --dump-header "$current_headers" \
  --output /dev/null \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/release.json"
assert_response_header "$current_headers" 'Content-Type: application/json'
assert_response_header "$current_headers" 'Cache-Control: no-store'

version_headers="$temporary_directory/version.headers"
assert_curl_succeeds 'authenticated versioned release metadata request' \
  --insecure \
  --silent \
  --user 'staging:test-password' \
  --dump-header "$version_headers" \
  --output /dev/null \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/v0.1.0/release.json"
assert_response_header "$version_headers" \
  'Cache-Control: public, max-age=31536000, immutable'

checksum_body="$temporary_directory/downloaded.sha256"
assert_curl_succeeds 'authenticated checksum download request' \
  --insecure \
  --silent \
  --user 'staging:test-password' \
  --output "$checksum_body" \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256"
cmp \
  "$checksum_body" \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256"

apk_headers="$temporary_directory/apk.headers"
apk_body="$temporary_directory/downloaded.apk"
assert_curl_succeeds 'authenticated APK HEAD request' \
  --head \
  --insecure \
  --silent \
  --user 'staging:test-password' \
  --dump-header "$apk_headers" \
  --output /dev/null \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk"
assert_response_header "$apk_headers" \
  'Content-Type: application/vnd.android.package-archive'
assert_response_header "$apk_headers" \
  'Cache-Control: public, max-age=31536000, immutable'
assert_response_header "$apk_headers" "Content-Length: $apk_size"
assert_curl_succeeds 'authenticated APK download request' \
  --insecure \
  --silent \
  --user 'staging:test-password' \
  --output "$apk_body" \
  --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
  "https://staging.speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk"
[[ "$(sha256sum "$apk_body" | awk '{print $1}')" == "$apk_sha" ]] || {
  printf '%s\n' 'downloaded Staging APK SHA-256 is incorrect' >&2
  exit 1
}

for path in \
  /downloads/android \
  /downloads/android/ \
  /downloads/android/latest.apk \
  /downloads/android/v0.1.0/unknown.txt \
  /downloads/android/v0.1.0/speakup-v0.1.1-production-arm64.apk; do
  status=$(curl \
    --insecure \
    --silent \
    --user 'staging:test-password' \
    --output /dev/null \
    --write-out '%{http_code}' \
    --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
    "https://staging.speak-up.top:$https_port$path")
  [[ "$status" == 404 ]] || {
    printf 'unexpected Staging Android path %s returned %s\n' "$path" "$status" >&2
    exit 1
  }
done

api_status=$(curl \
  --insecure \
  --silent \
  --output /dev/null \
  --write-out '%{http_code}' \
  --resolve "staging-api.speak-up.top:$https_port:127.0.0.1" \
  "https://staging-api.speak-up.top:$https_port/downloads/android/release.json")
[[ "$api_status" == 404 ]] || {
  printf 'Staging API download route returned %s instead of 404\n' "$api_status" >&2
  exit 1
}

printf '%s\n' 'staging Nginx configuration check passed'
