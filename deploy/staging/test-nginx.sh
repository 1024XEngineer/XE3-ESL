#!/usr/bin/env bash

set -euo pipefail

readonly staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$staging_directory/manage.sh"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
readonly container_fixture_directory="/tmp/staging-nginx-test"
readonly container_htpasswd_file="/etc/nginx/staging.htpasswd"
readonly runtime_container="xe3-staging-nginx-test-${PPID}-$$"

cleanup() {
  docker rm --force "$runtime_container" >/dev/null 2>&1 || true
  rm -rf "$temporary_directory"
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
  printf '%s\n' 'docker is required for the Nginx configuration check' >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  printf '%s\n' 'openssl is required for the Nginx configuration check' >&2
  exit 1
}

temporary_directory=$(mktemp -d)
readonly temporary_directory
trap cleanup EXIT

mkdir -p "$temporary_directory/acme" \
  "$temporary_directory/public/downloads/android/v0.1.0"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$temporary_directory/server.env"
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

printf '%s\n' \
  'STAGING_POSTGRES_DB=speakup_staging' \
  'STAGING_POSTGRES_USER=speakup_staging' \
  'STAGING_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
  'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
  "STAGING_SERVER_ENV_FILE=$temporary_directory/server.env" \
  'STAGING_PORTAL_HOST=staging.speak-up.top' \
  'STAGING_API_HOST=staging-api.speak-up.top' \
  "STAGING_TLS_CERTIFICATE=$container_fixture_directory/fullchain.pem" \
  "STAGING_TLS_CERTIFICATE_KEY=$container_fixture_directory/privkey.pem" \
  "STAGING_HTPASSWD_FILE=$container_htpasswd_file" \
  "STAGING_ACME_ROOT=$container_fixture_directory/acme" \
  "STAGING_PUBLIC_ROOT=$temporary_directory/public" \
  >"$temporary_directory/staging.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/staging.env" \
  --output "$temporary_directory/default.conf" \
  >/dev/null

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
  --volume "$temporary_directory:$container_fixture_directory:ro" \
  --volume "$temporary_directory/staging.htpasswd:$container_htpasswd_file:ro" \
  --volume "$temporary_directory/public:$temporary_directory/public:ro" \
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

docker run --detach \
  --name "$runtime_container" \
  --publish 127.0.0.1::443 \
  --volume "$temporary_directory:$container_fixture_directory:ro" \
  --volume "$temporary_directory/staging.htpasswd:$container_htpasswd_file:ro" \
  --volume "$temporary_directory/public:$temporary_directory/public:ro" \
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
  status=$(curl \
    --insecure \
    --silent \
    --output /dev/null \
    --write-out '%{http_code}' \
    --resolve "staging.speak-up.top:$https_port:127.0.0.1" \
    "https://staging.speak-up.top:$https_port/")
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
