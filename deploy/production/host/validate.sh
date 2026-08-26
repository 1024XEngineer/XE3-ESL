#!/usr/bin/env bash

set -euo pipefail

readonly ci_user="speakup-production-ci"
readonly broker="/usr/local/libexec/speakup-production-broker"
readonly gate="/usr/local/libexec/speakup-production-ssh-gate"
readonly state="/var/lib/speakup/production-broker"
readonly env_file="/etc/speakup/production.env"

(( EUID == 0 )) || { printf '%s\n' 'production host validation: run as root' >&2; exit 1; }
(( $# == 0 )) || { printf '%s\n' 'Usage: validate.sh' >&2; exit 1; }
fail() { printf 'production host validation: %s\n' "$*" >&2; exit 1; }

require_file() {
  local description=$1 path=$2 mode=$3
  [[ ! -L "$path" && -f "$path" && -s "$path" ]] || fail "$description is missing"
  [[ "$(stat -c '%u:%g:%a' "$path")" == "0:0:$mode" ]] || fail "$description has the wrong owner or mode"
}
require_directory() {
  local description=$1 path=$2 mode=$3
  [[ ! -L "$path" && -d "$path" ]] || fail "$description is missing"
  [[ "$(stat -c '%u:%g:%a' "$path")" == "0:0:$mode" ]] || fail "$description has the wrong owner or mode"
}

entry=$(getent passwd "$ci_user") || fail "CI account is missing"
IFS=: read -r name _ uid gid _ home shell <<< "$entry"
[[ "$name" == "$ci_user" && "$uid" != 0 && "$home" == /var/empty/speakup-production-ci && "$shell" == /bin/bash ]] || fail "CI account identity is invalid"
[[ "$(id -G "$ci_user" | wc -w | tr -d ' ')" == 1 ]] || fail "CI account has supplementary groups"
[[ "$(passwd -S "$ci_user" | awk '{print $2}')" == P ]] || fail "CI account cannot authenticate with its restricted key"

require_file "broker" "$broker" 555
require_file "SSH gate" "$gate" 555
require_file "sshd drop-in" /etc/ssh/sshd_config.d/60-speakup-production-ci.conf 644
require_file "authorized key" /etc/ssh/authorized_keys/speakup-production-ci 644
require_file "sudoers policy" /etc/sudoers.d/speakup-production-ci 440
require_file "Production environment" "$env_file" 600
for directory in "$state" "$state/manifests" "$state/engine-receipts" "$state/audit-receipts" "$state/incoming" /run/lock/xe3-speakup-production-broker; do
  require_directory "broker directory" "$directory" 700
done
[[ -L /opt/xe3-speakup-production-control/current ]] || fail "current contract pointer is missing"
require_file "Production manage contract" /opt/xe3-speakup-production-control/current/deploy/production/manage.sh 555
require_file "Production Compose contract" /opt/xe3-speakup-production-control/current/deploy/production/compose.yaml 444
require_file "Production Nginx contract" /opt/xe3-speakup-production-control/current/deploy/production/nginx.conf.template 444
require_file "Android publication contract" /opt/xe3-speakup-production-control/current/deploy/android-download/manage.sh 555

grep -Fxq 'restrict '"$(cut -d ' ' -f 2- /etc/ssh/authorized_keys/speakup-production-ci)" /etc/ssh/authorized_keys/speakup-production-ci || fail "authorized key is not restricted"
visudo -cf /etc/sudoers.d/speakup-production-ci >/dev/null
sudo -n -l -U "$ci_user" -u root -- "$broker" >/dev/null || fail "CI cannot call the no-argument broker"
if sudo -n -l -U "$ci_user" -u root -- "$broker" unexpected >/dev/null 2>&1; then fail "CI can pass broker arguments"; fi

effective=$(sshd -T -C "user=$ci_user,host=localhost,addr=127.0.0.1")
for expected in \
  'authenticationmethods publickey' \
  'authorizedkeysfile /etc/ssh/authorized_keys/speakup-production-ci' \
  'forcecommand /usr/local/libexec/speakup-production-ssh-gate' \
  'disableforwarding yes' \
  'permittty no' \
  'passwordauthentication no' \
  'kbdinteractiveauthentication no' \
  'x11forwarding no'; do
  grep -Fxq "$expected" <<< "$effective" || fail "effective sshd setting is missing: $expected"
done

temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
printf '%s\n' '{"protocol_version":1,"action":"inspect"}' > "$temporary/request.json"
tar -C "$temporary" -cf - request.json | "$broker" | jq -e '.protocol_version == 1 and .ok == true and .action == "inspect" and (.current_receipt_sha256 | test("^[0-9a-f]{64}$"))' >/dev/null || fail "broker state inspection failed"
printf '%s\n' 'Production CI is limited to the forced no-argument deployment broker.'
