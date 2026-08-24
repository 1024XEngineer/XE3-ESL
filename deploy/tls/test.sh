#!/usr/bin/env bash

set -euo pipefail

readonly tls_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly manage="$tls_directory/manage.sh"
readonly expected_certbot_image="certbot/certbot@sha256:34ee91d2f43008eb78a007d22f23ed4b2eaa9a454cb27ca2c042b49527a695b4"
readonly production_account_id="0123456789abcdef0123456789abcdef"
readonly secondary_account_id="abcdef0123456789abcdef0123456789"

fail() {
  printf 'TLS lifecycle contract test: %s\n' "$*" >&2
  exit 1
}

temporary_directory=$(mktemp -d /tmp/xe3-tls-test.XXXXXX)
trap 'rm -rf "$temporary_directory"' EXIT
readonly fake_bin="$temporary_directory/bin"
readonly certbot_root="$temporary_directory/letsencrypt"
readonly staging_webroot="$temporary_directory/staging-acme"
readonly production_webroot="$temporary_directory/production-acme"
readonly state_root="$temporary_directory/state"
readonly docker_log="$temporary_directory/docker.log"
readonly image_state="$temporary_directory/certbot-image.prepared"
readonly nginx_log="$temporary_directory/nginx.log"
readonly nginx_dump="$temporary_directory/nginx.dump"
readonly environment_file="$temporary_directory/tls.env"
readonly command_output="$temporary_directory/command.out"

write_account_fixture() {
  local account_id=$1
  local accounts_root="$certbot_root/accounts"
  local server_root="$accounts_root/acme-v02.api.letsencrypt.org"
  local directory_root="$server_root/directory"
  local account_root="$directory_root/$account_id"

  mkdir -p "$account_root"
  chmod 0700 "$accounts_root" "$server_root" "$directory_root" "$account_root"
  printf '%s\n' '{"kty":"RSA","fixture":true}' >"$account_root/private_key.json"
  printf '%s\n' '{"body":{},"uri":"fixture"}' >"$account_root/regr.json"
  printf '%s\n' '{"creation_host":"fixture"}' >"$account_root/meta.json"
  chmod 0400 "$account_root/private_key.json"
  chmod 0644 "$account_root/regr.json" "$account_root/meta.json"
}

mkdir -p \
  "$fake_bin" \
  "$certbot_root" \
  "$staging_webroot" \
  "$production_webroot" \
  "$state_root"
chmod 0700 "$certbot_root" "$state_root"
chmod 0755 "$staging_webroot" "$production_webroot"
: >"$docker_log"
: >"$nginx_log"

write_executable() {
  local path=$1
  local content=$2
  printf '%s\n' "$content" >"$path"
  chmod 0755 "$path"
}

write_executable "$fake_bin/uname" '#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  -s) printf "%s\n" "${FAKE_UNAME_SYSTEM:-Linux}" ;;
  -m) printf "%s\n" "${FAKE_UNAME_MACHINE:-x86_64}" ;;
  *) exit 2 ;;
esac'

write_executable "$fake_bin/flock" '#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == -n && "${2:-}" == 9 ]] || exit 2'

write_executable "$fake_bin/date" '#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "--utc +%s") printf "%s\n" 1800000000 ;;
  *"Aug 1 00:00:00 2026 GMT"*) printf "%s\n" 1799000000 ;;
  *"Dec 1 00:00:00 2026 GMT"*) printf "%s\n" 1808000000 ;;
  *"Six Days Remaining GMT"*) printf "%s\n" 1800518400 ;;
  *"Expires Within Skew GMT"*) printf "%s\n" 1800000299 ;;
  *"Expired Beyond Skew GMT"*) printf "%s\n" 1799999699 ;;
  *"Aug 1 00:00:00 2020 GMT"*) printf "%s\n" 1596240000 ;;
  *"Aug 2 00:00:00 2020 GMT"*) printf "%s\n" 1596326400 ;;
  *) exit 2 ;;
esac'

write_executable "$fake_bin/openssl" '#!/usr/bin/env bash
set -euo pipefail

value_from_file() {
  local name=$1 file=$2
  sed -n "s/^${name}=//p" "$file"
}

contains_argument() {
  local expected=$1 argument
  shift
  for argument in "$@"; do
    [[ "$argument" != "$expected" ]] || return 0
  done
  return 1
}

command=${1:-}
shift || true
case "$command" in
  x509)
    input=""
    previous=""
    for argument in "$@"; do
      if [[ "$previous" == -in ]]; then input=$argument; fi
      previous=$argument
    done
    [[ -f "$input" ]] || exit 1
    [[ "$(value_from_file TYPE "$input")" == CERTIFICATE ]] || exit 1
    if contains_argument -ext "$@"; then
      sans=$(value_from_file SANS "$input")
      [[ -n "$sans" ]] || exit 1
      printf "%s\n" "X509v3 Subject Alternative Name:"
      old_ifs=$IFS
      IFS=,
      set -- $sans
      IFS=$old_ifs
      first=true
      for domain in "$@"; do
        if $first; then printf "    DNS:%s" "$domain"; first=false
        else printf ", DNS:%s" "$domain"; fi
      done
      printf "\n"
    elif contains_argument -pubkey "$@"; then
      value_from_file PUBKEY "$input"
    elif contains_argument -startdate "$@"; then
      printf "notBefore=%s\n" "$(value_from_file START "$input")"
    elif contains_argument -enddate "$@"; then
      printf "notAfter=%s\n" "$(value_from_file END "$input")"
    fi
    ;;
  pkey)
    if contains_argument -pubin "$@"; then
      cat
      exit
    fi
    input=""
    previous=""
    for argument in "$@"; do
      if [[ "$previous" == -in ]]; then input=$argument; fi
      previous=$argument
    done
    [[ -f "$input" ]] || exit 1
    [[ "$(value_from_file TYPE "$input")" == PRIVATE_KEY ]] || exit 1
    value_from_file PUBKEY "$input"
    ;;
  *)
    exit 2
    ;;
esac'

write_executable "$fake_bin/docker" '#!/usr/bin/env bash
set -euo pipefail

write_renewal_configuration() {
  local root=$1 name=$2 sans=$3 webroot=$4 domain
  local -a mapped_domains
  printf "%s\n" \
    "archive_dir = /etc/letsencrypt/archive/$name" \
    "cert = /etc/letsencrypt/live/$name/cert.pem" \
    "privkey = /etc/letsencrypt/live/$name/privkey.pem" \
    "chain = /etc/letsencrypt/live/$name/chain.pem" \
    "fullchain = /etc/letsencrypt/live/$name/fullchain.pem" \
    "[renewalparams]" \
    "account = ${FAKE_DOCKER_WRITTEN_ACCOUNT_ID:-$FAKE_ACME_ACCOUNT_ID}" \
    "authenticator = webroot" \
    "autorenew = True" \
    "server = https://acme-v02.api.letsencrypt.org/directory" \
    "[[webroot_map]]" \
    >"$root/renewal/$name.conf"
  IFS=, read -r -a mapped_domains <<<"$sans"
  for domain in "${mapped_domains[@]}"; do
    printf "%s = %s\n" "$domain" "$webroot" >>"$root/renewal/$name.conf"
  done
  chmod 0600 "$root/renewal/$name.conf"
}

write_lineage() {
  local root=$1 name=$2 sans=$3 key=$4
  local archive="$root/archive/$name" live="$root/live/$name"
  local version_file="$archive/.version" version=1
  mkdir -p "$archive" "$live" "$root/renewal"
  chmod 0700 "$archive" "$live"
  if [[ -f "$version_file" ]]; then
    version=$(( $(<"$version_file") + 1 ))
  fi
  printf "%s\n" "$version" >"$version_file"
  printf "%s\n" \
    TYPE=CERTIFICATE \
    "VERSION=$version" \
    "PUBKEY=$key" \
    "SANS=$sans" \
    "START=Aug 1 00:00:00 2026 GMT" \
    "END=Dec 1 00:00:00 2026 GMT" \
    >"$archive/fullchain$version.pem"
  printf "%s\n" TYPE=PRIVATE_KEY "PUBKEY=$key" >"$archive/privkey$version.pem"
  chmod 0644 "$archive/fullchain$version.pem"
  chmod 0600 "$archive/privkey$version.pem"
  ln -sfn "../../archive/$name/fullchain$version.pem" "$live/fullchain.pem"
  ln -sfn "../../archive/$name/privkey$version.pem" "$live/privkey.pem"
  write_renewal_configuration "$root" "$name" "$sans" /var/www/acme
}

[[ -n "${FAKE_DOCKER_LOG:-}" ]] || exit 2
printf "%s\n" "$*" >>"$FAKE_DOCKER_LOG"
case "${1:-}" in
  version)
    [[ "${FAKE_DOCKER_UNAVAILABLE:-0}" != 1 ]]
    exit
    ;;
  pull)
    [[ "${2:-}" == --platform && "${3:-}" == linux/amd64 &&
      "${4:-}" == "$FAKE_CERTBOT_IMAGE" && $# == 4 ]] || exit 2
    [[ "${FAKE_DOCKER_PULL_FAIL:-0}" != 1 ]] || exit 42
    : >"$FAKE_DOCKER_IMAGE_STATE"
    exit
    ;;
  image)
    [[ "${2:-}" == inspect && -f "$FAKE_DOCKER_IMAGE_STATE" ]] || exit 1
    [[ "${3:-}" == --format && "${5:-}" == "$FAKE_CERTBOT_IMAGE" && $# == 5 ]] || exit 2
    printf "%s\n" \
      "${FAKE_DOCKER_IMAGE_OS:-linux}" \
      "${FAKE_DOCKER_IMAGE_ARCHITECTURE:-amd64}" \
      "${FAKE_DOCKER_IMAGE_REPODIGEST:-$FAKE_CERTBOT_IMAGE}"
    exit
    ;;
  run)
    [[ -f "$FAKE_DOCKER_IMAGE_STATE" ]] || exit 1
    ;;
  *)
    exit 2
    ;;
esac

config_root=""
cert_name=""
domains=""
dry_run=false
webroot_path=""
account_id=""
server=""
subcommand=""
previous=""
for argument in "$@"; do
  case "$previous" in
    --volume)
      case "$argument" in
        *:/etc/letsencrypt) config_root=${argument%:/etc/letsencrypt} ;;
      esac
      ;;
    --cert-name) cert_name=$argument ;;
    --webroot-path) webroot_path=$argument ;;
    --account) account_id=$argument ;;
    --server) server=$argument ;;
    --domains)
      if [[ -n "$domains" ]]; then domains="$domains,$argument"
      else domains=$argument; fi
      ;;
  esac
  case "$argument" in
    certonly | reconfigure | renew) subcommand=$argument ;;
    --dry-run) dry_run=true ;;
  esac
  previous=$argument
done
[[ -n "$config_root" && -n "$cert_name" && -n "$subcommand" ]] || exit 2
[[ "${FAKE_DOCKER_FAIL_CERT_NAME:-}" != "$cert_name" ]] || exit 41

case "$subcommand" in
  certonly)
    [[ -n "$domains" && "$account_id" == "$FAKE_ACME_ACCOUNT_ID" &&
      "$server" == https://acme-v02.api.letsencrypt.org/directory ]] || exit 2
    write_lineage "$config_root" "$cert_name" "$domains" "$cert_name-key"
    ;;
  reconfigure)
    [[ "$webroot_path" == /var/www/acme ]] || exit 2
    certificate="$config_root/live/$cert_name/fullchain.pem"
    [[ -L "$certificate" ]] || exit 2
    current_sans=$(sed -n "s/^SANS=//p" "$certificate")
    [[ -n "$current_sans" ]] || exit 2
    write_renewal_configuration "$config_root" "$cert_name" "$current_sans" "$webroot_path"
    ;;
  renew)
    $dry_run && exit
    if [[ "${FAKE_DOCKER_RENEW_CHANGE:-}" == "$cert_name" ]]; then
      certificate="$config_root/live/$cert_name/fullchain.pem"
      [[ -L "$certificate" ]] || exit 2
      current_sans=$(sed -n "s/^SANS=//p" "$certificate")
      if [[ "${FAKE_DOCKER_CORRUPT_SAN:-0}" == 1 ]]; then
        current_sans="$current_sans,unexpected.example.com"
      fi
      write_lineage "$config_root" "$cert_name" "$current_sans" "$cert_name-key"
    fi
    ;;
esac'

write_executable "$fake_bin/nginx" '#!/usr/bin/env bash
set -euo pipefail
[[ -n "${FAKE_NGINX_LOG:-}" ]] || exit 2
printf "%s\n" "$*" >>"$FAKE_NGINX_LOG"
if [[ "${1:-}" == -t && "${FAKE_NGINX_TEST_FAIL:-0}" == 1 ]]; then exit 42; fi
if [[ "${1:-}" == -T ]]; then
  [[ -f "${FAKE_NGINX_DUMP:-}" ]] || exit 2
  cat "$FAKE_NGINX_DUMP"
fi'

cat >"$environment_file" <<EOF
TLS_CERTBOT_CONFIG_ROOT=$certbot_root
TLS_STAGING_ACME_ROOT=$staging_webroot
TLS_PRODUCTION_ACME_ROOT=$production_webroot
TLS_STATE_ROOT=$state_root
TLS_NGINX_BINARY=$fake_bin/nginx
EOF
chmod 0600 "$environment_file"

write_nginx_dump() {
  local staging_portal_certificate=${1:-$certbot_root/live/staging.speak-up.top/fullchain.pem}
  local staging_portal_key=${2:-$certbot_root/live/staging.speak-up.top/privkey.pem}
  local unrelated_certificate=${3:-}
  local unrelated_key=${4:-}

  cat >"$nginx_dump" <<EOF
server {
    listen 443 ssl;
    server_name staging.speak-up.top;
    ssl_certificate $staging_portal_certificate;
    ssl_certificate_key $staging_portal_key;
    location / {
        return 204;
    }
}
server {
    listen [::]:443 ssl;
    server_name staging-api.speak-up.top;
    ssl_certificate $certbot_root/live/staging.speak-up.top/fullchain.pem;
    ssl_certificate_key $certbot_root/live/staging.speak-up.top/privkey.pem;
}
server {
    listen 443 ssl;
    server_name speak-up.top;
    ssl_certificate $certbot_root/live/speak-up.top/fullchain.pem;
    ssl_certificate_key $certbot_root/live/speak-up.top/privkey.pem;
    location / {
        return 204;
    }
}
server {
    listen 443 ssl;
    server_name www.speak-up.top;
    ssl_certificate $certbot_root/live/speak-up.top/fullchain.pem;
    ssl_certificate_key $certbot_root/live/speak-up.top/privkey.pem;
}
server {
    listen 443 ssl;
    server_name api.speak-up.top;
    ssl_certificate $certbot_root/live/speak-up.top/fullchain.pem;
    ssl_certificate_key $certbot_root/live/speak-up.top/privkey.pem;
}
server {
    listen 443 ssl;
    server_name monitor.speak-up.top;
    ssl_certificate $certbot_root/live/monitor.speak-up.top/fullchain.pem;
    ssl_certificate_key $certbot_root/live/monitor.speak-up.top/privkey.pem;
}
EOF

  if [[ -n "$unrelated_certificate" || -n "$unrelated_key" ]]; then
    [[ -n "$unrelated_certificate" && -n "$unrelated_key" ]] ||
      fail "Unrelated Nginx fixture requires both certificate paths"
    cat >>"$nginx_dump" <<EOF
server {
    listen 443 ssl;
    server_name unrelated.example.com;
    ssl_certificate $unrelated_certificate;
    ssl_certificate_key $unrelated_key;
}
EOF
  fi
}

write_nginx_dump

export PATH="$fake_bin:$PATH"
export FAKE_DOCKER_LOG="$docker_log"
export FAKE_DOCKER_IMAGE_STATE="$image_state"
export FAKE_CERTBOT_IMAGE="$expected_certbot_image"
export FAKE_ACME_ACCOUNT_ID="$production_account_id"
export FAKE_NGINX_LOG="$nginx_log"
export FAKE_NGINX_DUMP="$nginx_dump"

expect_failure() {
  local label=$1
  shift
  if "$@" >"$command_output" 2>&1; then
    fail "$label unexpectedly succeeded"
  fi
}

assert_no_reload() {
  if grep -Fxq -- '-s reload' "$nginx_log"; then
    fail "$1 unexpectedly reloaded Nginx"
  fi
}

expect_failure_before_docker() {
  local label=$1
  local docker_calls_before docker_calls_after
  shift

  docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
  expect_failure "$label" "$@"
  docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
  [[ "$docker_calls_before" == "$docker_calls_after" ]] ||
    fail "$label was rejected only after Docker ran"
}

expect_renewal_failure_before_docker() {
  local label=$1
  local docker_calls_before docker_calls_after

  docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
  reset_nginx_log
  expect_failure "$label" "$manage" renew --env-file "$environment_file"
  assert_no_reload "$label"
  docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
  [[ "$docker_calls_before" == "$docker_calls_after" ]] ||
    fail "$label was rejected only after Docker ran"
}

reset_nginx_log() {
  : >"$nginx_log"
}

write_fixture_renewal_configuration() {
  local name=$1 sans=$2 webroot=${3:-/var/www/acme}
  local domain
  local -a mapped_domains

  printf '%s\n' \
    "archive_dir = /etc/letsencrypt/archive/$name" \
    "cert = /etc/letsencrypt/live/$name/cert.pem" \
    "privkey = /etc/letsencrypt/live/$name/privkey.pem" \
    "chain = /etc/letsencrypt/live/$name/chain.pem" \
    "fullchain = /etc/letsencrypt/live/$name/fullchain.pem" \
    '[renewalparams]' \
    "account = $production_account_id" \
    'authenticator = webroot' \
    'autorenew = True' \
    'server = https://acme-v02.api.letsencrypt.org/directory' \
    '[[webroot_map]]' \
    >"$certbot_root/renewal/$name.conf"
  IFS=, read -r -a mapped_domains <<<"$sans"
  for domain in "${mapped_domains[@]}"; do
    printf '%s = %s\n' "$domain" "$webroot" >>"$certbot_root/renewal/$name.conf"
  done
  chmod 0600 "$certbot_root/renewal/$name.conf"
}

write_fixture_lineage() {
  local name=$1 sans=$2 version=${3:-1} webroot=${4:-/var/www/acme}
  local archive="$certbot_root/archive/$name" live="$certbot_root/live/$name"
  mkdir -p "$archive" "$live" "$certbot_root/renewal"
  chmod 0700 "$archive" "$live"
  printf '%s\n' "$version" >"$archive/.version"
  printf '%s\n' \
    TYPE=CERTIFICATE \
    "VERSION=$version" \
    "PUBKEY=$name-key" \
    "SANS=$sans" \
    'START=Aug 1 00:00:00 2026 GMT' \
    'END=Dec 1 00:00:00 2026 GMT' \
    >"$archive/fullchain$version.pem"
  printf '%s\n' TYPE=PRIVATE_KEY "PUBKEY=$name-key" >"$archive/privkey$version.pem"
  chmod 0644 "$archive/fullchain$version.pem"
  chmod 0600 "$archive/privkey$version.pem"
  ln -sfn "../../archive/$name/fullchain$version.pem" "$live/fullchain.pem"
  ln -sfn "../../archive/$name/privkey$version.pem" "$live/privkey.pem"
  write_fixture_renewal_configuration "$name" "$sans" "$webroot"
}

# Bootstrap only serves HTTP-01 and never creates a duplicate legacy Portal vhost.
"$manage" render-bootstrap \
  --environment staging \
  --env-file "$environment_file" \
  --output "$temporary_directory/staging-http.conf" >"$command_output"
grep -Fq 'listen 80;' "$temporary_directory/staging-http.conf" || fail "Staging bootstrap is not HTTP"
grep -Fq 'server_name staging.speak-up.top staging-api.speak-up.top;' \
  "$temporary_directory/staging-http.conf" || fail "Staging bootstrap hostnames are wrong"
grep -Fq "root $staging_webroot;" "$temporary_directory/staging-http.conf" ||
  fail "Staging bootstrap webroot is wrong"
grep -Fq 'try_files $uri =404;' "$temporary_directory/staging-http.conf" ||
  fail "Staging challenge path does not fail closed"
grep -Fq 'set $speakup_tls_bootstrap staging;' "$temporary_directory/staging-http.conf" ||
  fail "Staging bootstrap has no removable runtime marker"
grep -Fq 'return 404;' "$temporary_directory/staging-http.conf" ||
  fail "Staging bootstrap exposes non-challenge paths"
! grep -Fq 'listen 443' "$temporary_directory/staging-http.conf" ||
  fail "Staging bootstrap pretends HTTPS is available"

"$manage" render-bootstrap \
  --environment production \
  --env-file "$environment_file" \
  --output "$temporary_directory/production-http.conf" >"$command_output"
grep -Fq 'server_name api.speak-up.top;' "$temporary_directory/production-http.conf" ||
  fail "Production bootstrap must only claim the new API hostname"
grep -Fq 'set $speakup_tls_bootstrap production;' "$temporary_directory/production-http.conf" ||
  fail "Production bootstrap has no removable runtime marker"
! grep -Fq 'server_name speak-up.top' "$temporary_directory/production-http.conf" ||
  fail "Production bootstrap duplicates the legacy Portal hostname"
! grep -Fq 'server_name www.speak-up.top' "$temporary_directory/production-http.conf" ||
  fail "Production bootstrap duplicates the legacy redirect hostname"

"$manage" render-bootstrap \
  --environment monitor \
  --env-file "$environment_file" \
  --output "$temporary_directory/monitor-http.conf" >"$command_output"
grep -Fq 'server_name monitor.speak-up.top;' "$temporary_directory/monitor-http.conf" ||
  fail "Monitor bootstrap hostname is wrong"
grep -Fq "root $production_webroot;" "$temporary_directory/monitor-http.conf" ||
  fail "Monitor bootstrap webroot is wrong"
grep -Fq 'set $speakup_tls_bootstrap monitor;' "$temporary_directory/monitor-http.conf" ||
  fail "Monitor bootstrap has no removable runtime marker"

# Configuration parsing is strict and never prints values from rejected lines.
cp "$environment_file" "$temporary_directory/secret.env"
printf '%s\n' 'PORTAL_ADMIN_PASSWORD=do-not-print-this-value' >>"$temporary_directory/secret.env"
chmod 0600 "$temporary_directory/secret.env"
expect_failure "unsupported configuration" \
  "$manage" render-bootstrap --environment staging \
    --env-file "$temporary_directory/secret.env" \
    --output "$temporary_directory/rejected.conf"
! grep -Fq 'do-not-print-this-value' "$command_output" || fail "Rejected Secret value was printed"
cp "$environment_file" "$temporary_directory/duplicate.env"
printf '%s\n' "TLS_STATE_ROOT=$state_root" >>"$temporary_directory/duplicate.env"
chmod 0600 "$temporary_directory/duplicate.env"
expect_failure "duplicate configuration" \
  "$manage" render-bootstrap --environment staging \
    --env-file "$temporary_directory/duplicate.env" \
    --output "$temporary_directory/rejected.conf"

# Image installation is an explicit audited operation. Runtime commands never pull.
expect_failure "missing prepared Certbot image" \
  "$manage" issue-staging --env-file "$environment_file"
! grep -Fq 'run ' "$docker_log" || fail "Missing image reached docker run"
FAKE_DOCKER_PULL_FAIL=1 expect_failure "Certbot image pull failure" \
  "$manage" prepare-image --env-file "$environment_file"
FAKE_DOCKER_IMAGE_OS=windows expect_failure "wrong Certbot image OS" \
  "$manage" prepare-image --env-file "$environment_file"
FAKE_DOCKER_IMAGE_ARCHITECTURE=arm64 expect_failure "wrong Certbot image architecture" \
  "$manage" prepare-image --env-file "$environment_file"
FAKE_DOCKER_IMAGE_REPODIGEST=certbot/certbot@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  expect_failure "wrong Certbot repository digest" \
    "$manage" prepare-image --env-file "$environment_file"
"$manage" prepare-image --env-file "$environment_file" >"$command_output"
grep -Fq 'prepared=true' "$command_output" || fail "Certbot image preparation audit is missing"
grep -Fq "pull --platform linux/amd64 $expected_certbot_image" "$docker_log" ||
  fail "Certbot image preparation did not pull the fixed platform and digest"

# Issuance requires the account explicitly referenced by the existing Production
# renewal contract. Bootstrap rendering and image preparation above do not.
expect_failure_before_docker "missing Production account material" \
  "$manage" issue-staging --env-file "$environment_file"
write_account_fixture "$production_account_id"
write_account_fixture "$secondary_account_id"
mkdir -p "$certbot_root/renewal"
write_fixture_renewal_configuration \
  speak-up.top 'speak-up.top,www.speak-up.top' /var/www/certbot
expect_failure_before_docker "incomplete Production lineage" \
  "$manage" expand-production --env-file "$environment_file"
assert_no_reload "incomplete Production lineage"

write_fixture_lineage speak-up.top 'speak-up.top,www.speak-up.top' 1 /var/www/certbot
production_renewal="$certbot_root/renewal/speak-up.top.conf"
cp "$production_renewal" "$temporary_directory/valid-production-renewal.conf"

mv "$certbot_root/renewal" "$certbot_root/renewal.real"
ln -s renewal.real "$certbot_root/renewal"
expect_failure_before_docker "symbolic Production renewal directory" \
  "$manage" issue-staging --env-file "$environment_file"
unlink "$certbot_root/renewal"
mv "$certbot_root/renewal.real" "$certbot_root/renewal"

sed '/^account = /d' "$temporary_directory/valid-production-renewal.conf" \
  >"$production_renewal"
expect_failure_before_docker "missing Production account reference" \
  "$manage" issue-staging --env-file "$environment_file"
sed 's|^account = .*$|account = ../unsafe|' \
  "$temporary_directory/valid-production-renewal.conf" >"$production_renewal"
expect_failure_before_docker "unsafe Production account reference" \
  "$manage" issue-staging --env-file "$environment_file"
sed "s|^server = .*$|server = https://example.invalid/directory|" \
  "$temporary_directory/valid-production-renewal.conf" >"$production_renewal"
expect_failure_before_docker "wrong Production account server" \
  "$manage" issue-staging --env-file "$environment_file"
awk -v duplicate="$secondary_account_id" '
  { print }
  /^account = / { print "account = " duplicate }
' "$temporary_directory/valid-production-renewal.conf" >"$production_renewal"
expect_failure_before_docker "duplicate Production account reference" \
  "$manage" issue-staging --env-file "$environment_file"
cp "$temporary_directory/valid-production-renewal.conf" "$production_renewal"

production_account_root="$certbot_root/accounts/acme-v02.api.letsencrypt.org/directory/$production_account_id"
chmod 0755 "$certbot_root/accounts/acme-v02.api.letsencrypt.org/directory"
expect_failure_before_docker "permissive Production account parent" \
  "$manage" issue-staging --env-file "$environment_file"
chmod 0700 "$certbot_root/accounts/acme-v02.api.letsencrypt.org/directory"
chmod 0644 "$production_account_root/private_key.json"
expect_failure_before_docker "public Production account private key" \
  "$manage" issue-staging --env-file "$environment_file"
chmod 0400 "$production_account_root/private_key.json"

reset_nginx_log
FAKE_DOCKER_WRITTEN_ACCOUNT_ID="$secondary_account_id" \
  expect_failure "Certbot persisted the wrong Staging account" \
    "$manage" issue-staging --env-file "$environment_file"
! grep -Fq 'issued=true' "$command_output" ||
  fail "Wrong persisted Staging account produced a success audit"
assert_no_reload "wrong persisted Staging account"
mv "$certbot_root/live/staging.speak-up.top" \
  "$temporary_directory/rejected-staging-live"
mv "$certbot_root/archive/staging.speak-up.top" \
  "$temporary_directory/rejected-staging-archive"
mv "$certbot_root/renewal/staging.speak-up.top.conf" \
  "$temporary_directory/rejected-staging-renewal.conf"

"$manage" issue-staging --env-file "$environment_file" >"$command_output"
grep -Fq 'issued=true reload=false' "$command_output" || fail "Staging issue audit is missing"
grep -Fxq "account = $production_account_id" \
  "$certbot_root/renewal/staging.speak-up.top.conf" ||
  fail "Staging renewal did not persist the Production account"
assert_no_reload "Staging issuance"
expect_failure "duplicate Staging issuance" \
  "$manage" issue-staging --env-file "$environment_file"

"$manage" issue-monitor --env-file "$environment_file" >"$command_output"
grep -Fq 'environment=monitor certificate_name=monitor.speak-up.top' "$command_output" ||
  fail "Monitor issue audit is missing"
grep -Fxq "account = $production_account_id" \
  "$certbot_root/renewal/monitor.speak-up.top.conf" ||
  fail "Monitor renewal did not persist the Production account"
expect_failure "duplicate Monitor issuance" \
  "$manage" issue-monitor --env-file "$environment_file"

cp "$certbot_root/renewal/speak-up.top.conf" "$temporary_directory/legacy-production-renewal.conf"
sed '/^www\.speak-up\.top = /d' \
  "$temporary_directory/legacy-production-renewal.conf" \
  >"$certbot_root/renewal/speak-up.top.conf"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
expect_failure "incomplete legacy Production webroot map" \
  "$manage" expand-production --env-file "$environment_file"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Invalid legacy Production map was rejected only after Docker ran"

sed 's|^www\.speak-up\.top = /var/www/certbot$|www.speak-up.top = /var/www/acme|' \
  "$temporary_directory/legacy-production-renewal.conf" \
  >"$certbot_root/renewal/speak-up.top.conf"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
expect_failure "mixed legacy Production webroot map" \
  "$manage" expand-production --env-file "$environment_file"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Mixed legacy Production map was rejected only after Docker ran"

cp "$temporary_directory/legacy-production-renewal.conf" \
  "$certbot_root/renewal/speak-up.top.conf"
printf '%s\n' 'unexpected.example.com = /var/www/certbot' \
  >>"$certbot_root/renewal/speak-up.top.conf"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
expect_failure "extra legacy Production webroot domain" \
  "$manage" expand-production --env-file "$environment_file"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Extra legacy Production domain was rejected only after Docker ran"

cp "$temporary_directory/legacy-production-renewal.conf" \
  "$certbot_root/renewal/speak-up.top.conf"
"$manage" expand-production --env-file "$environment_file" >"$command_output"
grep -Fq 'webroot_map=/var/www/acme migrated=true' "$command_output" ||
  fail "Production legacy webroot migration audit is missing"
grep -Fq 'issued=true reload=false' "$command_output" || fail "Production expansion audit is missing"
grep -Fq 'reconfigure' "$docker_log" ||
  fail "Production legacy webroot was not migrated through Certbot reconfigure"
grep -Fq -- 'reconfigure --non-interactive --cert-name speak-up.top --webroot-path /var/www/acme' \
  "$docker_log" || fail "Production reconfigure did not use the canonical webroot path"
! grep -Fq '/var/www/certbot' "$certbot_root/renewal/speak-up.top.conf" ||
  fail "Production renewal configuration still contains the legacy webroot"
"$manage" verify --environment staging --env-file "$environment_file" >"$command_output"
"$manage" verify --environment production --env-file "$environment_file" >"$command_output"
"$manage" verify --environment monitor --env-file "$environment_file" >"$command_output"
grep -Fq 'verified=true' "$command_output" || fail "Production verification audit is missing"

staging_renewal="$certbot_root/renewal/staging.speak-up.top.conf"
cp "$staging_renewal" "$temporary_directory/valid-staging-renewal.conf"
sed "s|^account = .*$|account = $secondary_account_id|" \
  "$temporary_directory/valid-staging-renewal.conf" >"$staging_renewal"
reset_nginx_log
expect_failure "mismatched Staging renewal account" \
  "$manage" verify --environment staging --env-file "$environment_file"
assert_no_reload "mismatched Staging renewal account verification"
expect_renewal_failure_before_docker "mismatched Staging renewal account"
cp "$temporary_directory/valid-staging-renewal.conf" "$staging_renewal"

grep -Fq -- '--platform linux/amd64' "$docker_log" || fail "Certbot platform is not pinned"
grep -Fq -- '--pull never' "$docker_log" || fail "Certbot runtime pull policy is not fail-closed"
! grep -Fq -- '--pull always' "$docker_log" || fail "Certbot runtime can still pull implicitly"
grep -Fq "$expected_certbot_image" "$docker_log" || fail "Certbot image digest is not pinned"
grep -Fq -- '--no-directory-hooks' "$docker_log" || fail "Certbot directory hooks are not disabled"
grep -Fq -- '--config /dev/null' "$docker_log" || fail "Certbot global config is not isolated"
grep -Fq -- "--server https://acme-v02.api.letsencrypt.org/directory --account $production_account_id" \
  "$docker_log" || fail "Certbot issuance did not bind the existing Production account"
! grep -Fq -- "--account $secondary_account_id" "$docker_log" ||
  fail "Certbot selected an account that was not referenced by Production renewal"
! grep -Fq -- '--email' "$docker_log" || fail "Certbot issuance still sends an obsolete email"
! grep -Fq -- '--register-unsafely-without-email' "$docker_log" ||
  fail "Certbot issuance can register an untracked account"
grep -Fq -- '--domains staging.speak-up.top --domains staging-api.speak-up.top' "$docker_log" ||
  fail "Staging Certbot SAN arguments are not exact"
grep -Fq -- '--webroot-map {"staging.speak-up.top":"/var/www/acme","staging-api.speak-up.top":"/var/www/acme"}' \
  "$docker_log" || fail "Staging Certbot webroot map argument is not exact"
grep -Fq -- '--domains speak-up.top --domains www.speak-up.top --domains api.speak-up.top' \
  "$docker_log" || fail "Production Certbot SAN arguments are not exact"
grep -Fq -- '--webroot-map {"speak-up.top":"/var/www/acme","www.speak-up.top":"/var/www/acme","api.speak-up.top":"/var/www/acme"}' \
  "$docker_log" || fail "Production Certbot webroot map argument is not exact"
grep -Fq -- '--domains monitor.speak-up.top' "$docker_log" ||
  fail "Monitor Certbot SAN argument is not exact"
grep -Fq -- '--webroot-map {"monitor.speak-up.top":"/var/www/acme"}' \
  "$docker_log" || fail "Monitor Certbot webroot map argument is not exact"
grep -Fxq "account = $production_account_id" \
  "$certbot_root/renewal/speak-up.top.conf" ||
  fail "Production renewal did not retain the selected account"

# Expected certificate directives in an unrelated block cannot compensate for a
# wrong certificate/key on the block that owns an expected hostname.
staging_live_certificate="$certbot_root/live/staging.speak-up.top/fullchain.pem"
staging_live_key="$certbot_root/live/staging.speak-up.top/privkey.pem"
write_nginx_dump \
  "$temporary_directory/wrong-staging-certificate.pem" \
  "$temporary_directory/wrong-staging-key.pem" \
  "$staging_live_certificate" \
  "$staging_live_key"
reset_nginx_log
expect_failure "cross-block Staging certificate/key padding" \
  "$manage" activate --environment staging --env-file "$environment_file"
assert_no_reload "cross-block Staging certificate/key padding"
[[ ! -e "$state_root/staging.deployed.sha256" ]] ||
  fail "Rejected cross-block certificate/key padding wrote activation state"

# A correct certificate count cannot hide a wrong key in the hostname block.
write_nginx_dump \
  "$staging_live_certificate" \
  "$temporary_directory/wrong-staging-key.pem" \
  "$temporary_directory/unrelated-certificate.pem" \
  "$staging_live_key"
reset_nginx_log
expect_failure "cross-block Staging key padding" \
  "$manage" activate --environment staging --env-file "$environment_file"
assert_no_reload "cross-block Staging key padding"
[[ ! -e "$state_root/staging.deployed.sha256" ]] ||
  fail "Rejected cross-block key padding wrote activation state"

# Initial activation verifies each correct server block before reload and records
# exactly what Nginx uses.
write_nginx_dump
reset_nginx_log
"$manage" activate --environment staging --env-file "$environment_file" >"$command_output"
grep -Fxq -- '-t' "$nginx_log" || fail "Staging activation did not test Nginx"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Staging activation did not gracefully reload"
reset_nginx_log
"$manage" activate --environment production --env-file "$environment_file" >"$command_output"
grep -Fxq -- '-t' "$nginx_log" || fail "Production activation did not test Nginx"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Production activation did not gracefully reload"
reset_nginx_log
"$manage" activate --environment monitor --env-file "$environment_file" >"$command_output"
grep -Fxq -- '-t' "$nginx_log" || fail "Monitor activation did not test Nginx"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Monitor activation did not gracefully reload"
[[ -s "$state_root/staging.deployed.sha256" ]] || fail "Staging activation state is missing"
[[ -s "$state_root/production.deployed.sha256" ]] || fail "Production activation state is missing"
[[ -s "$state_root/monitor.deployed.sha256" ]] || fail "Monitor activation state is missing"

# A certificate with six days left is rejected for serving but may still reach Certbot renewal.
staging_certificate=$(realpath "$certbot_root/live/staging.speak-up.top/fullchain.pem")
cp "$staging_certificate" "$temporary_directory/staging-six-day.backup"
sed 's/^END=.*/END=Six Days Remaining GMT/' \
  "$temporary_directory/staging-six-day.backup" >"$staging_certificate"
expect_failure "six-day certificate release verification" \
  "$manage" verify --environment staging --env-file "$environment_file"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
reset_nginx_log
FAKE_DOCKER_RENEW_CHANGE=staging.speak-up.top \
  "$manage" renew --env-file "$environment_file" >"$command_output"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
((docker_calls_after > docker_calls_before)) || fail "Six-day certificate never reached Certbot"
grep -Fq 'renewed=true reload=true' "$command_output" ||
  fail "Six-day certificate was not replaced and activated"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Six-day renewal did not reload Nginx"

# A certificate inside the fail-closed clock-skew window never reaches Docker.
production_certificate=$(realpath "$certbot_root/live/speak-up.top/fullchain.pem")
cp "$production_certificate" "$temporary_directory/production-within-skew.backup"
sed 's/^END=.*/END=Expires Within Skew GMT/' \
  "$temporary_directory/production-within-skew.backup" >"$production_certificate"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
expect_failure "certificate expiring inside clock-skew window" \
  "$manage" renew --env-file "$environment_file"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Imminently expiring certificate was rejected only after Docker ran"
cp "$temporary_directory/production-within-skew.backup" "$production_certificate"

# An already expired certificate also fails before any Docker call.
cp "$production_certificate" "$temporary_directory/production-expired.backup"
sed 's/^END=.*/END=Expired Beyond Skew GMT/' \
  "$temporary_directory/production-expired.backup" >"$production_certificate"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
expect_failure "expired pre-renew certificate" \
  "$manage" renew --env-file "$environment_file"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Expired certificate was rejected only after Docker ran"
cp "$temporary_directory/production-expired.backup" "$production_certificate"

# A successful no-op renewal verifies all three lineages and never reloads.
reset_nginx_log
"$manage" renew --env-file "$environment_file" >"$command_output"
grep -Fq 'renewed=true reload=false' "$command_output" || fail "No-op renewal audit is wrong"
assert_no_reload "no-op renewal"

# Autorenew may be absent or exactly True; maps must be exact and canonical before Docker.
production_renewal="$certbot_root/renewal/speak-up.top.conf"
cp "$production_renewal" "$temporary_directory/strict-production-renewal.conf"
sed '/^autorenew = True$/d' \
  "$temporary_directory/strict-production-renewal.conf" >"$production_renewal"
"$manage" verify --environment production --env-file "$environment_file" >"$command_output"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

sed 's/^autorenew = True$/autorenew = False/' \
  "$temporary_directory/strict-production-renewal.conf" >"$production_renewal"
expect_renewal_failure_before_docker "disabled Certbot autorenew"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

sed 's/^autorenew = True$/autorenew = true/' \
  "$temporary_directory/strict-production-renewal.conf" >"$production_renewal"
expect_renewal_failure_before_docker "non-canonical Certbot autorenew"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

sed 's|^api\.speak-up\.top = /var/www/acme$|api.speak-up.top = /wrong/webroot|' \
  "$temporary_directory/strict-production-renewal.conf" >"$production_renewal"
expect_failure "strict Production verify webroot map" \
  "$manage" verify --environment production --env-file "$environment_file"
expect_renewal_failure_before_docker "wrong Certbot webroot path"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

sed '/^api\.speak-up\.top = /d' \
  "$temporary_directory/strict-production-renewal.conf" >"$production_renewal"
expect_renewal_failure_before_docker "missing Certbot webroot domain"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

printf '%s\n' 'unexpected.example.com = /var/www/acme' >>"$production_renewal"
expect_renewal_failure_before_docker "extra Certbot webroot domain"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

printf '%s\n' 'api.speak-up.top = /var/www/acme' >>"$production_renewal"
expect_renewal_failure_before_docker "duplicate Certbot webroot domain"
cp "$temporary_directory/strict-production-renewal.conf" "$production_renewal"

# Stored renewal hooks cannot execute outside the wrapper's verification sequence.
printf '%s\n' 'renew_hook = /bin/false' >>"$certbot_root/renewal/speak-up.top.conf"
docker_calls_before=$(wc -l <"$docker_log" | tr -d '[:space:]')
reset_nginx_log
expect_failure "stored Certbot hook" \
  "$manage" renew --env-file "$environment_file"
assert_no_reload "stored Certbot hook"
docker_calls_after=$(wc -l <"$docker_log" | tr -d '[:space:]')
[[ "$docker_calls_before" == "$docker_calls_after" ]] ||
  fail "Stored Certbot hook was rejected only after Docker ran"
sed -i.bak '/^renew_hook = /d' "$certbot_root/renewal/speak-up.top.conf"
rm -f "$certbot_root/renewal/speak-up.top.conf.bak"

# One changed lineage triggers one reload, but only after all lineages verify.
reset_nginx_log
FAKE_DOCKER_RENEW_CHANGE=staging.speak-up.top \
  "$manage" renew --env-file "$environment_file" >"$command_output"
[[ $(grep -Fxc -- '-t' "$nginx_log") -eq 1 ]] || fail "Changed renewal did not test Nginx once"
[[ $(grep -Fxc -- '-s reload' "$nginx_log") -eq 1 ]] ||
  fail "Changed renewal did not reload Nginx exactly once"
grep -Fq 'renewed=true reload=true' "$command_output" || fail "Changed renewal audit is wrong"

# Cert validation failure and nginx -t failure both preserve the deployed state and skip reload.
production_state_before=$(<"$state_root/production.deployed.sha256")
reset_nginx_log
expect_failure "invalid renewed SAN" env \
  FAKE_DOCKER_RENEW_CHANGE=speak-up.top \
  FAKE_DOCKER_CORRUPT_SAN=1 \
  "$manage" renew --env-file "$environment_file"
assert_no_reload "invalid renewed SAN"
[[ "$(<"$state_root/production.deployed.sha256")" == "$production_state_before" ]] ||
  fail "Invalid certificate changed the deployed state"
write_fixture_lineage speak-up.top 'speak-up.top,www.speak-up.top,api.speak-up.top' 20

reset_nginx_log
FAKE_NGINX_TEST_FAIL=1 FAKE_DOCKER_RENEW_CHANGE=speak-up.top \
  expect_failure "nginx configuration failure" \
    "$manage" renew --env-file "$environment_file"
grep -Fxq -- '-t' "$nginx_log" || fail "Failed renewal did not run nginx -t"
assert_no_reload "nginx -t failure"
[[ "$(<"$state_root/production.deployed.sha256")" == "$production_state_before" ]] ||
  fail "nginx -t failure changed the deployed state"

# A later run can activate the already-renewed verified certificate after nginx is fixed.
reset_nginx_log
"$manage" renew --env-file "$environment_file" >"$command_output"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Pending verified certificate was not activated"

# A syntactically valid but incomplete loaded vhost also blocks reload and state advancement.
grep -Fv 'server_name api.speak-up.top;' "$nginx_dump" >"$temporary_directory/incomplete-nginx.dump"
production_state_before=$(<"$state_root/production.deployed.sha256")
reset_nginx_log
FAKE_NGINX_DUMP="$temporary_directory/incomplete-nginx.dump" \
  FAKE_DOCKER_RENEW_CHANGE=speak-up.top \
  expect_failure "incomplete loaded Nginx contract" \
    "$manage" renew --env-file "$environment_file"
grep -Fxq -- '-t' "$nginx_log" || fail "Incomplete Nginx contract was not syntax-tested"
grep -Fxq -- '-T' "$nginx_log" || fail "Incomplete Nginx contract was not inspected"
assert_no_reload "incomplete loaded Nginx contract"
[[ "$(<"$state_root/production.deployed.sha256")" == "$production_state_before" ]] ||
  fail "Incomplete Nginx contract changed the deployed state"
reset_nginx_log
"$manage" renew --env-file "$environment_file" >"$command_output"
grep -Fxq -- '-s reload' "$nginx_log" || fail "Fixed loaded Nginx contract did not activate pending cert"

# A selected environment's temporary HTTP bootstrap must be removed before activation.
printf '%s\n' 'set $speakup_tls_bootstrap staging;' >>"$nginx_dump"
reset_nginx_log
expect_failure "still-loaded Staging bootstrap" \
  "$manage" activate --environment staging --env-file "$environment_file"
assert_no_reload "still-loaded Staging bootstrap"
sed -i.bak '/speakup_tls_bootstrap staging/d' "$nginx_dump"
rm -f "$nginx_dump.bak"

# Dry-run checks all renewal lineages and can neither change state nor reload.
staging_state_before=$(<"$state_root/staging.deployed.sha256")
production_state_before=$(<"$state_root/production.deployed.sha256")
monitor_state_before=$(<"$state_root/monitor.deployed.sha256")
reset_nginx_log
FAKE_DOCKER_RENEW_CHANGE=staging.speak-up.top \
  "$manage" renew-dry-run --env-file "$environment_file" >"$command_output"
assert_no_reload "renewal dry-run"
[[ "$(<"$state_root/staging.deployed.sha256")" == "$staging_state_before" ]] ||
  fail "Dry-run changed Staging deployed state"
[[ "$(<"$state_root/production.deployed.sha256")" == "$production_state_before" ]] ||
  fail "Dry-run changed Production deployed state"
[[ "$(<"$state_root/monitor.deployed.sha256")" == "$monitor_state_before" ]] ||
  fail "Dry-run changed Monitor deployed state"
[[ $(grep -Fc -- '--dry-run' "$docker_log") -ge 3 ]] || fail "Dry-run did not cover all lineages"

# Key mismatch, unsafe key permissions, bad validity, Docker failure, and wrong host all fail closed.
staging_key=$(realpath "$certbot_root/live/staging.speak-up.top/privkey.pem")
cp "$staging_key" "$temporary_directory/staging-key.backup"
printf '%s\n' TYPE=PRIVATE_KEY PUBKEY=wrong-key >"$staging_key"
expect_failure "certificate/key mismatch" \
  "$manage" verify --environment staging --env-file "$environment_file"
cp "$temporary_directory/staging-key.backup" "$staging_key"
chmod 0644 "$staging_key"
expect_failure "unsafe key permissions" \
  "$manage" verify --environment staging --env-file "$environment_file"
chmod 0600 "$staging_key"

staging_certificate=$(realpath "$certbot_root/live/staging.speak-up.top/fullchain.pem")
cp "$staging_certificate" "$temporary_directory/staging-cert.backup"
sed 's/^START=.*/START=Aug 1 00:00:00 2020 GMT/; s/^END=.*/END=Aug 2 00:00:00 2020 GMT/' \
  "$temporary_directory/staging-cert.backup" >"$staging_certificate"
expect_failure "expired certificate" \
  "$manage" verify --environment staging --env-file "$environment_file"
cp "$temporary_directory/staging-cert.backup" "$staging_certificate"

reset_nginx_log
FAKE_DOCKER_FAIL_CERT_NAME=staging.speak-up.top \
  expect_failure "Certbot renewal failure" \
    "$manage" renew --env-file "$environment_file"
assert_no_reload "Certbot renewal failure"
FAKE_UNAME_MACHINE=arm64 expect_failure "non-amd64 host" \
  "$manage" renew-dry-run --env-file "$environment_file"

# The unit is deterministic, locked by the script, and scheduled twice daily.
grep -Fq 'ExecStart=/usr/local/sbin/xe3-speakup-tls renew --env-file /etc/speakup/tls.env' \
  "$tls_directory/xe3-speakup-tls-renew.service" || fail "systemd service entry point is wrong"
grep -Fxq 'StateDirectory=speakup/safety-checks' \
  "$tls_directory/xe3-speakup-tls-renew.service" || fail "systemd renewal state directory is wrong"
grep -Fxq 'StateDirectoryMode=0700' \
  "$tls_directory/xe3-speakup-tls-renew.service" || fail "systemd renewal state directory is not root-only"
grep -Fxq \
  'ExecStartPost=/usr/bin/install --no-target-directory --owner=root --group=root --mode=0600 /dev/null /var/lib/speakup/safety-checks/tls-renewal.success' \
  "$tls_directory/xe3-speakup-tls-renew.service" || fail "systemd renewal success marker is wrong"
! grep -Eq 'prepare-image|docker[[:space:]]+pull' \
  "$tls_directory/xe3-speakup-tls-renew.service" ||
  fail "systemd renewal can prepare or pull the Certbot image"
grep -Fq 'OnCalendar=*-*-* 03,15:17:00' "$tls_directory/xe3-speakup-tls-renew.timer" ||
  fail "systemd timer schedule is wrong"
grep -Fq 'RandomizedDelaySec=30m' "$tls_directory/xe3-speakup-tls-renew.timer" ||
  fail "systemd timer has no randomized delay"
grep -Fq 'Persistent=true' "$tls_directory/xe3-speakup-tls-renew.timer" ||
  fail "systemd timer is not persistent"
! grep -Fq 'TLS_CONTACT_EMAIL' \
  "$manage" "$tls_directory/tls.env.example" "$tls_directory/README.md" ||
  fail "TLS lifecycle still requires an obsolete contact email"
grep -Fq "Let's Encrypt stopped its certificate-expiration email service" \
  "$tls_directory/README.md" || fail "TLS documentation omits the independent expiry-monitoring requirement"

printf '%s\n' 'TLS lifecycle contract tests passed'
