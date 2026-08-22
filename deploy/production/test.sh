#!/usr/bin/env bash

set -euo pipefail

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$production_directory/manage.sh"
readonly compose_file="$production_directory/compose.yaml"
readonly portal_digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
readonly server_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
readonly git_sha="cccccccccccccccccccccccccccccccccccccccc"
readonly staging_apk_sha="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
readonly production_apk_sha="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
readonly certificate_sha="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

fail() {
  printf 'production contract test: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local name=$1
  shift
  if "$@" > "$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

write_environment() {
  local destination=$1
  local server_environment=$2
  local certificate=${3:-$temporary_directory/fullchain.pem}
  local certificate_key=${4:-$temporary_directory/privkey.pem}

  printf '%s\n' \
    'PRODUCTION_POSTGRES_DB=speakup' \
    'PRODUCTION_POSTGRES_USER=speakup' \
    'PRODUCTION_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
    'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
    "PRODUCTION_SERVER_ENV_FILE=$server_environment" \
    'PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=172.31.0.1/32' \
    'PRODUCTION_PORTAL_HOST=speak-up.top' \
    'PRODUCTION_PORTAL_REDIRECT_HOST=www.speak-up.top' \
    'PRODUCTION_API_HOST=api.speak-up.top' \
    "PRODUCTION_TLS_CERTIFICATE=$certificate" \
    "PRODUCTION_TLS_CERTIFICATE_KEY=$certificate_key" \
    "PRODUCTION_ACME_ROOT=$temporary_directory/acme" > "$destination"
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
    '}' > "$destination"
}

temporary_directory=$(mktemp -d)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$temporary_directory/acme" "$temporary_directory/fake-bin"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' > "$temporary_directory/server.env"
printf '%s\n' 'test-certificate-placeholder' > "$temporary_directory/fullchain.pem"
printf '%s\n' 'test-key-placeholder' > "$temporary_directory/privkey.pem"
chmod 0600 "$temporary_directory/server.env" "$temporary_directory/privkey.pem"
write_environment "$temporary_directory/production.env" "$temporary_directory/server.env"
write_manifest "$temporary_directory/release-manifest.json"

real_docker="$(command -v docker)"
readonly real_docker
real_stat="$(command -v stat)"
readonly real_stat
cat > "$temporary_directory/fake-bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == volume && "${2:-}" == inspect ]]; then
  volume=${3:-}
  [[ "$volume" == xe3-speakup-portal-data || "$volume" == xe3-speakup-postgres-data ]] || exit 1
  [[ "${TEST_MISSING_VOLUME:-}" != "$volume" ]]
  exit
fi
if [[ "${1:-}" == network && "${2:-}" == inspect ]]; then
  [[ "${3:-}" == xe3-speakup-production-server-edge ]] || exit 1
  [[ "${TEST_MISSING_NETWORK:-0}" != 1 ]] || exit 1
  jq --null-input \
    --arg gateway "${TEST_RUNTIME_GATEWAY:-172.31.0.1}" '
      [{
        Name: "xe3-speakup-production-server-edge",
        IPAM: {Config: [{Subnet: "172.31.0.0/24", Gateway: $gateway}]}
      }]
    '
  exit
fi
if [[ "${1:-}" == ps ]]; then
  service=""
  requested_project=""
  shift
  while (($# > 0)); do
    case "$1" in
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
  runtime_project=${TEST_RUNTIME_PROJECT:-xe3-speakup-production}
  [[ "$requested_project" == "$runtime_project" ]] || exit 0
  [[ "$service" != "${TEST_MISSING_SERVICE:-}" ]] || exit 0
  case "$service" in
    portal) printf '%s\n' aaaaaaaaaaaa ;;
    server) printf '%s\n' bbbbbbbbbbbb ;;
    postgres) printf '%s\n' cccccccccccc ;;
    *) exit 2 ;;
  esac
  exit
fi
if [[ "${1:-}" == inspect && "$#" == 2 ]]; then
  case "$2" in
    aaaaaaaaaaaa)
      service=portal
      image="ghcr.io/1024xengineer/xe3-esl-portal@${TEST_RUNTIME_PORTAL_DIGEST:-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
      mounts='[{"Type":"volume","Name":"xe3-speakup-portal-data","Destination":"/app/.wrangler","RW":true}]'
      ports='{"3000/tcp":[{"HostIp":"127.0.0.1","HostPort":"18082"}]}'
      networks='{"xe3-speakup-production_portal_edge":{}}'
      environment='["NODE_ENV=production","PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests","PORTAL_SQLITE_PATH=/app/.wrangler/portal.sqlite","VINEXT_TRUSTED_HOSTS=speak-up.top"]'
      ;;
    bbbbbbbbbbbb)
      service=server
      image="ghcr.io/1024xengineer/xe3-esl-server@${TEST_RUNTIME_SERVER_DIGEST:-sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}"
      mounts='[]'
      ports='{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"18083"}]}'
      networks='{"xe3-speakup-production-server-edge":{},"xe3-speakup-production_database":{}}'
      environment='["TEXT_GENERATION_PROVIDER=test-fixture","DATABASE_URL=postgres://speakup:0123456789abcdef0123456789abcdef@postgres:5432/speakup?sslmode=disable","SERVER_HOST=0.0.0.0","SERVER_PORT=8080","TRUSTED_PROXY_CIDRS=172.31.0.1/32","TRUSTED_PROXY_HEADER=x-forwarded-for"]'
      ;;
    cccccccccccc)
      service=postgres
      image='postgres:18-bookworm@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
      mounts='[{"Type":"volume","Name":"xe3-speakup-postgres-data","Destination":"/var/lib/postgresql","RW":true}]'
      ports='{"5432/tcp":null}'
      networks='{"xe3-speakup-production_database":{}}'
      environment='["PGDATA=/var/lib/postgresql/18/docker","POSTGRES_DB=speakup","POSTGRES_USER=speakup","POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef"]'
      ;;
    *) exit 1 ;;
  esac
  health=healthy
  [[ "${TEST_UNHEALTHY_SERVICE:-}" != "$service" ]] || health=unhealthy
  [[ "${TEST_BAD_MOUNT_SERVICE:-}" != "$service" ]] || mounts='[]'
  [[ "${TEST_BAD_PORT_SERVICE:-}" != "$service" ]] || ports='{}'
  [[ "${TEST_BAD_NETWORK_SERVICE:-}" != "$service" ]] || networks='{}'
  if [[ "${TEST_BAD_ENV_SERVICE:-}" == "$service" ]]; then
    case "$service" in
      portal)
        environment='["PORTAL_ADMIN_PASSWORD=wrong","PORTAL_SQLITE_PATH=/app/.wrangler/portal.sqlite","VINEXT_TRUSTED_HOSTS=speak-up.top"]'
        ;;
      server)
        environment='["DATABASE_URL=postgres://wrong","SERVER_HOST=0.0.0.0","SERVER_PORT=8080","TRUSTED_PROXY_CIDRS=172.31.0.1/32","TRUSTED_PROXY_HEADER=x-forwarded-for"]'
        ;;
      postgres)
        environment='["POSTGRES_DB=speakup","POSTGRES_USER=speakup","POSTGRES_PASSWORD=wrong"]'
        ;;
    esac
  fi
  jq --null-input \
    --arg id "$2" \
    --arg project "${TEST_RUNTIME_PROJECT:-xe3-speakup-production}" \
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
exec "$TEST_REAL_DOCKER" "$@"
EOF
cat > "$temporary_directory/fake-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'curl %s\n' "$*" >> "$COMMAND_LOG"
[[ "${FAIL_HEALTH:-0}" != 1 ]]
EOF
cat > "$temporary_directory/fake-bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$temporary_directory/fake-bin/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${TEST_TLS_KEY_OWNER_MISMATCH:-0}" == 1 ]]; then
  case "${1:-}:${2:-}" in
    -c:%u | -f:%u)
      printf '%s\n' 999999
      exit 0
      ;;
  esac
fi
exec "$TEST_REAL_STAT" "$@"
EOF
chmod +x \
  "$temporary_directory/fake-bin/docker" \
  "$temporary_directory/fake-bin/curl" \
  "$temporary_directory/fake-bin/sleep" \
  "$temporary_directory/fake-bin/stat"

export TEST_REAL_DOCKER="$real_docker"
export TEST_REAL_STAT="$real_stat"
export PATH="$temporary_directory/fake-bin:$PATH"

bash -n "$manage" "$0"

"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/production.env" \
  > "$temporary_directory/validate.out"
grep -Fq 'validated=true' "$temporary_directory/validate.out" ||
  fail "valid Production contract was not accepted"

expect_failure "missing existing Portal data volume" \
  env TEST_MISSING_VOLUME=xe3-speakup-portal-data \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
expect_failure "missing existing PostgreSQL data volume" \
  env TEST_MISSING_VOLUME=xe3-speakup-postgres-data \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
expect_failure "missing Server edge network" \
  env TEST_MISSING_NETWORK=1 \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
expect_failure "mismatched Server edge gateway" \
  env TEST_RUNTIME_GATEWAY=172.31.0.2 \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"

sed 's/^PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=.*/PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=172.031.0.1\/32/' \
  "$temporary_directory/production.env" > "$temporary_directory/invalid-gateway.env"
chmod 0600 "$temporary_directory/invalid-gateway.env"
expect_failure "non-canonical Server edge gateway" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/invalid-gateway.env"

write_manifest "$temporary_directory/invalid-manifest.json" latest
expect_failure "mutable Portal image reference" \
  "$manage" validate \
    --manifest "$temporary_directory/invalid-manifest.json" \
    --env-file "$temporary_directory/production.env"

write_manifest \
  "$temporary_directory/zero-digest-manifest.json" \
  'sha256:0000000000000000000000000000000000000000000000000000000000000000'
expect_failure "placeholder Portal digest" \
  "$manage" validate \
    --manifest "$temporary_directory/zero-digest-manifest.json" \
    --env-file "$temporary_directory/production.env"

jq 'del(.quality_run_url)' \
  "$temporary_directory/release-manifest.json" > "$temporary_directory/incomplete-manifest.json"
expect_failure "incomplete release manifest" \
  "$manage" validate \
    --manifest "$temporary_directory/incomplete-manifest.json" \
    --env-file "$temporary_directory/production.env"

printf '%s\n%s\n' \
  "$(< "$temporary_directory/release-manifest.json")" \
  "$(< "$temporary_directory/release-manifest.json")" \
  > "$temporary_directory/multiple-manifests.json"
expect_failure "multiple JSON documents" \
  "$manage" validate \
    --manifest "$temporary_directory/multiple-manifests.json" \
    --env-file "$temporary_directory/production.env"

jq '.unexpected = true' \
  "$temporary_directory/release-manifest.json" > "$temporary_directory/extended-manifest.json"
expect_failure "non-canonical release manifest field" \
  "$manage" validate \
    --manifest "$temporary_directory/extended-manifest.json" \
    --env-file "$temporary_directory/production.env"

expect_failure "missing release manifest" \
  "$manage" validate \
    --manifest "$temporary_directory/missing.json" \
    --env-file "$temporary_directory/production.env"
expect_failure "missing environment file" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/not-there.env"
expect_failure "manifest directory path" \
  "$manage" validate \
    --manifest "$temporary_directory" \
    --env-file "$temporary_directory/production.env"
expect_failure "environment directory path" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory"

: > "$temporary_directory/empty-manifest.json"
expect_failure "empty release manifest" \
  "$manage" validate \
    --manifest "$temporary_directory/empty-manifest.json" \
    --env-file "$temporary_directory/production.env"

cp "$temporary_directory/production.env" "$temporary_directory/insecure-production.env"
chmod 0640 "$temporary_directory/insecure-production.env"
expect_failure "group-readable Production environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-production.env"

sed 's/^PORTAL_ADMIN_PASSWORD=.*/PORTAL_ADMIN_PASSWORD=/' \
  "$temporary_directory/production.env" > "$temporary_directory/missing-value.env"
chmod 0600 "$temporary_directory/missing-value.env"
expect_failure "missing required environment value" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/missing-value.env"

sed '/^PORTAL_ADMIN_PASSWORD=/d' \
  "$temporary_directory/production.env" > "$temporary_directory/missing-key.env"
chmod 0600 "$temporary_directory/missing-key.env"
expect_failure "ambient value replacing a missing environment key" \
  env PORTAL_ADMIN_PASSWORD=ambient-password-must-not-be-used \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/missing-key.env"

printf '%s\n' \
  "$(< "$temporary_directory/production.env")" \
  'UNKNOWN_SETTING=typo' > "$temporary_directory/unknown.env"
chmod 0600 "$temporary_directory/unknown.env"
expect_failure "unknown environment setting" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/unknown.env"

write_environment "$temporary_directory/missing-server.env" "$temporary_directory/not-there.env"
expect_failure "missing Server environment file" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/missing-server.env"

write_environment "$temporary_directory/server-directory.env" "$temporary_directory"
expect_failure "Server environment directory path" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/server-directory.env"

cp "$temporary_directory/server.env" "$temporary_directory/insecure-server.env"
chmod 0644 "$temporary_directory/insecure-server.env"
write_environment \
  "$temporary_directory/insecure-server-config.env" \
  "$temporary_directory/insecure-server.env"
expect_failure "world-readable Server environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-server-config.env"

ln -s "$temporary_directory/server.env" "$temporary_directory/server-link.env"
write_environment \
  "$temporary_directory/server-link-config.env" \
  "$temporary_directory/server-link.env"
expect_failure "symbolic-link Server environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/server-link-config.env"

ln -s "$temporary_directory/production.env" "$temporary_directory/production-link.env"
expect_failure "symbolic-link Production environment" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production-link.env"

write_environment \
  "$temporary_directory/missing-certificate.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/not-there.pem"
expect_failure "missing TLS certificate" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/missing-certificate.env"

write_environment \
  "$temporary_directory/certificate-directory.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory"
expect_failure "TLS certificate directory path" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/certificate-directory.env"

write_environment \
  "$temporary_directory/key-directory.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory"
expect_failure "TLS certificate key directory path" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/key-directory.env"

cp "$temporary_directory/privkey.pem" "$temporary_directory/insecure-key.pem"
chmod 0644 "$temporary_directory/insecure-key.pem"
write_environment \
  "$temporary_directory/insecure-key.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/insecure-key.pem"
expect_failure "world-readable TLS certificate key" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/insecure-key.env"

mkdir -p \
  "$temporary_directory/certbot/archive/speak-up.top" \
  "$temporary_directory/certbot/live/speak-up.top"
cp \
  "$temporary_directory/privkey.pem" \
  "$temporary_directory/certbot/archive/speak-up.top/privkey1.pem"
chmod 0600 "$temporary_directory/certbot/archive/speak-up.top/privkey1.pem"
ln -s \
  ../../archive/speak-up.top/privkey1.pem \
  "$temporary_directory/certbot/live/speak-up.top/privkey.pem"
write_environment \
  "$temporary_directory/symlink-key.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/certbot/live/speak-up.top/privkey.pem"
"$manage" validate \
  --manifest "$temporary_directory/release-manifest.json" \
  --env-file "$temporary_directory/symlink-key.env" \
  > "$temporary_directory/symlink-key.out"
grep -Fq 'validated=true' "$temporary_directory/symlink-key.out" ||
  fail "stable Certbot live private-key symbolic link was not accepted"

chmod 0644 "$temporary_directory/certbot/archive/speak-up.top/privkey1.pem"
expect_failure "world-readable TLS certificate key target" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/symlink-key.env"
chmod 0600 "$temporary_directory/certbot/archive/speak-up.top/privkey1.pem"

expect_failure "TLS certificate key target owned by another user" \
  env \
    TEST_TLS_KEY_OWNER_MISMATCH=1 \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/symlink-key.env"

ln -s \
  ../../archive/speak-up.top/missing.pem \
  "$temporary_directory/certbot/live/speak-up.top/missing.pem"
write_environment \
  "$temporary_directory/dangling-key.env" \
  "$temporary_directory/server.env" \
  "$temporary_directory/fullchain.pem" \
  "$temporary_directory/certbot/live/speak-up.top/missing.pem"
expect_failure "dangling TLS certificate key symbolic link" \
  "$manage" validate \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/dangling-key.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/production.env" \
  --output "$temporary_directory/production.conf" \
  > "$temporary_directory/render.out"
if grep -Eq '__PRODUCTION_[A-Z_]+' "$temporary_directory/production.conf"; then
  fail "rendered Nginx configuration contains a placeholder"
fi
grep -Fq 'server_name speak-up.top;' "$temporary_directory/production.conf" ||
  fail "rendered Nginx configuration is missing the canonical Portal host"
grep -Fq 'server_name api.speak-up.top;' "$temporary_directory/production.conf" ||
  fail "rendered Nginx configuration is missing the API host"

PRODUCTION_POSTGRES_DB=speakup \
PRODUCTION_POSTGRES_USER=speakup \
PRODUCTION_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef \
PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests \
PRODUCTION_SERVER_ENV_FILE="$temporary_directory/server.env" \
PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=172.31.0.1/32 \
PRODUCTION_PORTAL_HOST=speak-up.top \
PORTAL_IMAGE_DIGEST="$portal_digest" \
SERVER_IMAGE_DIGEST="$server_digest" \
COMPOSE_PROJECT_NAME=untrusted-project \
  "$real_docker" compose \
    --env-file /dev/null \
    --project-name xe3-speakup-production \
    --file "$compose_file" \
    --profile migration \
    config --format json > "$temporary_directory/compose.json"

jq --exit-status \
  --arg portal_digest "$portal_digest" \
  --arg server_digest "$server_digest" '
    .name == "xe3-speakup-production" and
    .services.portal.image ==
      ("ghcr.io/1024xengineer/xe3-esl-portal@" + $portal_digest) and
    .services.server.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    .services.migrate.image ==
      ("ghcr.io/1024xengineer/xe3-esl-server@" + $server_digest) and
    ([.services[] | has("build")] | any | not) and
    ([.services[] | has("container_name")] | any | not) and
    .services.portal.ports[0].host_ip == "127.0.0.1" and
    (.services.portal.ports[0].published | tostring) == "18082" and
    .services.server.ports[0].host_ip == "127.0.0.1" and
    (.services.server.ports[0].published | tostring) == "18083" and
    (.services.postgres | has("ports") | not) and
    .networks.database.internal == true and
    .networks.server_edge.external == true and
    .networks.server_edge.name == "xe3-speakup-production-server-edge" and
    (.services.portal.networks | keys) == ["portal_edge"] and
    (.services.postgres.networks | keys) == ["database"] and
    (.services.migrate.networks | keys) == ["database"] and
    (.services.server.networks | keys | sort) == ["database", "server_edge"] and
    .services.server.environment.TRUSTED_PROXY_CIDRS == "172.31.0.1/32" and
    .services.server.environment.TRUSTED_PROXY_HEADER == "x-forwarded-for" and
    .services.portal.healthcheck.test == [
      "CMD",
      "node",
      "-e",
      "fetch(\u0027http://127.0.0.1:3000/\u0027,{signal:AbortSignal.timeout(2000)}).then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
    ] and
    .services.server.healthcheck.test == [
      "CMD", "wget", "-q", "-T", "2", "--spider",
      "http://127.0.0.1:8080/readyz"
    ] and
    .services.postgres.healthcheck.test == [
      "CMD-SHELL",
      "pg_isready --username=\"$${POSTGRES_USER}\" --dbname=\"$${POSTGRES_DB}\""
    ] and
    .services.server.depends_on.postgres.condition == "service_healthy" and
    .services.migrate.depends_on.postgres.condition == "service_healthy" and
    .volumes.portal_data.external == true and
    .volumes.portal_data.name == "xe3-speakup-portal-data" and
    .volumes.postgres_data.external == true and
    .volumes.postgres_data.name == "xe3-speakup-postgres-data"
  ' "$temporary_directory/compose.json" >/dev/null ||
  fail "resolved Compose model violates the Production isolation contract"

COMMAND_LOG="$temporary_directory/verify.log" \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env" \
    > "$temporary_directory/verify.out"
grep -Fq 'http://127.0.0.1:18082/' "$temporary_directory/verify.log" ||
  fail "Production verification did not check Portal"
grep -Fq 'http://127.0.0.1:18083/health' "$temporary_directory/verify.log" ||
  fail "Production verification did not check Server liveness"
grep -Fq 'http://127.0.0.1:18083/readyz' "$temporary_directory/verify.log" ||
  fail "Production verification did not check Server readiness"

: > "$temporary_directory/wrong-project.log"
expect_failure "healthy legacy process outside the Production project" \
  env \
    COMMAND_LOG="$temporary_directory/wrong-project.log" \
    TEST_RUNTIME_PROJECT=xe3-speakup-legacy \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/wrong-project.log" ]] ||
  fail "endpoint checks ran before the Production project identity check"

: > "$temporary_directory/wrong-digest.log"
expect_failure "Portal running an unapproved digest" \
  env \
    COMMAND_LOG="$temporary_directory/wrong-digest.log" \
    TEST_RUNTIME_PORTAL_DIGEST=sha256:9999999999999999999999999999999999999999999999999999999999999999 \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/wrong-digest.log" ]] ||
  fail "endpoint checks ran before the image digest check"

: > "$temporary_directory/unhealthy.log"
expect_failure "unhealthy Production Server" \
  env \
    COMMAND_LOG="$temporary_directory/unhealthy.log" \
    TEST_UNHEALTHY_SERVICE=server \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/unhealthy.log" ]] ||
  fail "endpoint checks ran before the container health check"

: > "$temporary_directory/bad-mount.log"
expect_failure "Portal missing its external data mount" \
  env \
    COMMAND_LOG="$temporary_directory/bad-mount.log" \
    TEST_BAD_MOUNT_SERVICE=portal \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/bad-mount.log" ]] ||
  fail "endpoint checks ran before the external mount check"

: > "$temporary_directory/bad-port.log"
expect_failure "Portal missing its loopback port" \
  env \
    COMMAND_LOG="$temporary_directory/bad-port.log" \
    TEST_BAD_PORT_SERVICE=portal \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/bad-port.log" ]] ||
  fail "endpoint checks ran before the loopback port check"

: > "$temporary_directory/bad-network.log"
expect_failure "Server missing its audited edge network" \
  env \
    COMMAND_LOG="$temporary_directory/bad-network.log" \
    TEST_BAD_NETWORK_SERVICE=server \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/bad-network.log" ]] ||
  fail "endpoint checks ran before the runtime network check"

for service in portal server postgres; do
  runtime_environment_log="$temporary_directory/bad-${service}-environment.log"
  : > "$runtime_environment_log"
  expect_failure "$service runtime environment mismatch" \
    env \
      COMMAND_LOG="$runtime_environment_log" \
      TEST_BAD_ENV_SERVICE="$service" \
    "$manage" verify \
      --manifest "$temporary_directory/release-manifest.json" \
      --env-file "$temporary_directory/production.env"
  [[ ! -s "$runtime_environment_log" ]] ||
    fail "endpoint checks ran before the $service runtime environment check"
  if grep -Fq \
    -e 'portal-admin-password-for-tests' \
    -e '0123456789abcdef0123456789abcdef' \
    -e 'postgres://speakup:' \
    "$temporary_directory/failure.out"; then
    fail "$service runtime environment failure exposed a sensitive value"
  fi
done

: > "$temporary_directory/missing-postgres.log"
expect_failure "missing Production PostgreSQL container" \
  env \
    COMMAND_LOG="$temporary_directory/missing-postgres.log" \
    TEST_MISSING_SERVICE=postgres \
  "$manage" verify \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"
[[ ! -s "$temporary_directory/missing-postgres.log" ]] ||
  fail "endpoint checks ran before all Production services were identified"

expect_failure "unsupported deployment mutation" \
  "$manage" deploy \
    --manifest "$temporary_directory/release-manifest.json" \
    --env-file "$temporary_directory/production.env"

printf '%s\n' 'Production contract tests passed'
