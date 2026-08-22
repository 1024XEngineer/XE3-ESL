#!/usr/bin/env bash

set -euo pipefail

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$production_directory/manage.sh"
readonly nginx_image="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' 'docker is required for the Nginx configuration check' >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  printf '%s\n' 'openssl is required for the Nginx configuration check' >&2
  exit 1
}

temporary_directory=$(mktemp -d /tmp/production-nginx-test.XXXXXX)
readonly temporary_directory
readonly rendered_configuration="$temporary_directory/default.conf"
readonly upstream_configuration="$temporary_directory/upstream-stub.conf"
readonly runtime_container="xe3-production-nginx-test-${PPID}-$$"

cleanup() {
  docker rm --force "$runtime_container" >/dev/null 2>&1 || true
  rm -rf "$temporary_directory"
}
trap cleanup EXIT

assert_contains() {
  local expected=$1
  grep -Fq -- "$expected" "$rendered_configuration" || {
    printf 'missing expected Nginx directive: %s\n' "$expected" >&2
    exit 1
  }
}

assert_count() {
  local expected=$1
  local wanted=$2
  local actual

  actual=$(grep -Fc -- "$expected" "$rendered_configuration" || true)
  [[ "$actual" == "$wanted" ]] || {
    printf 'unexpected count for Nginx directive %s: wanted %s, got %s\n' \
      "$expected" "$wanted" "$actual" >&2
    exit 1
  }
}

assert_response_header() {
  local response_file=$1
  local expected=$2

  grep -Fqi -- "$expected" "$response_file" || {
    printf 'missing expected response header: %s\n' "$expected" >&2
    return 1
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

mkdir -p "$temporary_directory/acme"
mkdir -p "$temporary_directory/logs"
mkdir -p "$temporary_directory/public/downloads/android/v0.1.0"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$temporary_directory/server.env"
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
chmod 600 "$temporary_directory/server.env"
openssl req \
  -x509 \
  -newkey rsa:2048 \
  -nodes \
  -subj '/CN=speak-up.top' \
  -days 1 \
  -keyout "$temporary_directory/privkey.pem" \
  -out "$temporary_directory/fullchain.pem" \
  >/dev/null 2>&1
chmod 600 "$temporary_directory/privkey.pem"

printf '%s\n' \
  'PRODUCTION_POSTGRES_DB=speakup_production' \
  'PRODUCTION_POSTGRES_USER=speakup_production' \
  'PRODUCTION_POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef' \
  'PORTAL_ADMIN_PASSWORD=portal-admin-password-for-tests' \
  "PRODUCTION_SERVER_ENV_FILE=$temporary_directory/server.env" \
  'PRODUCTION_SERVER_EDGE_GATEWAY_CIDR=172.31.253.1/32' \
  'PRODUCTION_PORTAL_HOST=speak-up.top' \
  'PRODUCTION_PORTAL_REDIRECT_HOST=www.speak-up.top' \
  'PRODUCTION_API_HOST=api.speak-up.top' \
  "PRODUCTION_TLS_CERTIFICATE=$temporary_directory/fullchain.pem" \
  "PRODUCTION_TLS_CERTIFICATE_KEY=$temporary_directory/privkey.pem" \
  "PRODUCTION_ACME_ROOT=$temporary_directory/acme" \
  "PRODUCTION_PUBLIC_ROOT=$temporary_directory/public" \
  >"$temporary_directory/production.env"
chmod 600 "$temporary_directory/production.env"

"$manage" render-nginx \
  --env-file "$temporary_directory/production.env" \
  --output "$rendered_configuration" \
  >/dev/null

assert_count 'server_name speak-up.top;' 2
assert_count 'server_name api.speak-up.top;' 2
assert_count 'server_name www.speak-up.top;' 2
assert_count 'return 301 https://speak-up.top$request_uri;' 3
assert_count 'return 301 https://api.speak-up.top$request_uri;' 1
assert_count "root $temporary_directory/acme;" 3
assert_count "root $temporary_directory/public;" 4
assert_count "ssl_certificate $temporary_directory/fullchain.pem;" 3
assert_count "ssl_certificate_key $temporary_directory/privkey.pem;" 3
assert_count 'location = /metrics {' 2
assert_count 'proxy_pass http://127.0.0.1:18082;' 4
assert_count 'proxy_pass http://127.0.0.1:18083;' 1
assert_contains 'limit_req zone=speakup_portal_events burst=10 nodelay;'
assert_contains 'limit_req zone=speakup_portal_waitlist burst=3 nodelay;'
assert_contains 'limit_req zone=speakup_portal_admin burst=3 nodelay;'
assert_count 'proxy_set_header Host $host;' 5
assert_count 'proxy_set_header X-Real-IP $remote_addr;' 5
assert_count 'proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;' 5
assert_count 'proxy_set_header X-Forwarded-Proto $scheme;' 5
assert_count 'proxy_set_header Authorization $http_authorization;' 1
assert_count 'proxy_set_header Upgrade $http_upgrade;' 1
assert_count 'proxy_set_header Connection $speakup_production_connection_upgrade;' 1
assert_contains 'access_log logs/xe3-speakup-production-portal.access.log;'
assert_contains 'access_log logs/xe3-speakup-production-api.access.log;'
assert_contains 'default_type application/vnd.android.package-archive;'
assert_contains 'add_header Cache-Control "no-store" always;'
assert_contains 'add_header Cache-Control "public, max-age=31536000, immutable" always;'
assert_contains 'location ^~ /downloads/android/'

if grep -Eq '__PRODUCTION_[A-Z_]+__' "$rendered_configuration"; then
  printf '%s\n' 'rendered Nginx configuration contains a placeholder' >&2
  exit 1
fi

docker run --rm \
  --volume "$temporary_directory/acme:$temporary_directory/acme:ro" \
  --volume "$temporary_directory/fullchain.pem:$temporary_directory/fullchain.pem:ro" \
  --volume "$temporary_directory/privkey.pem:$temporary_directory/privkey.pem:ro" \
  --volume "$temporary_directory/logs:/etc/nginx/logs" \
  --volume "$temporary_directory/public:$temporary_directory/public:ro" \
  --volume "$rendered_configuration:/etc/nginx/conf.d/default.conf:ro" \
  "$nginx_image" \
  nginx -t

printf '%s\n' \
  'server {' \
  '    listen 18082;' \
  '    location / {' \
  '        add_header X-SpeakUp-Upstream portal always;' \
  '        return 200 "portal\n";' \
  '    }' \
  '}' \
  'server {' \
  '    listen 18083;' \
  '    location / {' \
  '        add_header X-SpeakUp-Upstream api always;' \
  '        add_header X-Seen-Authorization $http_authorization always;' \
  '        add_header X-Seen-Upgrade $http_upgrade always;' \
  '        add_header X-Seen-Connection $http_connection always;' \
  '        add_header X-Seen-XFF $http_x_forwarded_for always;' \
  '        add_header X-Seen-Host $http_host always;' \
  '        return 200 "api\n";' \
  '    }' \
  '}' \
  >"$upstream_configuration"

docker run --detach \
  --name "$runtime_container" \
  --publish 127.0.0.1::80 \
  --publish 127.0.0.1::443 \
  --volume "$temporary_directory/acme:$temporary_directory/acme:ro" \
  --volume "$temporary_directory/fullchain.pem:$temporary_directory/fullchain.pem:ro" \
  --volume "$temporary_directory/privkey.pem:$temporary_directory/privkey.pem:ro" \
  --volume "$temporary_directory/logs:/etc/nginx/logs" \
  --volume "$temporary_directory/public:$temporary_directory/public:ro" \
  --volume "$rendered_configuration:/etc/nginx/conf.d/default.conf:ro" \
  --volume "$upstream_configuration:/etc/nginx/conf.d/upstream-stub.conf:ro" \
  "$nginx_image" \
  >/dev/null

http_port=$(docker port "$runtime_container" 80/tcp | sed -n 's/.*://p')
https_port=$(docker port "$runtime_container" 443/tcp | sed -n 's/.*://p')
readonly http_port https_port
[[ "$http_port" =~ ^[0-9]+$ && "$https_port" =~ ^[0-9]+$ ]] || {
  printf '%s\n' 'failed to resolve runtime Nginx ports' >&2
  exit 1
}

runtime_ready=false
for _ in {1..30}; do
  if curl \
    --fail \
    --insecure \
    --silent \
    --output /dev/null \
    --resolve "speak-up.top:$https_port:127.0.0.1" \
    "https://speak-up.top:$https_port/"; then
    runtime_ready=true
    break
  fi
  sleep 0.2
done
[[ "$runtime_ready" == true ]] || {
  docker logs "$runtime_container" >&2
  printf '%s\n' 'runtime Nginx did not become ready' >&2
  exit 1
}

redirect_headers="$temporary_directory/redirect.headers"
curl \
  --silent \
  --show-error \
  --dump-header "$redirect_headers" \
  --output /dev/null \
  --header 'Host: attacker.example' \
  "http://127.0.0.1:$http_port/release?source=test"
grep -Fqi 'Location: https://speak-up.top/release?source=test' "$redirect_headers" || {
  printf '%s\n' 'HTTP redirect did not use the fixed canonical host' >&2
  exit 1
}
if grep -Fqi 'attacker.example' "$redirect_headers"; then
  printf '%s\n' 'HTTP redirect reflected an untrusted Host header' >&2
  exit 1
fi

for host in speak-up.top api.speak-up.top; do
  metrics_status=$(curl \
    --insecure \
    --silent \
    --output /dev/null \
    --write-out '%{http_code}' \
    --resolve "$host:$https_port:127.0.0.1" \
    "https://$host:$https_port/metrics")
  [[ "$metrics_status" == 404 ]] || {
    printf '%s /metrics returned %s instead of 404\n' "$host" "$metrics_status" >&2
    exit 1
  }
done

current_headers="$temporary_directory/current-release.headers"
current_body="$temporary_directory/current-release.json"
assert_curl_succeeds 'current release metadata request' \
  --insecure \
  --silent \
  --dump-header "$current_headers" \
  --output "$current_body" \
  --resolve "speak-up.top:$https_port:127.0.0.1" \
  "https://speak-up.top:$https_port/downloads/android/release.json"
assert_response_header "$current_headers" 'Content-Type: application/json'
assert_response_header "$current_headers" 'Cache-Control: no-store'
cmp \
  "$current_body" \
  "$temporary_directory/public/downloads/android/release.json"

version_headers="$temporary_directory/version-release.headers"
assert_curl_succeeds 'versioned release metadata request' \
  --insecure \
  --silent \
  --dump-header "$version_headers" \
  --output /dev/null \
  --resolve "speak-up.top:$https_port:127.0.0.1" \
  "https://speak-up.top:$https_port/downloads/android/v0.1.0/release.json"
assert_response_header "$version_headers" \
  'Cache-Control: public, max-age=31536000, immutable'

checksum_body="$temporary_directory/downloaded.sha256"
assert_curl_succeeds 'checksum download request' \
  --insecure \
  --silent \
  --output "$checksum_body" \
  --resolve "speak-up.top:$https_port:127.0.0.1" \
  "https://speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256"
cmp \
  "$checksum_body" \
  "$temporary_directory/public/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256"

apk_headers="$temporary_directory/apk.headers"
apk_body="$temporary_directory/downloaded.apk"
assert_curl_succeeds 'APK HEAD request' \
  --head \
  --insecure \
  --silent \
  --dump-header "$apk_headers" \
  --output /dev/null \
  --resolve "speak-up.top:$https_port:127.0.0.1" \
  "https://speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk"
assert_response_header "$apk_headers" \
  'Content-Type: application/vnd.android.package-archive'
assert_response_header "$apk_headers" \
  'Cache-Control: public, max-age=31536000, immutable'
assert_response_header "$apk_headers" "Content-Length: $apk_size"
assert_curl_succeeds 'APK download request' \
  --insecure \
  --silent \
  --output "$apk_body" \
  --resolve "speak-up.top:$https_port:127.0.0.1" \
  "https://speak-up.top:$https_port/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk"
[[ "$(sha256sum "$apk_body" | awk '{print $1}')" == "$apk_sha" ]] || {
  printf '%s\n' 'downloaded APK SHA-256 is incorrect' >&2
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
    --output /dev/null \
    --write-out '%{http_code}' \
    --resolve "speak-up.top:$https_port:127.0.0.1" \
    "https://speak-up.top:$https_port$path")
  [[ "$status" == 404 ]] || {
    printf 'unexpected Android download path %s returned %s\n' "$path" "$status" >&2
    exit 1
  }
done

api_download_status=$(curl \
  --insecure \
  --silent \
  --output /dev/null \
  --write-out '%{http_code}' \
  --resolve "api.speak-up.top:$https_port:127.0.0.1" \
  "https://api.speak-up.top:$https_port/downloads/android/release.json")
[[ "$api_download_status" == 404 ]] || {
  printf 'API Android download route returned %s instead of 404\n' \
    "$api_download_status" >&2
  exit 1
}

api_headers="$temporary_directory/api.headers"
assert_curl_succeeds 'API proxy request' \
  --http1.1 \
  --insecure \
  --silent \
  --dump-header "$api_headers" \
  --output /dev/null \
  --resolve "api.speak-up.top:$https_port:127.0.0.1" \
  --header 'Authorization: Bearer release-smoke' \
  --header 'Upgrade: websocket' \
  --header 'Connection: Upgrade' \
  --header 'X-Forwarded-For: 198.51.100.7' \
  "https://api.speak-up.top:$https_port/v1/smoke"
assert_response_header "$api_headers" 'X-SpeakUp-Upstream: api'
assert_response_header "$api_headers" \
  'X-Seen-Authorization: Bearer release-smoke'
assert_response_header "$api_headers" 'X-Seen-Upgrade: websocket'
assert_response_header "$api_headers" 'X-Seen-Connection: upgrade'
assert_response_header "$api_headers" 'X-Seen-XFF: 198.51.100.7,'
assert_response_header "$api_headers" 'X-Seen-Host: api.speak-up.top'

rate_limited=false
for _ in {1..14}; do
  event_status=$(curl \
    --insecure \
    --silent \
    --output /dev/null \
    --write-out '%{http_code}' \
    --resolve "speak-up.top:$https_port:127.0.0.1" \
    --request POST \
    "https://speak-up.top:$https_port/api/events")
  if [[ "$event_status" == 429 ]]; then
    rate_limited=true
    break
  fi
done
[[ "$rate_limited" == true ]] || {
  printf '%s\n' 'Portal event endpoint did not enforce its request limit' >&2
  exit 1
}

printf '%s\n' 'production Nginx configuration check passed'
