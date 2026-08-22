#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$staging_directory/manage.sh"
readonly compose_file="$staging_directory/compose.yaml"
readonly portal_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
readonly server_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
readonly git_sha="cccccccccccccccccccccccccccccccccccccccc"
readonly staging_apk_sha="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
readonly production_apk_sha="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
readonly certificate_sha="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

fail() {
  printf 'staging deploy test: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local name=$1
  shift
  if "$@" >"$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

write_environment() {
  local destination=$1
  local server_environment=$2
  local certificate=${3:-$temporary_directory/fullchain.pem}
  local certificate_key=${4:-$temporary_directory/privkey.pem}
  local htpasswd=${5:-$temporary_directory/staging.htpasswd}
  local acme_root=${6:-$temporary_directory/acme}

  printf '%s\n' \
    'STAGING_POSTGRES_DB=speakup_staging' \
    'STAGING_POSTGRES_USER=speakup_staging' \
    'STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
    'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
    "STAGING_SERVER_ENV_FILE=$server_environment" \
    'STAGING_PORTAL_HOST=staging.speak-up.top' \
    'STAGING_API_HOST=staging-api.speak-up.top' \
    "STAGING_TLS_CERTIFICATE=$certificate" \
    "STAGING_TLS_CERTIFICATE_KEY=$certificate_key" \
    "STAGING_HTPASSWD_FILE=$htpasswd" \
    "STAGING_ACME_ROOT=$acme_root" >"$destination"
  chmod 0600 "$destination"
}

write_manifest() {
  local destination=$1
  local selected_portal_digest=${2:-$portal_digest}

  printf '%s\n' \
    '{' \
    '  "manifest_version": 1,' \
    '  "version": "0.1.1",' \
    "  \"git_sha\": \"$git_sha\"," \
    '  "version_code": 2,' \
    '  "portal_image": "ghcr.io/1024xengineer/xe3-esl-portal",' \
    "  \"portal_image_digest\": \"$selected_portal_digest\"," \
    '  "server_image": "ghcr.io/1024xengineer/xe3-esl-server",' \
    "  \"server_image_digest\": \"$server_digest\"," \
    '  "staging_apk_file": "speakup-v0.1.1-staging-arm64.apk",' \
    "  \"staging_apk_sha256\": \"$staging_apk_sha\"," \
    '  "production_apk_file": "speakup-v0.1.1-production-arm64.apk",' \
    '  "production_apk_size_bytes": 123456,' \
    "  \"production_apk_sha256\": \"$production_apk_sha\"," \
    '  "application_id": "com.xengineer.speakup",' \
    '  "minimum_android_api": 24,' \
    '  "abis": ["arm64-v8a"],' \
    "  \"apk_certificate_sha256\": \"$certificate_sha\"," \
    '  "database_schema_version": 7,' \
    '  "quality_run_url": "https://github.com/1024XEngineer/XE3-ESL/actions/runs/123456"' \
    '}' >"$destination"
}

temporary_directory=$(mktemp -d)
temporary_directory=$(cd "$temporary_directory" && pwd -P)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$temporary_directory/acme" "$temporary_directory/fake-bin"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$temporary_directory/server.env"
printf '%s\n' 'test-user:test-password-hash' >"$temporary_directory/staging.htpasswd"
printf '%s\n' 'test-certificate-placeholder' >"$temporary_directory/fullchain.pem"
printf '%s\n' 'test-key-placeholder' >"$temporary_directory/privkey.pem"
chmod 0600 \
  "$temporary_directory/server.env" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/privkey.pem"
write_environment "$temporary_directory/staging.env" "$temporary_directory/server.env"
write_manifest "$temporary_directory/release-manifest.json"

bash -n "$manage" "$0"

"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/staging.env" \
  >"$temporary_directory/validate.out"
grep -Fq 'validated=true' "$temporary_directory/validate.out" ||
  fail "valid deployment contract was not accepted"

write_manifest "$temporary_directory/invalid-manifest.json" 'latest'
expect_failure "mutable Portal image reference" \
  "$manage" validate \
  --manifest "$temporary_directory/invalid-manifest.json" \
  --env-file "$temporary_directory/staging.env"

write_manifest \
  "$temporary_directory/zero-digest-manifest.json" \
  'sha256:0000000000000000000000000000000000000000000000000000000000000000'
expect_failure "placeholder Portal digest" \
  "$manage" validate \
  --manifest "$temporary_directory/zero-digest-manifest.json" \
  --env-file "$temporary_directory/staging.env"

jq 'del(.quality_run_url)' \
  "$temporary_directory/release-manifest.json" \
  > "$temporary_directory/incomplete-manifest.json"
expect_failure "incomplete release manifest" \
  "$manage" validate \
    --manifest "$temporary_directory/incomplete-manifest.json" \
    --env-file "$temporary_directory/staging.env"

jq '.unexpected = true' \
  "$temporary_directory/release-manifest.json" \
  > "$temporary_directory/extended-manifest.json"
expect_failure "release manifest with an unknown field" \
  "$manage" validate \
    --manifest "$temporary_directory/extended-manifest.json" \
    --env-file "$temporary_directory/staging.env"

printf '%s\n%s\n' \
  "$(< "$temporary_directory/release-manifest.json")" \
  "$(< "$temporary_directory/release-manifest.json")" \
  > "$temporary_directory/multiple-manifests.json"
expect_failure "multiple release manifest documents" \
  "$manage" validate \
    --manifest "$temporary_directory/multiple-manifests.json" \
    --env-file "$temporary_directory/staging.env"

expect_failure "missing release manifest" \
  "$manage" validate \
  --manifest "$temporary_directory/missing.json" \
  --env-file "$temporary_directory/staging.env"

expect_failure "missing environment file" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/not-there.env"

sed 's/^PORTAL_ADMIN_PASSWORD=.*/PORTAL_ADMIN_PASSWORD=/' \
  "$temporary_directory/staging.env" >"$temporary_directory/missing-value.env"
chmod 0600 "$temporary_directory/missing-value.env"
expect_failure "missing required environment value" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/missing-value.env"

sed '/^PORTAL_ADMIN_PASSWORD=/d' \
  "$temporary_directory/staging.env" >"$temporary_directory/missing-key.env"
chmod 0600 "$temporary_directory/missing-key.env"
expect_failure "ambient value replacing a missing environment key" \
  env PORTAL_ADMIN_PASSWORD=ambient-password-must-not-be-used \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/missing-key.env"

printf '%s\n' \
  "$(<"$temporary_directory/staging.env")" \
  'UNKNOWN_SETTING=typo' >"$temporary_directory/unknown.env"
chmod 0600 "$temporary_directory/unknown.env"
expect_failure "unknown environment setting" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/unknown.env"

readonly secret_sentinel='must-not-leak-environment-secret-sentinel'
environment_leak_index=0
for unsafe_environment_line in \
  "$secret_sentinel" \
  "INVALID-KEY=$secret_sentinel" \
  "UNSUPPORTED_SECRET_KEY=$secret_sentinel"; do
  environment_leak_index=$((environment_leak_index + 1))
  leak_environment="$temporary_directory/leak-$environment_leak_index.env"
  leak_output="$temporary_directory/leak-$environment_leak_index.out"
  printf '%s\n%s\n' \
    "$(< "$temporary_directory/staging.env")" \
    "$unsafe_environment_line" > "$leak_environment"
  chmod 0600 "$leak_environment"
  if "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$leak_environment" > "$leak_output" 2>&1; then
    fail "invalid environment entry unexpectedly succeeded"
  fi
  if grep -Fq "$secret_sentinel" "$leak_output"; then
    fail "invalid environment entry leaked its raw content"
  fi
  grep -Eq 'at line [1-9][0-9]*' "$leak_output" ||
    fail "invalid environment entry did not report a safe line number"
done

write_environment "$temporary_directory/missing-server.env" "$temporary_directory/not-there.env"
expect_failure "missing Server environment file" \
  "$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/missing-server.env"

cp "$temporary_directory/staging.env" "$temporary_directory/insecure-staging.env"
chmod 0640 "$temporary_directory/insecure-staging.env"
expect_failure "group-readable Staging environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-staging.env"

ln -s "$temporary_directory/staging.env" "$temporary_directory/symlink-staging.env"
expect_failure "symbolic-link Staging environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/symlink-staging.env"

mkdir "$temporary_directory/staging-env-ancestor-target"
cp "$temporary_directory/staging.env" \
  "$temporary_directory/staging-env-ancestor-target/staging.env"
chmod 0600 "$temporary_directory/staging-env-ancestor-target/staging.env"
ln -s "$temporary_directory/staging-env-ancestor-target" \
  "$temporary_directory/staging-env-ancestor-link"
expect_failure "Staging environment with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging-env-ancestor-link/staging.env"

cp "$temporary_directory/server.env" "$temporary_directory/insecure-server.env"
chmod 0644 "$temporary_directory/insecure-server.env"
write_environment "$temporary_directory/insecure-server-config.env" \
  "$temporary_directory/insecure-server.env"
expect_failure "world-readable Server environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-server-config.env"

ln -s "$temporary_directory/server.env" "$temporary_directory/server-link.env"
write_environment "$temporary_directory/server-link-config.env" \
  "$temporary_directory/server-link.env"
expect_failure "symbolic-link Server environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/server-link-config.env"

mkdir -p \
  "$temporary_directory/input-ancestor-target/nested/acme" \
  "$temporary_directory/unsafe-input-ancestor/nested"
cp "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/input-ancestor-target/nested/"
chmod 0600 \
  "$temporary_directory/input-ancestor-target/nested/server.env" \
  "$temporary_directory/input-ancestor-target/nested/privkey.pem" \
  "$temporary_directory/input-ancestor-target/nested/staging.htpasswd"
ln -s "$temporary_directory/input-ancestor-target" \
  "$temporary_directory/input-ancestor-link"

write_environment "$temporary_directory/server-ancestor-link.env" \
  "$temporary_directory/input-ancestor-link/nested/server.env"
expect_failure "Server environment with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/server-ancestor-link.env"

write_environment "$temporary_directory/certificate-ancestor-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/input-ancestor-link/nested/fullchain.pem"
expect_failure "TLS certificate with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/certificate-ancestor-link.env"

write_environment "$temporary_directory/key-ancestor-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/input-ancestor-link/nested/privkey.pem"
expect_failure "TLS private key with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/key-ancestor-link.env"

write_environment "$temporary_directory/htpasswd-ancestor-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/input-ancestor-link/nested/staging.htpasswd"
expect_failure "htpasswd with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/htpasswd-ancestor-link.env"

write_environment "$temporary_directory/acme-ancestor-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/input-ancestor-link/nested/acme"
expect_failure "ACME root with a symbolic-link ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/acme-ancestor-link.env"

chmod 0777 "$temporary_directory/unsafe-input-ancestor"
cp "$temporary_directory/server.env" \
  "$temporary_directory/unsafe-input-ancestor/nested/server.env"
chmod 0600 "$temporary_directory/unsafe-input-ancestor/nested/server.env"
write_environment "$temporary_directory/unsafe-server-ancestor.env" \
  "$temporary_directory/unsafe-input-ancestor/nested/server.env"
expect_failure "Server environment with an unsafe writable ancestor" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/unsafe-server-ancestor.env"

cp "$temporary_directory/privkey.pem" "$temporary_directory/insecure-key.pem"
chmod 0644 "$temporary_directory/insecure-key.pem"
write_environment "$temporary_directory/insecure-key.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-key.pem"
expect_failure "world-readable TLS private key" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-key.env"

ln -s "$temporary_directory/privkey.pem" "$temporary_directory/key-link.pem"
write_environment "$temporary_directory/key-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/key-link.pem"
"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/key-link.env" \
  > "$temporary_directory/key-link-validate.out"
grep -Fq 'validated=true' "$temporary_directory/key-link-validate.out" ||
  fail "valid Certbot-style TLS private-key symlink was rejected"

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
write_environment "$temporary_directory/certbot-links.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/certbot/live/staging.speak-up.top/fullchain.pem" \
  "$temporary_directory/certbot/live/staging.speak-up.top/privkey.pem"
"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/certbot-links.env" \
  > "$temporary_directory/certbot-links.out"
grep -Fq 'validated=true' "$temporary_directory/certbot-links.out" ||
  fail "valid Certbot live/archive links were rejected"

ln -s "$temporary_directory/not-there-key.pem" \
  "$temporary_directory/broken-key-link.pem"
write_environment "$temporary_directory/broken-key-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/broken-key-link.pem"
expect_failure "broken TLS private-key symlink" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/broken-key-link.env"

ln -s "$temporary_directory/insecure-key.pem" \
  "$temporary_directory/insecure-key-link.pem"
write_environment "$temporary_directory/insecure-key-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-key-link.pem"
expect_failure "TLS private-key symlink with an insecure target" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-key-link.env"

cp "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/insecure.htpasswd"
chmod 0644 "$temporary_directory/insecure.htpasswd"
write_environment "$temporary_directory/insecure-htpasswd.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/insecure.htpasswd"
expect_failure "world-readable htpasswd" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-htpasswd.env"

ln -s "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/htpasswd-link"
write_environment "$temporary_directory/htpasswd-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/htpasswd-link"
expect_failure "symbolic-link htpasswd" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/htpasswd-link.env"

cp "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-certificate.pem"
chmod 0664 "$temporary_directory/insecure-certificate.pem"
write_environment "$temporary_directory/insecure-certificate.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/insecure-certificate.pem"
expect_failure "group-writable public certificate" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-certificate.env"

mkdir "$temporary_directory/insecure-acme"
chmod 0777 "$temporary_directory/insecure-acme"
write_environment "$temporary_directory/insecure-acme.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/insecure-acme"
expect_failure "world-writable ACME root" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-acme.env"

mkdir "$temporary_directory/acme-target"
ln -s "$temporary_directory/acme-target" "$temporary_directory/acme-link"
write_environment "$temporary_directory/acme-link.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/staging.htpasswd" \
  "$temporary_directory/acme-link"
expect_failure "symbolic-link ACME root" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/acme-link.env"

real_id=$(command -v id)
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'if [[ "${1:-}" == -u ]]; then' \
  '  printf "%s\\n" 999999' \
  '  exit' \
  'fi' \
  'exec "$TEST_REAL_ID" "$@"' > "$temporary_directory/fake-bin/id"
chmod +x "$temporary_directory/fake-bin/id"
expect_failure "Staging environment owned by another UID" \
  env TEST_REAL_ID="$real_id" PATH="$temporary_directory/fake-bin:$PATH" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
rm "$temporary_directory/fake-bin/id"

mkdir "$temporary_directory/fake-owner-bin"
real_stat=$(command -v stat)
cat > "$temporary_directory/fake-owner-bin/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
path=${!#}
if [[ "$path" == "${TEST_WRONG_OWNER_PATH:-}" ]]; then
  for argument in "$@"; do
    if [[ "$argument" == '%u' ]]; then
      printf '%s\n' 999999
      exit
    fi
  done
fi
exec "$TEST_REAL_STAT" "$@"
EOF
chmod +x "$temporary_directory/fake-owner-bin/stat"
for owner_case in \
  "$temporary_directory/server.env|Server environment" \
  "$temporary_directory/fullchain.pem|public certificate" \
  "$temporary_directory/acme|ACME root"; do
  owner_path=${owner_case%%|*}
  owner_description=${owner_case#*|}
  expect_failure "$owner_description owned by another UID" \
    env \
      TEST_REAL_STAT="$real_stat" \
      TEST_WRONG_OWNER_PATH="$owner_path" \
      PATH="$temporary_directory/fake-owner-bin:$PATH" \
    "$manage" validate \
      --manifest "$temporary_directory/release-manifest.json" \
      --env-file "$temporary_directory/staging.env"
done
expect_failure "TLS private-key symlink target owned by another UID" \
  env \
    TEST_REAL_STAT="$real_stat" \
    TEST_WRONG_OWNER_PATH="$temporary_directory/privkey.pem" \
    PATH="$temporary_directory/fake-owner-bin:$PATH" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/key-link.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/staging.env" \
  --output "$temporary_directory/staging.conf" \
  >"$temporary_directory/render.out"
if grep -Eq '__STAGING_[A-Z_]+__' "$temporary_directory/staging.conf"; then
  fail "rendered Nginx configuration contains a placeholder"
fi
[[ $(grep -Fc 'auth_basic "SpeakUp Staging";' "$temporary_directory/staging.conf") -eq 1 ]] ||
  fail "Nginx Basic Auth must apply only to the Portal host"
sed -n '/server_name staging-api.speak-up.top;/,$p' \
  "$temporary_directory/staging.conf" >"$temporary_directory/api-server.conf"
if grep -Fq 'auth_basic' "$temporary_directory/api-server.conf"; then
  fail "API host must preserve application Bearer auth without HTTP Basic Auth"
fi
grep -Fq 'proxy_set_header Authorization $http_authorization;' \
  "$temporary_directory/api-server.conf" ||
  fail "API proxy does not explicitly preserve the Authorization header"
[[ $(grep -Fc 'location = /metrics {' "$temporary_directory/staging.conf") -eq 2 ]] ||
  fail "Nginx does not block /metrics on both hosts"
grep -Fq 'proxy_pass http://127.0.0.1:28082;' "$temporary_directory/staging.conf" ||
  fail "Portal proxy is not loopback-only"
grep -Fq 'proxy_pass http://127.0.0.1:28083;' "$temporary_directory/staging.conf" ||
  fail "Server proxy is not loopback-only"

STAGING_POSTGRES_DB=speakup_staging \
STAGING_POSTGRES_USER=speakup_staging \
STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef \
PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests \
STAGING_SERVER_ENV_FILE="$temporary_directory/server.env" \
STAGING_PORTAL_HOST=staging.speak-up.top \
PORTAL_IMAGE_DIGEST="$portal_digest" \
SERVER_IMAGE_DIGEST="$server_digest" \
COMPOSE_PROJECT_NAME=xe3-speakup-production \
  docker compose \
    --env-file /dev/null \
    --project-name xe3-speakup-staging \
    --file "$compose_file" \
    --profile migration \
    config --format json >"$temporary_directory/compose.json"

jq --exit-status \
  --arg portal_digest "$portal_digest" \
  --arg server_digest "$server_digest" '
    .name == "xe3-speakup-staging" and
    .services.portal.image ==
      ("ghcr.io/1024xengineer/xe3-esl-portal@" + $portal_digest) and
    .services.server.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    .services.migrate.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    ([.services[] | has("build")] | any | not) and
    ([.services[] | has("container_name")] | any | not) and
    .services.portal.ports[0].host_ip == "127.0.0.1" and
    (.services.portal.ports[0].published | tostring) == "28082" and
    .services.server.ports[0].host_ip == "127.0.0.1" and
    (.services.server.ports[0].published | tostring) == "28083" and
    (.services.postgres | has("ports") | not) and
    .networks.database.internal == true and
    (.services.portal.networks | keys) == ["portal_edge"] and
    (.services.postgres.networks | keys) == ["database"] and
    (.services.migrate.networks | keys) == ["database"] and
    (.services.server.networks | keys | sort) == ["database", "server_edge"] and
    (.volumes | keys | sort) == ["portal_data", "postgres_data"]
  ' "$temporary_directory/compose.json" >/dev/null ||
  fail "resolved Compose model violates the isolation contract"

cat > "$temporary_directory/fake-bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[[ -z "${COMMAND_LOG:-}" ]] || printf 'docker %s\n' "$*" >> "$COMMAND_LOG"

readonly portal_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
readonly server_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
readonly postgres_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
readonly project=xe3-speakup-staging

if [[ "${1:-}" == compose ]]; then
  command_line=" $* "
  if [[ "$command_line" == *" run "* &&
    "$command_line" == *"/usr/local/bin/speakup-migrate version"* ]]; then
    printf '%s\n' "${SCHEMA_OUTPUT:-version=7 dirty=false}"
    exit
  fi
  if [[ "$command_line" == *" run "* && "$command_line" == *" migrate "* ]]; then
    [[ "${FAIL_MIGRATION:-0}" != 1 ]] || exit 71
    printf '%s\n' 'migrations=applied'
    exit
  fi
  exit
fi

if [[ "${1:-}" == ps ]]; then
  all=0
  service=""
  requested_project=""
  shift
  while (($# > 0)); do
    case "$1" in
      --all)
        all=1
        shift
        ;;
      --filter)
        case "$2" in
          label=com.docker.compose.project=*)
            requested_project=${2#label=com.docker.compose.project=}
            ;;
          label=com.docker.compose.service=*)
            service=${2#label=com.docker.compose.service=}
            ;;
        esac
        shift 2
        ;;
      --format)
        shift 2
        ;;
      *)
        exit 2
        ;;
    esac
  done
  runtime_project=${TEST_RUNTIME_PROJECT:-$project}
  [[ "$requested_project" == "$runtime_project" ]] || exit
  if ((all == 1)); then
    for candidate in portal postgres server; do
      [[ "$candidate" != "${TEST_MISSING_SERVICE:-}" ]] || continue
      printf '%s\n' "$candidate"
    done
    [[ "${TEST_EXTRA_SERVICE:-0}" != 1 ]] || printf '%s\n' legacy
    exit
  fi
  [[ "$service" != "${TEST_MISSING_SERVICE:-}" ]] || exit
  case "$service" in
    portal) printf '%.12s\n' "$portal_id" ;;
    server) printf '%.12s\n' "$server_id" ;;
    postgres) printf '%.12s\n' "$postgres_id" ;;
    *) exit 2 ;;
  esac
  [[ "$service" != "${TEST_DUPLICATE_SERVICE:-}" ]] ||
    printf '%s\n' dddddddddddd
  exit
fi

if [[ "${1:-}" == inspect && "$#" == 2 ]]; then
  case "$2" in
    aaaaaaaaaaaa)
      id=$portal_id
      service=portal
      image="ghcr.io/1024xengineer/xe3-esl-portal@${TEST_RUNTIME_PORTAL_DIGEST:-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
      mounts='[{"Type":"volume","Name":"xe3-speakup-staging_portal_data","Destination":"/app/.wrangler","RW":true}]'
      ports='{"3000/tcp":[{"HostIp":"127.0.0.1","HostPort":"28082"}]}'
      networks='{"xe3-speakup-staging_portal_edge":{}}'
      environment='["NODE_ENV=production","PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests","PORTAL_SQLITE_PATH=/app/.wrangler/portal.sqlite","VINEXT_TRUSTED_HOSTS=staging.speak-up.top"]'
      ;;
    bbbbbbbbbbbb)
      id=$server_id
      service=server
      image="ghcr.io/1024xengineer/xe3-esl-server@${TEST_RUNTIME_SERVER_DIGEST:-sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}"
      mounts='[]'
      ports='{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"28083"}]}'
      networks='{"xe3-speakup-staging_database":{},"xe3-speakup-staging_server_edge":{}}'
      environment='["TEXT_GENERATION_PROVIDER=test-fixture","DATABASE_URL=postgres://speakup_staging:0123456789abcdef0123456789abcdef@postgres:5432/speakup_staging?sslmode=disable","SERVER_HOST=0.0.0.0","SERVER_PORT=8080"]'
      ;;
    cccccccccccc)
      id=$postgres_id
      service=postgres
      image='postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
      mounts='[{"Type":"volume","Name":"xe3-speakup-staging_postgres_data","Destination":"/var/lib/postgresql","RW":true}]'
      ports='{"5432/tcp":null}'
      networks='{"xe3-speakup-staging_database":{}}'
      environment='["PGDATA=/var/lib/postgresql/18/docker","POSTGRES_DB=speakup_staging","POSTGRES_USER=speakup_staging","POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef"]'
      ;;
    *)
      exit 1
      ;;
  esac
  health=healthy
  runtime_project=${TEST_RUNTIME_PROJECT:-$project}
  project_label=$runtime_project
  [[ "${TEST_BAD_CONTAINER_LABEL:-}" != "$service" ]] || project_label=wrong-project
  [[ "${TEST_UNHEALTHY_SERVICE:-}" != "$service" ]] || health=unhealthy
  if [[ "${TEST_BAD_RUNTIME_IMAGE:-}" == "$service" ]]; then
    image="${image%sha256:*}sha256:9999999999999999999999999999999999999999999999999999999999999999"
  fi
  if [[ "${TEST_BAD_VOLUME_SERVICE:-}" == "$service" ]]; then
    mounts='[{"Type":"volume","Name":"wrong-volume","Destination":"/wrong","RW":true}]'
  fi
  [[ "${TEST_BAD_PORT_SERVICE:-}" != "$service" ]] || ports='{}'
  [[ "${TEST_BAD_NETWORK_SERVICE:-}" != "$service" ]] || networks='{}'
  jq --null-input \
    --arg id "$id" \
    --arg project "$project_label" \
    --arg service "$service" \
    --arg image "$image" \
    --arg health "$health" \
    --argjson environment "$environment" \
    --argjson mounts "$mounts" \
    --argjson ports "$ports" \
    --argjson networks "$networks" '
      [{
        Id: $id,
        Config: {
          Image: $image,
          Env: $environment,
          Labels: {
            "com.docker.compose.project": $project,
            "com.docker.compose.service": $service
          }
        },
        State: {Status: "running", Running: true, Health: {Status: $health}},
        Mounts: $mounts,
        NetworkSettings: {Ports: $ports, Networks: $networks}
      }]
    '
  exit
fi

if [[ "${1:-}" == image && "${2:-}" == inspect && "$#" == 3 ]]; then
  expected_image=$3
  case "$expected_image" in
    ghcr.io/1024xengineer/xe3-esl-portal@*) service=portal ;;
    ghcr.io/1024xengineer/xe3-esl-server@*) service=server ;;
    postgres:18-bookworm@*) service=postgres ;;
    *) exit 1 ;;
  esac
  [[ "${TEST_MISSING_IMAGE:-}" != "$service" ]] || exit 1
  architecture=amd64
  [[ "${TEST_BAD_IMAGE_PLATFORM:-}" != "$service" ]] || architecture=arm64
  if [[ "$service" == postgres ]]; then
    labels='{}'
  else
    version=0.1.1
    [[ "${TEST_BAD_OCI_LABEL:-}" != "$service" ]] || version=9.9.9
    labels=$(jq --null-input \
      --arg version "$version" \
      --arg revision cccccccccccccccccccccccccccccccccccccccc '
        {
          "org.opencontainers.image.source":
            "https://github.com/1024XEngineer/XE3-ESL",
          "org.opencontainers.image.revision": $revision,
          "org.opencontainers.image.version": $version
        }
      ')
  fi
  repo_digests=$(jq --null-input --arg image "$expected_image" '[$image]')
  [[ "${TEST_BAD_REPO_DIGEST:-}" != "$service" ]] || repo_digests='[]'
  jq --null-input \
    --arg image "$expected_image" \
    --arg architecture "$architecture" \
    --argjson labels "$labels" \
    --argjson repo_digests "$repo_digests" '
      [{
        Id: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
        RepoDigests: $repo_digests,
        Os: "linux",
        Architecture: $architecture,
        Config: {Labels: $labels}
      }]
    '
  exit
fi

if [[ "${1:-}" == network && "${2:-}" == inspect && "$#" == 3 ]]; then
  name=$3
  logical_name=${name#xe3-speakup-staging_}
  case "$logical_name" in
    portal_edge | server_edge) internal=false ;;
    database) internal=true ;;
    *) exit 1 ;;
  esac
  [[ "${TEST_DATABASE_NETWORK_PUBLIC:-0}" != 1 || "$logical_name" != database ]] ||
    internal=false
  project_label=$project
  [[ "${TEST_BAD_NETWORK_LABEL:-}" != "$logical_name" ]] || project_label=wrong-project
  jq --null-input \
    --arg name "$name" \
    --arg project "$project_label" \
    --arg logical_name "$logical_name" \
    --argjson internal "$internal" '
      [{
        Name: $name,
        Internal: $internal,
        Labels: {
          "com.docker.compose.project": $project,
          "com.docker.compose.network": $logical_name
        }
      }]
    '
  exit
fi

if [[ "${1:-}" == volume && "${2:-}" == inspect && "$#" == 3 ]]; then
  name=$3
  logical_name=${name#xe3-speakup-staging_}
  case "$logical_name" in
    portal_data | postgres_data) ;;
    *) exit 1 ;;
  esac
  project_label=$project
  [[ "${TEST_BAD_VOLUME_LABEL:-}" != "$logical_name" ]] || project_label=wrong-project
  jq --null-input \
    --arg name "$name" \
    --arg project "$project_label" \
    --arg logical_name "$logical_name" '
      [{
        Name: $name,
        Labels: {
          "com.docker.compose.project": $project,
          "com.docker.compose.volume": $logical_name
        }
      }]
    '
  exit
fi

exit 2
EOF

cat > "$temporary_directory/fake-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ -z "${COMMAND_LOG:-}" ]] || printf 'curl %s\n' "$*" >> "$COMMAND_LOG"
[[ "${FAIL_HEALTH:-0}" != 1 ]]
EOF

cat > "$temporary_directory/fake-bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

cat > "$temporary_directory/fake-bin/flock" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ -z "${COMMAND_LOG:-}" ]] || printf 'flock %s\n' "$*" >> "$COMMAND_LOG"
if [[ "${TEST_FLOCK_BUSY:-0}" == 1 ]]; then
  exit 1
fi
if [[ "${TEST_FLOCK_SWAP_SYMLINK:-0}" == 1 ]]; then
  rm -f "$TEST_LOCK_PATH"
  ln -s "$TEST_LOCK_SWAP_TARGET" "$TEST_LOCK_PATH"
fi
EOF

chmod +x \
  "$temporary_directory/fake-bin/docker" \
  "$temporary_directory/fake-bin/curl" \
  "$temporary_directory/fake-bin/sleep" \
  "$temporary_directory/fake-bin/flock"

readonly fake_path="$temporary_directory/fake-bin:$PATH"
mkdir "$temporary_directory/lock-directory"
chmod 0700 "$temporary_directory/lock-directory"
readonly lock_file="$temporary_directory/lock-directory/deploy.lock"

expect_failure "deploy without a receipt" \
  env PATH="$fake_path" SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"

receipt="$temporary_directory/staging-deploy-receipt.json"
COMMAND_LOG="$temporary_directory/deploy.log" \
PATH="$fake_path" \
SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    --receipt "$receipt" \
    > "$temporary_directory/deploy.out"

postgres_line=$(grep -nF \
  'up --pull never --detach --no-build --wait --wait-timeout 90 postgres' \
  "$temporary_directory/deploy.log" | head -n 1 | cut -d: -f1)
migration_line=$(grep -nF \
  'run --pull never --rm --no-deps migrate' \
  "$temporary_directory/deploy.log" | head -n 1 | cut -d: -f1)
schema_line=$(grep -nF \
  'run --pull never --rm --no-deps migrate /usr/local/bin/speakup-migrate version' \
  "$temporary_directory/deploy.log" | head -n 1 | cut -d: -f1)
application_line=$(grep -nF \
  'up --pull never --detach --no-build --wait --wait-timeout 90 portal server' \
  "$temporary_directory/deploy.log" | head -n 1 | cut -d: -f1)
[[ -n "$postgres_line" && -n "$migration_line" && -n "$schema_line" &&
  -n "$application_line" ]] || fail "deployment steps were not recorded"
((postgres_line < migration_line && migration_line < schema_line &&
  schema_line < application_line)) ||
  fail "the exact schema was not verified before the application update"
grep -Fq -- '--project-name xe3-speakup-staging' \
  "$temporary_directory/deploy.log" ||
  fail "deployment does not pin the isolated Compose project name"
grep -Fq 'http://127.0.0.1:28082/' "$temporary_directory/deploy.log" ||
  fail "deployment did not verify the Portal loopback endpoint"
grep -Fq 'http://127.0.0.1:28083/health' "$temporary_directory/deploy.log" ||
  fail "deployment did not verify /health"
grep -Fq 'http://127.0.0.1:28083/readyz' "$temporary_directory/deploy.log" ||
  fail "deployment did not verify /readyz"
[[ -f "$receipt" ]] || fail "successful deployment did not write a receipt"
manifest_sha=$(shasum -a 256 "$temporary_directory/release-manifest.json" |
  awk '{print $1}')
jq --exit-status \
  --arg manifest_sha "$manifest_sha" \
  --arg portal_digest "$portal_digest" \
  --arg server_digest "$server_digest" \
  --arg git_sha "$git_sha" '
    keys == [
      "database_schema_version",
      "deployed_at_utc",
      "git_sha",
      "manifest_sha256",
      "portal_container_id",
      "portal_image_digest",
      "postgres_container_id",
      "receipt_version",
      "server_container_id",
      "server_image_digest",
      "version"
    ] and
    .receipt_version == 1 and
    .manifest_sha256 == $manifest_sha and
    .version == "0.1.1" and
    .git_sha == $git_sha and
    .database_schema_version == 7 and
    .portal_image_digest == $portal_digest and
    .server_image_digest == $server_digest and
    (.portal_container_id | test("^[a-f0-9]{64}$")) and
    (.server_container_id | test("^[a-f0-9]{64}$")) and
    (.postgres_container_id | test("^[a-f0-9]{64}$")) and
    (.deployed_at_utc | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))
  ' "$receipt" >/dev/null || fail "deployment receipt is incomplete"
if grep -Fq \
  -e 'portal-admin-password-for-tests' \
  -e '0123456789abcdef0123456789abcdef' \
  -e 'postgres://' \
  "$receipt"; then
  fail "deployment receipt exposed a secret"
fi

: > "$temporary_directory/no-clobber.log"
expect_failure "existing deployment receipt" \
  env \
    COMMAND_LOG="$temporary_directory/no-clobber.log" \
    PATH="$fake_path" \
    SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    --receipt "$receipt"
jq --exit-status '.receipt_version == 1' "$receipt" >/dev/null ||
  fail "existing deployment receipt was changed"
if grep -Eq ' compose .* (pull|up|run|down) ' \
  "$temporary_directory/no-clobber.log"; then
  fail "existing receipt was rejected only after a deployment side effect"
fi

assert_failed_deploy_before_application() {
  local name=$1
  local log=$2
  local failed_receipt=$3
  shift 3

  : > "$log"
  expect_failure "$name" env \
    COMMAND_LOG="$log" \
    PATH="$fake_path" \
    SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
    "$@" \
    "$manage" deploy \
      --manifest "$temporary_directory/release-manifest.json" \
      --env-file "$temporary_directory/staging.env" \
      --receipt "$failed_receipt"
  [[ ! -e "$failed_receipt" && ! -L "$failed_receipt" ]] ||
    fail "$name wrote a deployment receipt"
  if grep -Fq \
    'up --pull never --detach --no-build --wait --wait-timeout 90 portal server' \
    "$log"; then
    fail "$name updated application containers"
  fi
}

assert_failed_deploy_before_application \
  "failed migration" \
  "$temporary_directory/migration-failure.log" \
  "$temporary_directory/migration-failure-receipt.json" \
  FAIL_MIGRATION=1
assert_failed_deploy_before_application \
  "dirty database schema" \
  "$temporary_directory/dirty-schema.log" \
  "$temporary_directory/dirty-schema-receipt.json" \
  'SCHEMA_OUTPUT=version=7 dirty=true'
assert_failed_deploy_before_application \
  "wrong database schema" \
  "$temporary_directory/wrong-schema.log" \
  "$temporary_directory/wrong-schema-receipt.json" \
  'SCHEMA_OUTPUT=version=6 dirty=false'
assert_failed_deploy_before_application \
  "multiple schema status lines" \
  "$temporary_directory/multiple-schema.log" \
  "$temporary_directory/multiple-schema-receipt.json" \
  $'SCHEMA_OUTPUT=version=7 dirty=false\nversion=7 dirty=false'

failed_runtime_receipt="$temporary_directory/runtime-failure-receipt.json"
expect_failure "runtime verification failure after update" \
  env \
    COMMAND_LOG="$temporary_directory/runtime-failure.log" \
    PATH="$fake_path" \
    SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
    TEST_BAD_RUNTIME_IMAGE=portal \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    --receipt "$failed_runtime_receipt"
[[ ! -e "$failed_runtime_receipt" ]] ||
  fail "runtime verification failure wrote a deployment receipt"

failed_health_receipt="$temporary_directory/health-failure-receipt.json"
expect_failure "loopback verification failure after update" \
  env \
    COMMAND_LOG="$temporary_directory/health-failure.log" \
    PATH="$fake_path" \
    SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
    FAIL_HEALTH=1 \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    --receipt "$failed_health_receipt"
[[ ! -e "$failed_health_receipt" ]] ||
  fail "loopback verification failure wrote a deployment receipt"

COMMAND_LOG="$temporary_directory/verify.log" \
PATH="$fake_path" \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    > "$temporary_directory/verify.out"
grep -Fq \
  'run --pull never --rm --no-deps migrate /usr/local/bin/speakup-migrate version' \
  "$temporary_directory/verify.log" ||
  fail "verify did not validate the live database schema without pulling"
if grep -Eq ' compose .* pull ' "$temporary_directory/verify.log"; then
  fail "verify attempted to pull an image"
fi
grep -Fq 'http://127.0.0.1:28083/readyz' \
  "$temporary_directory/verify.log" ||
  fail "verify did not check loopback endpoints"

while IFS='|' read -r name variable value; do
  runtime_log="$temporary_directory/runtime-negative.log"
  : > "$runtime_log"
  expect_failure "$name" env \
    COMMAND_LOG="$runtime_log" \
    PATH="$fake_path" \
    "$variable=$value" \
    "$manage" verify \
      --manifest "$temporary_directory/release-manifest.json" \
      --env-file "$temporary_directory/staging.env"
  if grep -Fq 'curl ' "$runtime_log"; then
    fail "$name reached endpoint checks before runtime identity validation"
  fi
done <<'EOF'
unexpected extra project container|TEST_EXTRA_SERVICE|1
wrong Compose project label|TEST_BAD_CONTAINER_LABEL|portal
wrong Portal image|TEST_BAD_RUNTIME_IMAGE|portal
wrong Server OCI release label|TEST_BAD_OCI_LABEL|server
wrong Portal image platform|TEST_BAD_IMAGE_PLATFORM|portal
unhealthy Server container|TEST_UNHEALTHY_SERVICE|server
wrong Portal loopback port|TEST_BAD_PORT_SERVICE|portal
wrong Server network membership|TEST_BAD_NETWORK_SERVICE|server
public database network|TEST_DATABASE_NETWORK_PUBLIC|1
wrong Portal volume mount|TEST_BAD_VOLUME_SERVICE|portal
wrong PostgreSQL volume label|TEST_BAD_VOLUME_LABEL|postgres_data
EOF

: > "$temporary_directory/schema-verify.log"
expect_failure "verify with wrong live schema" env \
  COMMAND_LOG="$temporary_directory/schema-verify.log" \
  PATH="$fake_path" \
  'SCHEMA_OUTPUT=version=6 dirty=false' \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
if grep -Fq 'curl ' "$temporary_directory/schema-verify.log"; then
  fail "verify checked endpoints after the schema mismatch"
fi

busy_receipt="$temporary_directory/busy-receipt.json"
: > "$temporary_directory/busy.log"
expect_failure "concurrent Staging deploy" env \
  COMMAND_LOG="$temporary_directory/busy.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  TEST_FLOCK_BUSY=1 \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env" \
    --receipt "$busy_receipt"
[[ ! -e "$busy_receipt" ]] || fail "concurrent deploy wrote a receipt"
if grep -Eq ' compose .* (pull|up|run|down) ' "$temporary_directory/busy.log"; then
  fail "concurrent deploy reached a mutation after lock failure"
fi

insecure_lock="$temporary_directory/insecure.lock"
: > "$insecure_lock"
chmod 0644 "$insecure_lock"
expect_failure "insecure deployment lock" env \
  COMMAND_LOG="$temporary_directory/insecure-lock.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$insecure_lock" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"

lock_target="$temporary_directory/lock-target"
: > "$lock_target"
chmod 0600 "$lock_target"
ln -s "$lock_target" "$temporary_directory/lock-link"
expect_failure "symbolic-link deployment lock" env \
  COMMAND_LOG="$temporary_directory/symlink-lock.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$temporary_directory/lock-link" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"

missing_lock_directory="$temporary_directory/missing-lock-directory"
expect_failure "missing dedicated deployment lock directory" env \
  COMMAND_LOG="$temporary_directory/missing-lock-directory.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$missing_lock_directory/deploy.lock" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
[[ ! -e "$missing_lock_directory" ]] ||
  fail "deployment created its missing lock directory"

unsafe_lock_directory="$temporary_directory/unsafe-lock-directory"
mkdir "$unsafe_lock_directory"
chmod 0777 "$unsafe_lock_directory"
expect_failure "world-writable deployment lock directory" env \
  COMMAND_LOG="$temporary_directory/unsafe-lock-directory.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$unsafe_lock_directory/deploy.lock" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
[[ ! -e "$unsafe_lock_directory/deploy.lock" ]] ||
  fail "deployment created a lock in a world-writable directory"

mkdir "$temporary_directory/real-lock-directory"
chmod 0700 "$temporary_directory/real-lock-directory"
ln -s "$temporary_directory/real-lock-directory" \
  "$temporary_directory/symlink-lock-directory"
expect_failure "symbolic-link deployment lock directory" env \
  COMMAND_LOG="$temporary_directory/symlink-lock-directory.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$temporary_directory/symlink-lock-directory/deploy.lock" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
[[ ! -e "$temporary_directory/real-lock-directory/deploy.lock" ]] ||
  fail "deployment followed a symbolic-link lock directory"

mkdir -p "$temporary_directory/real-lock-ancestor/nested"
chmod 0700 \
  "$temporary_directory/real-lock-ancestor" \
  "$temporary_directory/real-lock-ancestor/nested"
ln -s "$temporary_directory/real-lock-ancestor" \
  "$temporary_directory/symlink-lock-ancestor"
expect_failure "deployment lock with a symbolic-link ancestor" env \
  COMMAND_LOG="$temporary_directory/symlink-lock-ancestor.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$temporary_directory/symlink-lock-ancestor/nested/deploy.lock" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
[[ ! -e "$temporary_directory/real-lock-ancestor/nested/deploy.lock" ]] ||
  fail "deployment followed a symbolic-link lock ancestor"

mkdir "$temporary_directory/race-lock-directory"
chmod 0700 "$temporary_directory/race-lock-directory"
race_lock="$temporary_directory/race-lock-directory/deploy.lock"
race_target="$temporary_directory/race-lock-target"
: > "$race_target"
chmod 0600 "$race_target"
: > "$temporary_directory/race-lock.log"
expect_failure "deployment lock replaced during acquisition" env \
  COMMAND_LOG="$temporary_directory/race-lock.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$race_lock" \
  TEST_FLOCK_SWAP_SYMLINK=1 \
  TEST_LOCK_PATH="$race_lock" \
  TEST_LOCK_SWAP_TARGET="$race_target" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
[[ -L "$race_lock" ]] || fail "lock replacement boundary was not exercised"
if grep -Fq ' down ' "$temporary_directory/race-lock.log"; then
  fail "deployment continued after the lock path was replaced"
fi

: > "$temporary_directory/down-busy.log"
expect_failure "concurrent Staging down" env \
  COMMAND_LOG="$temporary_directory/down-busy.log" \
  PATH="$fake_path" \
  SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  TEST_FLOCK_BUSY=1 \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
if grep -Fq ' compose ' "$temporary_directory/down-busy.log" &&
  grep -Fq ' down ' "$temporary_directory/down-busy.log"; then
  fail "concurrent down mutated the Staging project"
fi

COMMAND_LOG="$temporary_directory/down.log" \
PATH="$fake_path" \
SPEAKUP_STAGING_LOCK_FILE="$lock_file" \
  "$manage" down \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/staging.env"
grep -Fq 'flock --nonblock 9' "$temporary_directory/down.log" ||
  fail "down did not acquire the shared Staging lock"
grep -Fq 'down --remove-orphans' "$temporary_directory/down.log" ||
  fail "down did not target the isolated Staging project"

printf '%s\n' 'staging deploy contract tests passed'
