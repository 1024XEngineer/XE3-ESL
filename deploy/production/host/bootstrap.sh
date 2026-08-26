#!/usr/bin/env bash

set -euo pipefail

readonly ci_user="speakup-production-ci"
readonly ci_home="/var/empty/speakup-production-ci"
readonly broker_path="/usr/local/libexec/speakup-production-broker"
readonly gate_path="/usr/local/libexec/speakup-production-ssh-gate"
readonly control_root="/opt/xe3-speakup-production-control"
readonly state_root="/var/lib/speakup/production-broker"
readonly sshd_drop_in="/etc/ssh/sshd_config.d/60-speakup-production-ci.conf"
readonly authorized_keys_file="/etc/ssh/authorized_keys/speakup-production-ci"
readonly sudoers_file="/etc/sudoers.d/speakup-production-ci"
readonly tmpfiles_file="/etc/tmpfiles.d/xe3-speakup-production-broker.conf"
readonly trusted_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

PATH=$trusted_path
export PATH
(( EUID == 0 )) || { printf '%s\n' 'production host bootstrap: run as root' >&2; exit 1; }

fail() { printf 'production host bootstrap: %s\n' "$*" >&2; exit 1; }
usage() {
  cat >&2 <<'EOF'
Usage: bootstrap.sh \
  --broker-binary ABS \
  --contract-directory ABS \
  --contract-revision 40HEX \
  --ssh-public-key-file ABS
EOF
}

safe_absolute_path() {
  [[ "$1" == /* && "$1" != *//* && "$1" != */../* && "$1" != */./* &&
     "$1" != */.. && "$1" != */. ]]
}

require_source_file() {
  [[ ! -L "$2" && -f "$2" && -s "$2" ]] || fail "$1 must be a non-empty regular file"
}

require_linux_amd64_elf() {
  local header
  header=$(od -An -tx1 -N20 -- "$1" | tr -d '[:space:]')
  [[ "$header" =~ ^7f454c46020101[0-9a-f]{18}(0200|0300)3e00$ ]] ||
    fail "broker binary must be a Linux amd64 ELF executable"
}

broker_binary=""
contract_directory=""
contract_revision=""
ssh_public_key_file=""
while (( $# > 0 )); do
  case "$1" in
    --broker-binary|--contract-directory|--contract-revision|--ssh-public-key-file)
      (( $# >= 2 )) || fail "$1 requires one value"
      flag=$1
      value=$2
      case "$flag" in
        --broker-binary) [[ -z "$broker_binary" ]] || fail "$flag is duplicated"; broker_binary=$value ;;
        --contract-directory) [[ -z "$contract_directory" ]] || fail "$flag is duplicated"; contract_directory=$value ;;
        --contract-revision) [[ -z "$contract_revision" ]] || fail "$flag is duplicated"; contract_revision=$value ;;
        --ssh-public-key-file) [[ -z "$ssh_public_key_file" ]] || fail "$flag is duplicated"; ssh_public_key_file=$value ;;
      esac
      shift 2
      ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$broker_binary" && -n "$contract_directory" && -n "$contract_revision" && -n "$ssh_public_key_file" ]] || { usage; fail "all arguments are required"; }
safe_absolute_path "$broker_binary" || fail "broker binary path is unsafe"
safe_absolute_path "$contract_directory" || fail "contract directory path is unsafe"
safe_absolute_path "$ssh_public_key_file" || fail "SSH public key path is unsafe"
[[ "$contract_revision" =~ ^[0-9a-f]{40}$ ]] || fail "contract revision must be a full lowercase SHA"
[[ ! -L "$contract_directory" && -d "$contract_directory" ]] || fail "contract directory must be real"
require_source_file "broker binary" "$broker_binary"
[[ -x "$broker_binary" ]] || fail "broker binary is not executable"
require_source_file "SSH public key" "$ssh_public_key_file"
for file in deploy/production/manage.sh deploy/production/compose.yaml deploy/production/nginx.conf.template deploy/android-download/manage.sh; do
  require_source_file "$file" "$contract_directory/$file"
done
require_linux_amd64_elf "$broker_binary"

public_key=""
line_count=0
while IFS= read -r line || [[ -n "$line" ]]; do
  line_count=$((line_count + 1))
  public_key=${line%$'\r'}
done < "$ssh_public_key_file"
(( line_count == 1 )) || fail "SSH public key file must contain one line"
[[ "$public_key" =~ ^ssh-ed25519[[:space:]][A-Za-z0-9+/]+={0,3}([[:space:]][^[:cntrl:]]+)?$ ]] ||
  fail "SSH public key must be one Ed25519 key"

for command in awk chmod chown chpasswd cmp getent groupadd id install ln mktemp mv od openssl passwd rm sshd stat systemctl systemd-tmpfiles tr useradd visudo wc; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done

getent group "$ci_user" >/dev/null || groupadd "$ci_user"
getent passwd "$ci_user" >/dev/null || useradd --gid "$ci_user" --home-dir "$ci_home" --no-create-home --shell /bin/bash --comment "SpeakUp Production CI forced command" --password '!' "$ci_user"
[[ "$(id -u "$ci_user")" != 0 ]] || fail "CI account cannot be root"
[[ "$(id -G "$ci_user" | wc -w | tr -d ' ')" == 1 ]] || fail "CI account must not have supplementary groups"
if [[ "$(passwd -S "$ci_user" | awk '{print $2}')" != P ]]; then
  printf '%s:%s\n' "$ci_user" "$(openssl rand -hex 48)" | chpasswd
fi

install -d -o root -g root -m 755 /var/empty /var/lib/speakup /usr/local/libexec /etc/ssh/authorized_keys /etc/ssh/sshd_config.d /etc/sudoers.d /etc/tmpfiles.d
install -d -o root -g root -m 555 "$ci_home"
install -d -o root -g root -m 700 "$state_root" "$state_root/manifests" "$state_root/engine-receipts" "$state_root/audit-receipts" "$state_root/incoming"
install -d -o root -g root -m 755 "$control_root" "$control_root/releases"
install -d -o root -g root -m 700 /run/lock/xe3-speakup-production-broker

install -o root -g root -m 0555 "$broker_binary" "$broker_path"
install -o root -g root -m 0555 "$script_directory/ssh-gate" "$gate_path"
install -o root -g root -m 0644 "$script_directory/sshd_config" "$sshd_drop_in"
install -o root -g root -m 0440 "$script_directory/sudoers" "$sudoers_file"
install -o root -g root -m 0444 "$script_directory/tmpfiles.conf" "$tmpfiles_file"

key_temporary=$(mktemp)
trap 'rm -f -- "$key_temporary"' EXIT
printf 'restrict %s\n' "$public_key" > "$key_temporary"
install -o root -g root -m 0644 "$key_temporary" "$authorized_keys_file"

release="$control_root/releases/$contract_revision"
if [[ -e "$release" || -L "$release" ]]; then
  [[ ! -L "$release" && -d "$release" ]] || fail "existing contract revision is invalid"
  for file in deploy/production/manage.sh deploy/production/compose.yaml deploy/production/nginx.conf.template deploy/android-download/manage.sh; do
    cmp -s "$contract_directory/$file" "$release/$file" || fail "existing contract revision differs: $file"
  done
else
  snapshot=$(mktemp -d "$control_root/releases/.${contract_revision}.XXXXXX")
  install -d -m 0555 "$snapshot/deploy/production" "$snapshot/deploy/android-download"
  install -m 0555 "$contract_directory/deploy/production/manage.sh" "$snapshot/deploy/production/manage.sh"
  install -m 0444 "$contract_directory/deploy/production/compose.yaml" "$snapshot/deploy/production/compose.yaml"
  install -m 0444 "$contract_directory/deploy/production/nginx.conf.template" "$snapshot/deploy/production/nginx.conf.template"
  install -m 0555 "$contract_directory/deploy/android-download/manage.sh" "$snapshot/deploy/android-download/manage.sh"
  chown -R root:root "$snapshot"
  chmod 0555 "$snapshot" "$snapshot/deploy"
  mv -- "$snapshot" "$release"
fi

current_temporary=$(mktemp "$control_root/.current.XXXXXX")
rm -f "$current_temporary"
ln -s "releases/$contract_revision" "$current_temporary"
mv -Tf "$current_temporary" "$control_root/current"

visudo -cf "$sudoers_file" >/dev/null
sshd -t
systemd-tmpfiles --create "$tmpfiles_file"
systemctl reload ssh
printf 'Production forced-command host boundary installed at revision %s.\n' "$contract_revision"
