#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly certbot_image="certbot/certbot@sha256:34ee91d2f43008eb78a007d22f23ed4b2eaa9a454cb27ca2c042b49527a695b4"
readonly certbot_platform="linux/amd64"
readonly minimum_validity_seconds=604800
readonly clock_skew_seconds=300
readonly tls_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly bootstrap_template="$tls_directory/nginx-http.conf.template"

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh prepare-image --env-file FILE
  manage.sh render-bootstrap --environment staging|production --env-file FILE --output FILE
  manage.sh issue-staging --env-file FILE
  manage.sh expand-production --env-file FILE
  manage.sh verify --environment staging|production --env-file FILE
  manage.sh activate --environment staging|production --env-file FILE
  manage.sh renew --env-file FILE
  manage.sh renew-dry-run --env-file FILE
EOF
}

fail() {
  printf 'tls lifecycle: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

valid_absolute_path() {
  local value=$1
  [[ "$value" =~ ^/[A-Za-z0-9._/-]+$ ]] &&
    [[ "$value" != *//* ]] &&
    [[ "$value" != */../* ]] &&
    [[ "$value" != */./* ]] &&
    [[ "$value" != */.. ]] &&
    [[ "$value" != */. ]]
}

file_mode() {
  if stat -Lc '%a' "$1" >/dev/null 2>&1; then
    stat -Lc '%a' "$1"
  else
    stat -Lf '%Lp' "$1"
  fi
}

file_owner() {
  if stat -Lc '%u' "$1" >/dev/null 2>&1; then
    stat -Lc '%u' "$1"
  else
    stat -Lf '%u' "$1"
  fi
}

require_owned_path() {
  local label=$1
  local path=$2
  [[ "$(file_owner "$path")" == "$(id -u)" ]] ||
    fail "$label must be owned by the invoking user"
}

require_private_directory() {
  local label=$1
  local path=$2
  local mode mode_value

  [[ -d "$path" && ! -L "$path" ]] || fail "$label must be a real directory"
  require_owned_path "$label" "$path"
  mode=$(file_mode "$path")
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "$label has an unreadable mode"
  mode_value=$((8#$mode))
  (( (mode_value & 0077) == 0 )) || fail "$label must not grant group or other access"
}

require_webroot_directory() {
  local label=$1
  local path=$2
  local mode mode_value

  [[ -d "$path" && ! -L "$path" ]] || fail "$label must be a real directory"
  require_owned_path "$label" "$path"
  mode=$(file_mode "$path")
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "$label has an unreadable mode"
  mode_value=$((8#$mode))
  (( (mode_value & 0022) == 0 )) || fail "$label must not be group or other writable"
  (( (mode_value & 0005) == 0005 )) || fail "$label must be readable and searchable by Nginx"
}

require_secure_file() {
  local label=$1
  local path=$2
  local mode mode_value

  [[ -f "$path" && ! -L "$path" ]] || fail "$label must be a regular file"
  require_owned_path "$label" "$path"
  mode=$(file_mode "$path")
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "$label has an unreadable mode"
  mode_value=$((8#$mode))
  (( (mode_value & 0077) == 0 )) || fail "$label must not grant group or other access"
}

allowed_configuration_key() {
  case "$1" in
    TLS_CONTACT_EMAIL | \
      TLS_CERTBOT_CONFIG_ROOT | \
      TLS_STAGING_ACME_ROOT | \
      TLS_PRODUCTION_ACME_ROOT | \
      TLS_STATE_ROOT | \
      TLS_NGINX_BINARY)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

load_configuration() {
  local file=$1
  local line line_number=0 name value
  local seen_keys=$'\n'

  require_secure_file "TLS environment file" "$file"

  unset \
    TLS_CONTACT_EMAIL \
    TLS_CERTBOT_CONFIG_ROOT \
    TLS_STAGING_ACME_ROOT \
    TLS_PRODUCTION_ACME_ROOT \
    TLS_STATE_ROOT \
    TLS_NGINX_BINARY

  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    line=${line%$'\r'}
    case "$line" in
      "" | \#*)
        continue
        ;;
    esac
    [[ "$line" == *=* ]] || fail "invalid environment syntax at line $line_number"
    name=${line%%=*}
    value=${line#*=}
    [[ "$name" =~ ^[A-Z][A-Z0-9_]*$ ]] ||
      fail "invalid environment key at line $line_number"
    allowed_configuration_key "$name" || fail "unsupported environment key: $name"
    case "$seen_keys" in
      *$'\n'"$name"$'\n'*) fail "duplicate environment key: $name" ;;
    esac
    seen_keys+="$name"$'\n'
    printf -v "$name" '%s' "$value"
    export "$name"
  done <"$file"
}

require_value() {
  local name=$1
  [[ -n "${!name:-}" ]] || fail "$name is required"
}

validate_configuration() {
  local name canonical_certbot canonical_staging canonical_production canonical_state
  local required=(
    TLS_CONTACT_EMAIL
    TLS_CERTBOT_CONFIG_ROOT
    TLS_STAGING_ACME_ROOT
    TLS_PRODUCTION_ACME_ROOT
    TLS_STATE_ROOT
    TLS_NGINX_BINARY
  )

  for name in "${required[@]}"; do
    require_value "$name"
  done

  [[ "$TLS_CONTACT_EMAIL" =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,63}$ ]] ||
    fail "TLS_CONTACT_EMAIL must be a valid contact address"

  for name in \
    TLS_CERTBOT_CONFIG_ROOT \
    TLS_STAGING_ACME_ROOT \
    TLS_PRODUCTION_ACME_ROOT \
    TLS_STATE_ROOT \
    TLS_NGINX_BINARY; do
    valid_absolute_path "${!name}" || fail "$name must be a safe absolute path"
  done

  [[ "$TLS_STAGING_ACME_ROOT" != "$TLS_PRODUCTION_ACME_ROOT" ]] ||
    fail "Staging and Production ACME webroots must be different"
  [[ "$TLS_CERTBOT_CONFIG_ROOT" != "$TLS_STATE_ROOT" ]] ||
    fail "Certbot configuration and deployment state roots must be different"

  require_private_directory "Certbot configuration root" "$TLS_CERTBOT_CONFIG_ROOT"
  require_private_directory "TLS deployment state root" "$TLS_STATE_ROOT"
  require_webroot_directory "Staging ACME webroot" "$TLS_STAGING_ACME_ROOT"
  require_webroot_directory "Production ACME webroot" "$TLS_PRODUCTION_ACME_ROOT"
  require_command realpath
  canonical_certbot=$(realpath "$TLS_CERTBOT_CONFIG_ROOT")
  canonical_staging=$(realpath "$TLS_STAGING_ACME_ROOT")
  canonical_production=$(realpath "$TLS_PRODUCTION_ACME_ROOT")
  canonical_state=$(realpath "$TLS_STATE_ROOT")
  [[ "$canonical_staging" != "$canonical_production" ]] ||
    fail "Staging and Production ACME webroots resolve to the same directory"
  [[ "$canonical_certbot" != "$canonical_state" ]] ||
    fail "Certbot configuration and deployment state resolve to the same directory"
  case "$canonical_state/" in
    "$canonical_certbot/"* | "$canonical_staging/"* | "$canonical_production/"*)
      fail "TLS deployment state must not be nested below another TLS root"
      ;;
  esac
  case "$canonical_certbot/" in
    "$canonical_state/"* | "$canonical_staging/"* | "$canonical_production/"*)
      fail "Certbot configuration must not be nested below another TLS root"
      ;;
  esac
  case "$canonical_staging/" in
    "$canonical_state/"* | "$canonical_certbot/"* | "$canonical_production/"*)
      fail "Staging ACME webroot must not be nested below another TLS root"
      ;;
  esac
  case "$canonical_production/" in
    "$canonical_state/"* | "$canonical_certbot/"* | "$canonical_staging/"*)
      fail "Production ACME webroot must not be nested below another TLS root"
      ;;
  esac

  [[ -x "$TLS_NGINX_BINARY" && -f "$TLS_NGINX_BINARY" && ! -L "$TLS_NGINX_BINARY" ]] ||
    fail "TLS_NGINX_BINARY must be an executable regular file"
  require_owned_path "Nginx binary" "$TLS_NGINX_BINARY"
  local nginx_mode nginx_mode_value
  nginx_mode=$(file_mode "$TLS_NGINX_BINARY")
  [[ "$nginx_mode" =~ ^[0-7]{3,4}$ ]] || fail "Nginx binary has an unreadable mode"
  nginx_mode_value=$((8#$nginx_mode))
  (( (nginx_mode_value & 0022) == 0 )) ||
    fail "Nginx binary must not be group or other writable"
}

validate_linux_amd64_host() {
  [[ "$(uname -s)" == Linux ]] || fail "Certbot requires a Linux host"
  [[ "$(uname -m)" == x86_64 ]] || fail "Certbot requires an amd64 host"
}

validate_certbot_runtime() {
  validate_linux_amd64_host
  require_command docker
  docker version >/dev/null || fail "Docker Engine is unavailable"
}

validate_prepared_certbot_image() {
  local inspect_output line image_os="" image_architecture=""
  local line_number=0
  local digest_found=false

  inspect_output=$(docker image inspect \
    --format '{{println .Os}}{{println .Architecture}}{{range .RepoDigests}}{{println .}}{{end}}' \
    "$certbot_image" 2>/dev/null) ||
    fail "the fixed Certbot image is not prepared; run prepare-image"
  while IFS= read -r line; do
    line_number=$((line_number + 1))
    case "$line_number" in
      1) image_os=$line ;;
      2) image_architecture=$line ;;
      *)
        [[ "$line" != "$certbot_image" ]] || digest_found=true
        ;;
    esac
  done <<<"$inspect_output"

  [[ "$image_os" == linux && "$image_architecture" == amd64 ]] ||
    fail "the prepared Certbot image is not linux/amd64"
  $digest_found || fail "the prepared Certbot image does not contain the fixed repository digest"
}

prepare_certbot_image() {
  validate_certbot_runtime
  if ! docker pull --platform "$certbot_platform" "$certbot_image"; then
    fail "failed to pull the fixed Certbot image"
  fi
  validate_prepared_certbot_image
  printf 'image=%s platform=%s prepared=true\n' "$certbot_image" "$certbot_platform"
}

select_environment() {
  selected_environment=$1
  case "$selected_environment" in
    staging)
      certificate_name="staging.speak-up.top"
      acme_root=$TLS_STAGING_ACME_ROOT
      expected_domains=(
        staging.speak-up.top
        staging-api.speak-up.top
      )
      exact_webroot_map='{"staging.speak-up.top":"/var/www/acme","staging-api.speak-up.top":"/var/www/acme"}'
      bootstrap_domains=(
        staging.speak-up.top
        staging-api.speak-up.top
      )
      ;;
    production)
      certificate_name="speak-up.top"
      acme_root=$TLS_PRODUCTION_ACME_ROOT
      expected_domains=(
        speak-up.top
        www.speak-up.top
        api.speak-up.top
      )
      exact_webroot_map='{"speak-up.top":"/var/www/acme","www.speak-up.top":"/var/www/acme","api.speak-up.top":"/var/www/acme"}'
      # speak-up.top and www.speak-up.top already have the legacy Portal vhost.
      # The temporary bootstrap include must not duplicate those server names.
      bootstrap_domains=(api.speak-up.top)
      ;;
    *)
      fail "environment must be staging or production"
      ;;
  esac
  certificate="$TLS_CERTBOT_CONFIG_ROOT/live/$certificate_name/fullchain.pem"
  certificate_key="$TLS_CERTBOT_CONFIG_ROOT/live/$certificate_name/privkey.pem"
  state_file="$TLS_STATE_ROOT/$selected_environment.deployed.sha256"
}

resolve_certificate_file() {
  local label=$1
  local path=$2
  local expected_leaf=$3
  local resolved archive_root basename

  require_command realpath
  [[ -L "$path" ]] || fail "$label must be a Certbot live symlink"
  resolved=$(realpath "$path") || fail "$label symlink cannot be resolved"
  archive_root="$(realpath "$TLS_CERTBOT_CONFIG_ROOT")/archive/$certificate_name"
  case "$resolved" in
    "$archive_root"/"$expected_leaf"[0-9]*.pem)
      ;;
    *)
      fail "$label must resolve inside its Certbot archive lineage"
      ;;
  esac
  basename=${resolved##*/}
  [[ "$basename" =~ ^${expected_leaf}[1-9][0-9]*\.pem$ ]] ||
    fail "$label target does not use Certbot's versioned filename"
  [[ -f "$resolved" && ! -L "$resolved" ]] || fail "$label target must be a regular file"
  require_owned_path "$label target" "$resolved"
  printf '%s\n' "$resolved"
}

validate_certificate_permissions() {
  local resolved_certificate resolved_key certificate_mode key_mode
  local certificate_mode_value key_mode_value

  resolved_certificate=$(resolve_certificate_file "certificate" "$certificate" fullchain)
  resolved_key=$(resolve_certificate_file "certificate key" "$certificate_key" privkey)
  certificate_mode=$(file_mode "$resolved_certificate")
  key_mode=$(file_mode "$resolved_key")
  [[ "$certificate_mode" =~ ^[0-7]{3,4}$ ]] || fail "certificate has an unreadable mode"
  [[ "$key_mode" =~ ^[0-7]{3,4}$ ]] || fail "certificate key has an unreadable mode"
  certificate_mode_value=$((8#$certificate_mode))
  key_mode_value=$((8#$key_mode))
  (( (certificate_mode_value & 0022) == 0 )) ||
    fail "certificate must not be group or other writable"
  (( (key_mode_value & 0077) == 0 )) ||
    fail "certificate key must not grant group or other access"
}

renewal_configuration_value() {
  local file=$1
  local key=$2
  sed -nE \
    "s|^[[:space:]]*$key[[:space:]]*=[[:space:]]*([^[:space:]#;]+)[[:space:]]*$|\\1|p" \
    "$file"
}

renewal_parameter_values() {
  local file=$1
  local requested_key=$2

  awk -v requested_key="$requested_key" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    {
      line = $0
      sub(/\r$/, "", line)
      if (line ~ /^[[:space:]]*\[renewalparams\][[:space:]]*$/) {
        in_renewal = 1
        next
      }
      if (line ~ /^[[:space:]]*\[/) {
        in_renewal = 0
        next
      }
      if (in_renewal && line ~ ("^[[:space:]]*" requested_key "[[:space:]]*=")) {
        sub("^[[:space:]]*" requested_key "[[:space:]]*=[[:space:]]*", "", line)
        line = trim(line)
        if (line !~ /^[^[:space:]#;]+$/) {
          exit 1
        }
        print line
      }
    }
  ' "$file"
}

renewal_webroot_map_entries() {
  local file=$1

  awk '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    {
      line = $0
      sub(/\r$/, "", line)
      if (line ~ /^[[:space:]]*($|#|;)/) {
        next
      }
      if (line ~ /^[[:space:]]*\[renewalparams\][[:space:]]*$/) {
        renewal_sections++
        section = "renewal"
        in_map = 0
        next
      }
      if (line ~ /^[[:space:]]*\[\[webroot_map\]\][[:space:]]*$/) {
        if (section != "renewal" || in_map) {
          exit 1
        }
        map_sections++
        in_map = 1
        next
      }
      if (line ~ /^[[:space:]]*\[/) {
        section = "other"
        in_map = 0
        next
      }
      if (in_map) {
        if (line !~ /^[[:space:]]*[a-z0-9.-]+[[:space:]]*=[[:space:]]*\/[A-Za-z0-9._\/-]+[[:space:]]*$/) {
          exit 1
        }
        separator = index(line, "=")
        key = trim(substr(line, 1, separator - 1))
        value = trim(substr(line, separator + 1))
        print key "\t" value
      }
    }
    END {
      if (renewal_sections != 1 || map_sections != 1) {
        exit 1
      }
    }
  ' "$file"
}

validate_renewal_webroot_map() {
  local renewal_file=$1
  local contract=$2
  local map_entries domain path expected_sorted map_domains_sorted
  local seen_domains=$'\n'
  local strict_paths=0 legacy_paths=0 entry_count=0
  local -a map_domains=()

  map_entries=$(renewal_webroot_map_entries "$renewal_file") ||
    fail "$selected_environment Certbot webroot map is malformed"
  while IFS=$'\t' read -r domain path; do
    [[ -n "$domain" ]] || continue
    case "$seen_domains" in
      *$'\n'"$domain"$'\n'*)
        fail "$selected_environment Certbot webroot map contains a duplicate domain"
        ;;
    esac
    seen_domains+="$domain"$'\n'
    map_domains+=("$domain")
    entry_count=$((entry_count + 1))
    case "$path" in
      /var/www/acme) strict_paths=$((strict_paths + 1)) ;;
      /var/www/certbot) legacy_paths=$((legacy_paths + 1)) ;;
      *) fail "$selected_environment Certbot webroot map contains an unexpected path" ;;
    esac
  done <<<"$map_entries"
  ((entry_count > 0)) || fail "$selected_environment Certbot webroot map is empty"
  map_domains_sorted=$(printf '%s\n' "${map_domains[@]}" | LC_ALL=C sort)

  case "$contract" in
    strict)
      expected_sorted=$(printf '%s\n' "${expected_domains[@]}" | LC_ALL=C sort)
      [[ "$map_domains_sorted" == "$expected_sorted" ]] ||
        fail "$selected_environment Certbot webroot map domains are not exact"
      ((strict_paths == entry_count)) ||
        fail "$selected_environment Certbot webroot map must use /var/www/acme"
      renewal_webroot_migration_required=false
      ;;
    production-pre-expansion)
      [[ "$selected_environment" == production ]] ||
        fail "internal renewal webroot contract is invalid"
      [[ "$map_domains_sorted" == "$certificate_domains_sorted" ]] ||
        fail "Production Certbot webroot map must exactly match the current certificate"
      if ((strict_paths == entry_count)); then
        renewal_webroot_migration_required=false
      elif [[ "$certificate_domains_sorted" == $'speak-up.top\nwww.speak-up.top' ]] &&
        ((legacy_paths == entry_count)); then
        renewal_webroot_migration_required=true
      else
        fail "Production legacy webroot map must be the exact two-name /var/www/certbot map"
      fi
      ;;
    *)
      fail "internal renewal webroot contract is invalid"
      ;;
  esac
}

validate_renewal_configuration() {
  local contract=${1:-strict}
  local renewal_file="$TLS_CERTBOT_CONFIG_ROOT/renewal/$certificate_name.conf"
  local mode mode_value authenticator autorenew server archive_dir cert_path key_path chain_path fullchain_path

  [[ -f "$renewal_file" && ! -L "$renewal_file" ]] ||
    fail "$selected_environment Certbot renewal configuration is missing"
  require_owned_path "$selected_environment Certbot renewal configuration" "$renewal_file"
  mode=$(file_mode "$renewal_file")
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "Certbot renewal configuration has an unreadable mode"
  mode_value=$((8#$mode))
  (( (mode_value & 0022) == 0 )) ||
    fail "Certbot renewal configuration must not be group or other writable"
  if grep -Eq '^[[:space:]]*(pre_hook|post_hook|renew_hook|deploy_hook)[[:space:]]*=' \
    "$renewal_file"; then
    fail "$selected_environment Certbot renewal hooks are not allowed"
  fi
  authenticator=$(renewal_configuration_value "$renewal_file" authenticator)
  [[ "$authenticator" == webroot ]] ||
    fail "$selected_environment Certbot renewal authenticator must be webroot"
  server=$(renewal_configuration_value "$renewal_file" server)
  [[ "$server" == https://acme-v02.api.letsencrypt.org/directory ]] ||
    fail "$selected_environment Certbot renewal CA is not the expected production endpoint"
  archive_dir=$(renewal_configuration_value "$renewal_file" archive_dir)
  cert_path=$(renewal_configuration_value "$renewal_file" cert)
  key_path=$(renewal_configuration_value "$renewal_file" privkey)
  chain_path=$(renewal_configuration_value "$renewal_file" chain)
  fullchain_path=$(renewal_configuration_value "$renewal_file" fullchain)
  [[ "$archive_dir" == "/etc/letsencrypt/archive/$certificate_name" ]] ||
    fail "$selected_environment Certbot renewal archive path is invalid"
  [[ "$cert_path" == "/etc/letsencrypt/live/$certificate_name/cert.pem" ]] ||
    fail "$selected_environment Certbot renewal certificate path is invalid"
  [[ "$key_path" == "/etc/letsencrypt/live/$certificate_name/privkey.pem" ]] ||
    fail "$selected_environment Certbot renewal private key path is invalid"
  [[ "$chain_path" == "/etc/letsencrypt/live/$certificate_name/chain.pem" ]] ||
    fail "$selected_environment Certbot renewal chain path is invalid"
  [[ "$fullchain_path" == "/etc/letsencrypt/live/$certificate_name/fullchain.pem" ]] ||
    fail "$selected_environment Certbot renewal fullchain path is invalid"
  autorenew=$(renewal_parameter_values "$renewal_file" autorenew) ||
    fail "$selected_environment Certbot autorenew value is malformed"
  case "$autorenew" in
    "" | True)
      ;;
    *)
      fail "$selected_environment Certbot autorenew must be absent or exactly True"
      ;;
  esac
  validate_renewal_webroot_map "$renewal_file" "$contract"
}

read_certificate_domains() {
  local san_output token domain
  local seen_domains=$'\n'
  local parsed_count=0
  local -a parsed_domains=()

  san_output=$(openssl x509 -in "$certificate" -noout -ext subjectAltName) ||
    fail "certificate Subject Alternative Name extension is unreadable"
  while IFS= read -r token; do
    token=$(printf '%s' "$token" | tr -d '[:space:]')
    [[ -n "$token" ]] || continue
    [[ "$token" == DNS:* ]] || fail "certificate contains a non-DNS SAN"
    domain=${token#DNS:}
    [[ "$domain" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]] ||
      fail "certificate contains an invalid DNS SAN"
    case "$seen_domains" in
      *$'\n'"$domain"$'\n'*) fail "certificate contains a duplicate DNS SAN" ;;
    esac
    seen_domains+="$domain"$'\n'
    parsed_domains+=("$domain")
    parsed_count=$((parsed_count + 1))
  done < <(printf '%s\n' "$san_output" | sed '1d' | tr ',' '\n')

  ((parsed_count > 0)) || fail "certificate has no DNS SANs"
  certificate_domains_sorted=$(printf '%s\n' "${parsed_domains[@]}" | LC_ALL=C sort)
}

validate_exact_domains() {
  local expected_sorted
  expected_sorted=$(printf '%s\n' "${expected_domains[@]}" | LC_ALL=C sort)
  [[ "$certificate_domains_sorted" == "$expected_sorted" ]] ||
    fail "$selected_environment certificate SANs do not exactly match the release contract"
}

validate_production_subset_domains() {
  local legacy_domains_sorted expected_sorted
  legacy_domains_sorted=$(printf '%s\n' speak-up.top www.speak-up.top | LC_ALL=C sort)
  expected_sorted=$(printf '%s\n' "${expected_domains[@]}" | LC_ALL=C sort)
  if [[ "$certificate_domains_sorted" == "$legacy_domains_sorted" ]]; then
    production_certificate_already_exact=false
    return
  fi
  if [[ "$certificate_domains_sorted" == "$expected_sorted" ]]; then
    production_certificate_already_exact=true
    return
  fi
  fail "existing Production certificate SANs must be the legacy pair or exact release set"
}

validate_public_key_match() {
  local temporary_directory
  temporary_directory=$(mktemp -d "$TLS_STATE_ROOT/.key-check.XXXXXX")
  chmod 0700 "$temporary_directory"
  if ! openssl x509 -in "$certificate" -pubkey -noout |
    openssl pkey -pubin -outform DER >"$temporary_directory/certificate.der"; then
    rm -rf "$temporary_directory"
    fail "certificate public key is unreadable"
  fi
  if ! openssl pkey -in "$certificate_key" -passin pass: -pubout -outform DER \
    >"$temporary_directory/private-key.der"; then
    rm -rf "$temporary_directory"
    fail "certificate private key is unreadable or encrypted"
  fi
  if ! cmp -s "$temporary_directory/certificate.der" \
    "$temporary_directory/private-key.der"; then
    rm -rf "$temporary_directory"
    fail "certificate and private key do not match"
  fi
  rm -rf "$temporary_directory"
}

validate_validity_window() {
  local policy=$1
  local not_before_line not_after_line not_before not_after now

  not_before_line=$(openssl x509 -in "$certificate" -noout -startdate) ||
    fail "certificate start date is unreadable"
  not_after_line=$(openssl x509 -in "$certificate" -noout -enddate) ||
    fail "certificate end date is unreadable"
  [[ "$not_before_line" == notBefore=* ]] || fail "certificate start date is malformed"
  [[ "$not_after_line" == notAfter=* ]] || fail "certificate end date is malformed"
  not_before=$(LC_ALL=C date --utc --date="${not_before_line#notBefore=}" +%s) ||
    fail "certificate start date cannot be parsed"
  not_after=$(LC_ALL=C date --utc --date="${not_after_line#notAfter=}" +%s) ||
    fail "certificate end date cannot be parsed"
  now=$(date --utc +%s) || fail "current time is unavailable"
  ((not_before <= now + clock_skew_seconds)) || fail "certificate is not valid yet"
  ((not_after > not_before)) || fail "certificate validity interval is invalid"
  case "$policy" in
    pre-renew)
      ((not_after >= now + clock_skew_seconds)) ||
        fail "certificate expires inside the five-minute clock-skew safety window"
      ;;
    release)
      ((not_after >= now + minimum_validity_seconds)) ||
        fail "certificate has less than seven days of validity remaining"
      ;;
    *)
      fail "internal certificate validity policy is invalid"
      ;;
  esac
  certificate_not_after=${not_after_line#notAfter=}
}

certificate_sha256() {
  sha256sum "$certificate" | awk '{print $1}'
}

verify_selected_certificate() {
  local domain_mode=${1:-exact}
  local validity_policy=${2:-release}

  validate_certificate_runtime
  validate_certificate_permissions
  openssl x509 -in "$certificate" -noout >/dev/null || fail "certificate is invalid"
  read_certificate_domains
  case "$domain_mode" in
    exact)
      validate_exact_domains
      ;;
    production-subset)
      validate_production_subset_domains
      ;;
    *)
      fail "internal certificate domain mode is invalid"
      ;;
  esac
  validate_public_key_match
  validate_validity_window "$validity_policy"
  current_certificate_sha256=$(certificate_sha256)
  [[ "$current_certificate_sha256" =~ ^[0-9a-f]{64}$ ]] ||
    fail "certificate SHA-256 is invalid"
}

validate_certificate_runtime() {
  require_command awk
  require_command cmp
  require_command date
  require_command openssl
  require_command sed
  require_command sha256sum
  require_command sort
  require_command tr
}

render_bootstrap() {
  local output=$1
  local server_names temporary

  valid_absolute_path "$output" || fail "--output must be a safe absolute path"
  [[ -d "$(dirname "$output")" ]] || fail "Nginx output directory does not exist"
  [[ ! -d "$output" ]] || fail "Nginx output must not be a directory"
  server_names=$(printf '%s ' "${bootstrap_domains[@]}")
  server_names=${server_names% }
  temporary=$(mktemp "${output}.tmp.XXXXXX")
  if ! sed \
    -e "s|__TLS_SERVER_NAMES__|$server_names|g" \
    -e "s|__TLS_ACME_ROOT__|$acme_root|g" \
    -e "s|__TLS_BOOTSTRAP_ENVIRONMENT__|$selected_environment|g" \
    "$bootstrap_template" >"$temporary"; then
    rm -f "$temporary"
    fail "failed to render HTTP-01 bootstrap configuration"
  fi
  if grep -Eq '__TLS_[A-Z_]+__' "$temporary"; then
    rm -f "$temporary"
    fail "HTTP-01 bootstrap configuration contains an unresolved placeholder"
  fi
  chmod 0644 "$temporary"
  mv -f "$temporary" "$output"
  printf 'environment=%s bootstrap=%s rendered=true\n' "$selected_environment" "$output"
}

run_certbot() {
  validate_certbot_runtime
  validate_prepared_certbot_image
  docker run \
    --rm \
    --name "xe3-certbot-$selected_environment-$$" \
    --platform "$certbot_platform" \
    --pull never \
    --read-only \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
    --volume "$TLS_CERTBOT_CONFIG_ROOT:/etc/letsencrypt" \
    --volume "$acme_root:/var/www/acme" \
    "$certbot_image" \
    --config /dev/null \
    --config-dir /etc/letsencrypt \
    --work-dir /tmp/work \
    --logs-dir /tmp/logs \
    --no-directory-hooks \
    "$@"
}

lineage_exists() {
  [[ -e "$TLS_CERTBOT_CONFIG_ROOT/live/$certificate_name" ||
    -L "$TLS_CERTBOT_CONFIG_ROOT/live/$certificate_name" ||
    -e "$TLS_CERTBOT_CONFIG_ROOT/archive/$certificate_name" ||
    -L "$TLS_CERTBOT_CONFIG_ROOT/archive/$certificate_name" ||
    -e "$TLS_CERTBOT_CONFIG_ROOT/renewal/$certificate_name.conf" ||
    -L "$TLS_CERTBOT_CONFIG_ROOT/renewal/$certificate_name.conf" ]]
}

migrate_legacy_production_webroot() {
  local certificate_sha_before=$current_certificate_sha256

  if ! run_certbot reconfigure \
    --non-interactive \
    --cert-name "$certificate_name" \
    --webroot-path /var/www/acme; then
    fail "Certbot failed to migrate the Production webroot configuration"
  fi
  verify_selected_certificate production-subset
  [[ "$current_certificate_sha256" == "$certificate_sha_before" ]] ||
    fail "Production webroot migration unexpectedly changed the live certificate"
  validate_renewal_configuration production-pre-expansion
  ! $renewal_webroot_migration_required ||
    fail "Certbot did not normalize the Production webroot map"
  printf 'environment=production webroot_map=/var/www/acme migrated=true certificate_changed=false reload=false\n'
}

issue_certificate() {
  local before_sha="" expansion=false
  local -a domain_arguments=()
  local domain

  validate_certificate_runtime
  if lineage_exists; then
    [[ "$selected_environment" == production ]] ||
      fail "Staging certificate lineage already exists; use renew"
    [[ (-e "$certificate" || -L "$certificate") &&
      (-e "$certificate_key" || -L "$certificate_key") ]] ||
      fail "Production certificate lineage is incomplete"
    verify_selected_certificate production-subset
    validate_renewal_configuration production-pre-expansion
    if $renewal_webroot_migration_required; then
      migrate_legacy_production_webroot
    fi
    before_sha=$current_certificate_sha256
    if $production_certificate_already_exact; then
      printf 'environment=production certificate_name=%s certificate_sha256=%s not_after=%s expanded=false already_exact=true reload=false\n' \
        "$certificate_name" "$current_certificate_sha256" "$certificate_not_after"
      return
    fi
    expansion=true
  elif [[ "$selected_environment" == production ]]; then
    fail "existing speak-up.top certificate lineage is required for Production expansion"
  fi

  for domain in "${expected_domains[@]}"; do
    domain_arguments+=(--domains "$domain")
  done

  local -a certbot_arguments=(
    certonly
    --non-interactive
    --agree-tos
    --no-eff-email
    --email "$TLS_CONTACT_EMAIL"
    --webroot
    --webroot-path /var/www/acme
    --webroot-map "$exact_webroot_map"
    --preferred-challenges http
    --cert-name "$certificate_name"
  )
  $expansion && certbot_arguments+=(--expand)
  certbot_arguments+=("${domain_arguments[@]}")

  if ! run_certbot "${certbot_arguments[@]}"; then
    fail "Certbot issuance failed for $selected_environment"
  fi
  verify_selected_certificate exact
  validate_renewal_configuration
  if [[ -n "$before_sha" && "$before_sha" == "$current_certificate_sha256" ]]; then
    fail "Production certificate was not expanded"
  fi
  printf 'environment=%s certificate_name=%s certificate_sha256=%s not_after=%s issued=true reload=false\n' \
    "$selected_environment" "$certificate_name" "$current_certificate_sha256" \
    "$certificate_not_after"
}

read_deployed_sha() {
  [[ -f "$state_file" && ! -L "$state_file" ]] ||
    fail "$selected_environment deployed certificate state is missing; run activate"
  require_owned_path "$selected_environment deployed certificate state" "$state_file"
  local mode mode_value
  mode=$(file_mode "$state_file")
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "deployed certificate state has an unreadable mode"
  mode_value=$((8#$mode))
  (( (mode_value & 0077) == 0 )) ||
    fail "deployed certificate state must not grant group or other access"
  IFS= read -r deployed_certificate_sha256 <"$state_file"
  [[ "$deployed_certificate_sha256" =~ ^[0-9a-f]{64}$ ]] ||
    fail "$selected_environment deployed certificate state is invalid"
  [[ $(wc -l <"$state_file" | tr -d '[:space:]') == 1 &&
    $(wc -c <"$state_file" | tr -d '[:space:]') == 65 ]] ||
    fail "$selected_environment deployed certificate state must contain exactly one SHA-256"
}

write_deployed_sha() {
  local sha=$1
  local temporary
  [[ ! -L "$state_file" ]] || fail "deployed certificate state must not be a symlink"
  temporary=$(mktemp "$TLS_STATE_ROOT/.deployed.XXXXXX")
  chmod 0600 "$temporary"
  printf '%s\n' "$sha" >"$temporary"
  mv -f "$temporary" "$state_file"
}

test_nginx_configuration() {
  local environment
  "$TLS_NGINX_BINARY" -t || fail "nginx -t failed; configuration was not reloaded"
  for environment in "$@"; do
    validate_loaded_nginx_contract "$environment"
  done
}

test_and_reload_nginx() {
  test_nginx_configuration "$@"
  "$TLS_NGINX_BINARY" -s reload || fail "Nginx graceful reload failed"
}

validate_loaded_nginx_contract() {
  local environment=$1
  local temporary expected_domain_csv bootstrap_count

  select_environment "$environment"
  expected_domain_csv=$(printf '%s,' "${expected_domains[@]}")
  expected_domain_csv=${expected_domain_csv%,}

  temporary=$(mktemp "$TLS_STATE_ROOT/.nginx-dump.XXXXXX")
  chmod 0600 "$temporary"
  if ! "$TLS_NGINX_BINARY" -T >"$temporary" 2>&1; then
    rm -f "$temporary"
    fail "nginx -T failed; configuration was not reloaded"
  fi
  if ! awk \
    -v expected_csv="$expected_domain_csv" \
    -v expected_certificate="$certificate" \
    -v expected_key="$certificate_key" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    function without_comment(value, output, character, quote, escaped, position) {
      output = ""
      quote = ""
      escaped = 0
      for (position = 1; position <= length(value); position++) {
        character = substr(value, position, 1)
        if (escaped) {
          output = output character
          escaped = 0
        } else if (character == "\\") {
          output = output character
          escaped = 1
        } else if (quote != "") {
          output = output character
          if (character == quote) quote = ""
        } else if (character == "\"" || character == "\047") {
          output = output character
          quote = character
        } else if (character == "#") {
          break
        } else {
          output = output character
        }
      }
      return output
    }
    function brace_delta(value, character, quote, escaped, position, delta) {
      quote = ""
      escaped = 0
      delta = 0
      for (position = 1; position <= length(value); position++) {
        character = substr(value, position, 1)
        if (escaped) {
          escaped = 0
        } else if (character == "\\") {
          escaped = 1
        } else if (quote != "") {
          if (character == quote) quote = ""
        } else if (character == "\"" || character == "\047") {
          quote = character
        } else if (character == "{") {
          delta++
        } else if (character == "}") {
          delta--
        }
      }
      return delta
    }
    function reset_server(name) {
      for (name in server_names) delete server_names[name]
      server_has_443_ssl = 0
      certificate_count = 0
      certificate_value = ""
      key_count = 0
      key_value = ""
    }
    function finish_server(name) {
      if (!server_has_443_ssl) return
      for (name in server_names) {
        if (!(name in expected_domains)) continue
        domain_blocks[name]++
        if (server_names[name] != 1 ||
            certificate_count != 1 || certificate_value != expected_certificate ||
            key_count != 1 || key_value != expected_key) {
          invalid = 1
        }
      }
    }
    BEGIN {
      expected_count = split(expected_csv, expected_list, ",")
      for (position = 1; position <= expected_count; position++) {
        expected_domains[expected_list[position]] = 1
      }
    }
    {
      content = without_comment($0)
      directive = trim(content)
      opening_server = 0
      if (!in_server && directive ~ /^server[[:space:]]*\{$/) {
        reset_server()
        in_server = 1
        server_depth = depth + 1
        opening_server = 1
      } else if (in_server && depth == server_depth && directive != "") {
        field_count = split(directive, fields, /[[:space:]]+/)
        keyword = fields[1]
        if (keyword == "listen" || keyword == "server_name" ||
            keyword == "ssl_certificate" || keyword == "ssl_certificate_key") {
          if (directive !~ /;$/) invalid = 1
          sub(/;$/, "", fields[field_count])
        }
        if (keyword == "listen" && field_count >= 2) {
          listen_has_ssl = 0
          for (position = 3; position <= field_count; position++) {
            if (fields[position] == "ssl") listen_has_ssl = 1
          }
          if (fields[2] ~ /(^|:)443$/ && listen_has_ssl) {
            server_has_443_ssl = 1
          }
        } else if (keyword == "server_name" && field_count >= 2) {
          for (position = 2; position <= field_count; position++) {
            server_names[fields[position]]++
          }
        } else if (keyword == "ssl_certificate") {
          certificate_count++
          if (field_count != 2) invalid = 1
          certificate_value = fields[2]
        } else if (keyword == "ssl_certificate_key") {
          key_count++
          if (field_count != 2) invalid = 1
          key_value = fields[2]
        }
      }

      depth += brace_delta(content)
      if (depth < 0) invalid = 1
      if (in_server && !opening_server && depth < server_depth) {
        finish_server()
        in_server = 0
      }
    }
    END {
      if (depth != 0 || in_server) invalid = 1
      for (name in expected_domains) {
        if (domain_blocks[name] != 1) invalid = 1
      }
      exit invalid
    }
  ' "$temporary"; then
    rm -f "$temporary"
    fail "$environment Nginx TLS server blocks do not match the final release contract"
  fi
  bootstrap_count=$(awk -v environment="$environment" '
    $1 == "set" && $2 == "$speakup_tls_bootstrap" &&
      $3 == environment ";" && NF == 3 { count++ }
    END { print count + 0 }
  ' "$temporary")
  if ((bootstrap_count > 0)); then
    rm -f "$temporary"
    fail "$environment HTTP-01 bootstrap is still loaded"
  fi
  rm -f "$temporary"
}

activate_selected_certificate() {
  local deployed=""
  verify_selected_certificate exact
  validate_renewal_configuration
  if [[ -e "$state_file" || -L "$state_file" ]]; then
    read_deployed_sha
    deployed=$deployed_certificate_sha256
  fi
  if [[ "$deployed" == "$current_certificate_sha256" ]]; then
    test_nginx_configuration "$selected_environment"
    printf 'environment=%s certificate_sha256=%s activated=true reload=false\n' \
      "$selected_environment" "$current_certificate_sha256"
    return
  fi
  test_and_reload_nginx "$selected_environment"
  write_deployed_sha "$current_certificate_sha256"
  printf 'environment=%s certificate_sha256=%s activated=true reload=true\n' \
    "$selected_environment" "$current_certificate_sha256"
}

renew_all_certificates() {
  local environment
  local -a environments=(staging production)
  local -a changed_environments=()
  local -a changed_shas=()
  local -a final_shas=()
  local -a final_expiries=()

  for environment in "${environments[@]}"; do
    select_environment "$environment"
    read_deployed_sha
    verify_selected_certificate exact pre-renew
    validate_renewal_configuration
  done

  for environment in "${environments[@]}"; do
    select_environment "$environment"
    if ! run_certbot renew \
      --non-interactive \
      --no-random-sleep-on-renew \
      --cert-name "$certificate_name" \
      --webroot-path /var/www/acme; then
      fail "Certbot renewal failed for $selected_environment"
    fi
  done

  for environment in "${environments[@]}"; do
    select_environment "$environment"
    read_deployed_sha
    verify_selected_certificate exact
    validate_renewal_configuration
    final_shas+=("$current_certificate_sha256")
    final_expiries+=("$certificate_not_after")
    if [[ "$current_certificate_sha256" != "$deployed_certificate_sha256" ]]; then
      changed_environments+=("$environment")
      changed_shas+=("$current_certificate_sha256")
    fi
  done

  local index
  for ((index = 0; index < ${#environments[@]}; index++)); do
    printf 'environment=%s certificate_sha256=%s not_after=%s verified=true\n' \
      "${environments[$index]}" "${final_shas[$index]}" "${final_expiries[$index]}"
  done

  if ((${#changed_environments[@]} == 0)); then
    printf 'certificates=staging,production renewed=true reload=false\n'
    return
  fi

  test_and_reload_nginx staging production
  for ((index = 0; index < ${#changed_environments[@]}; index++)); do
    select_environment "${changed_environments[$index]}"
    write_deployed_sha "${changed_shas[$index]}"
  done
  printf 'certificates=staging,production renewed=true reload=true\n'
}

renew_dry_run_selected() {
  local before_sha
  verify_selected_certificate exact pre-renew
  validate_renewal_configuration
  before_sha=$current_certificate_sha256
  if ! run_certbot renew \
    --dry-run \
    --non-interactive \
    --no-random-sleep-on-renew \
    --cert-name "$certificate_name" \
    --webroot-path /var/www/acme; then
    fail "Certbot renewal dry-run failed for $selected_environment"
  fi
  verify_selected_certificate exact pre-renew
  validate_renewal_configuration
  [[ "$before_sha" == "$current_certificate_sha256" ]] ||
    fail "renewal dry-run unexpectedly changed the live certificate"
  printf 'environment=%s renewal_dry_run=true reload=false\n' "$selected_environment"
}

renew_dry_run_all() {
  local environment
  for environment in staging production; do
    select_environment "$environment"
    renew_dry_run_selected
  done
}

acquire_lock() {
  local lock_file="$TLS_STATE_ROOT/lifecycle.lock"
  require_command flock
  [[ ! -L "$lock_file" ]] || fail "TLS lifecycle lock must not be a symlink"
  exec 9>"$lock_file"
  flock -n 9 || fail "another TLS lifecycle operation is already running"
  chmod 0600 "$lock_file"
}

main() {
  local command=${1:-}
  local environment=""
  local environment_file=""
  local output=""
  local environment_seen=false env_file_seen=false output_seen=false

  [[ -n "$command" ]] || {
    usage
    exit 2
  }
  shift
  while (($# > 0)); do
    case "$1" in
      --environment)
        ! $environment_seen || fail "--environment may only be provided once"
        (($# >= 2)) || fail "--environment requires a value"
        environment=$2
        environment_seen=true
        shift 2
        ;;
      --env-file)
        ! $env_file_seen || fail "--env-file may only be provided once"
        (($# >= 2)) || fail "--env-file requires a value"
        environment_file=$2
        env_file_seen=true
        shift 2
        ;;
      --output)
        ! $output_seen || fail "--output may only be provided once"
        (($# >= 2)) || fail "--output requires a value"
        output=$2
        output_seen=true
        shift 2
        ;;
      *)
        fail "unknown or misplaced argument"
        ;;
    esac
  done

  [[ -n "$environment_file" ]] || fail "--env-file is required"
  load_configuration "$environment_file"
  validate_configuration

  case "$command" in
    prepare-image | issue-staging | expand-production | renew | renew-dry-run)
      [[ -z "$environment" ]] || fail "$command does not accept --environment"
      ;;
    render-bootstrap | verify | activate)
      [[ -n "$environment" ]] || fail "$command requires --environment"
      select_environment "$environment"
      ;;
    *)
      usage
      exit 2
      ;;
  esac

  case "$command" in
    prepare-image)
      [[ -z "$output" ]] || fail "prepare-image does not accept --output"
      acquire_lock
      prepare_certbot_image
      ;;
    render-bootstrap)
      [[ -n "$output" ]] || fail "render-bootstrap requires --output"
      render_bootstrap "$output"
      ;;
    issue-staging)
      [[ -z "$output" ]] || fail "issue-staging does not accept --output"
      select_environment staging
      acquire_lock
      issue_certificate
      ;;
    expand-production)
      [[ -z "$output" ]] || fail "expand-production does not accept --output"
      select_environment production
      acquire_lock
      issue_certificate
      ;;
    verify)
      [[ -z "$output" ]] || fail "verify does not accept --output"
      verify_selected_certificate exact
      validate_renewal_configuration
      printf 'environment=%s certificate_name=%s certificate_sha256=%s not_after=%s verified=true\n' \
        "$selected_environment" "$certificate_name" "$current_certificate_sha256" \
        "$certificate_not_after"
      ;;
    activate)
      [[ -z "$output" ]] || fail "activate does not accept --output"
      acquire_lock
      activate_selected_certificate
      ;;
    renew)
      [[ -z "$output" ]] || fail "renew does not accept --output"
      acquire_lock
      renew_all_certificates
      ;;
    renew-dry-run)
      [[ -z "$output" ]] || fail "renew-dry-run does not accept --output"
      acquire_lock
      renew_dry_run_all
      ;;
  esac
}

main "$@"
