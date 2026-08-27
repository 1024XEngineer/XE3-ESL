#!/usr/bin/env bash

set -euo pipefail

readonly ci_user="speakup-staging-ci"
readonly runtime_user="speakup-staging-runtime"
readonly ci_home="/var/empty/speakup-staging-ci"
readonly runtime_home="/var/lib/speakup/staging-runtime"
readonly broker_path="/usr/local/libexec/speakup-staging-broker"
readonly gate_path="/usr/local/libexec/speakup-staging-ssh-gate"
readonly control_root="/opt/xe3-speakup-staging-control"
readonly broker_state="/var/lib/speakup/staging-broker"
readonly candidate_public_root="/var/lib/speakup/staging-apk-public"
readonly lock_directory="/run/lock/xe3-speakup-staging"
readonly sshd_drop_in="/etc/ssh/sshd_config.d/60-speakup-staging-ci.conf"
readonly authorized_keys_file="/etc/ssh/authorized_keys/speakup-staging-ci"
readonly sudoers_file="/etc/sudoers.d/speakup-staging-ci"
readonly tmpfiles_file="/etc/tmpfiles.d/xe3-speakup-staging.conf"
readonly rootless_unit_name="speakup-staging-rootless-docker.service"
readonly rootless_unit="$runtime_home/.config/systemd/user/$rootless_unit_name"
readonly rootless_wants_directory="$runtime_home/.config/systemd/user/default.target.wants"
readonly rootless_wants_link="$rootless_wants_directory/$rootless_unit_name"
readonly rootless_daemon_config="$runtime_home/.config/docker/daemon.json"
readonly trusted_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

test_mode=0
host_root=""
test_owner_gid=""
if [[ "${SPEAKUP_STAGING_HOST_TESTING:-0}" == 1 ]]; then
  (( EUID != 0 )) || {
    printf '%s\n' 'staging host bootstrap: test mode is forbidden for root' >&2
    exit 1
  }
  test_mode=1
  host_root=${SPEAKUP_STAGING_HOST_ROOT:-}
  [[ "$host_root" == /* && -d "$host_root" ]] || {
    printf '%s\n' \
      'staging host bootstrap: test root must be an existing absolute directory' >&2
    exit 1
  }
  PATH=${SPEAKUP_STAGING_HOST_TEST_PATH:-$PATH}
else
  (( EUID == 0 )) || {
    printf '%s\n' 'staging host bootstrap: run as root' >&2
    exit 1
  }
  [[ -z "${SPEAKUP_STAGING_HOST_ROOT:-}" ]] || {
    printf '%s\n' 'staging host bootstrap: host root override is test-only' >&2
    exit 1
  }
  PATH=$trusted_path
fi
export PATH

fail() {
  printf 'staging host bootstrap: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
Usage: bootstrap.sh \
  --broker-binary ABS \
  --contract-directory ABS \
  --contract-revision 40HEX \
  --ssh-public-key-file ABS
EOF
}

host_path() {
  local logical_path=$1
  printf '%s%s\n' "$host_root" "$logical_path"
}

path_mode() {
  local path=$1
  stat -c '%a' -- "$path" 2>/dev/null || stat -f '%Lp' "$path"
}

path_owner() {
  local path=$1
  stat -c '%u' -- "$path" 2>/dev/null || stat -f '%u' "$path"
}

path_group() {
  local path=$1
  stat -c '%g' -- "$path" 2>/dev/null || stat -f '%g' "$path"
}

if (( test_mode )); then
  test_owner_gid=$(path_group "$host_root") ||
    fail "cannot inspect test root group"
fi

is_socket() {
  local path=$1
  if (( test_mode )); then
    [[ -f "$path" ]]
  else
    [[ -S "$path" ]]
  fi
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_absolute_path() {
  local name=$1
  local value=$2
  [[ "$value" =~ ^/[A-Za-z0-9._/+@=-]+$ ]] &&
    [[ "$value" != *//* ]] &&
    [[ "$value" != */../* ]] &&
    [[ "$value" != */./* ]] &&
    [[ "$value" != */.. ]] &&
    [[ "$value" != */. ]] ||
    fail "$name must be a safe absolute path"
}

require_source_file() {
  local description=$1
  local path=$2
  [[ ! -L "$path" && -f "$path" && -s "$path" ]] ||
    fail "$description must be a non-empty regular file"
}

require_shared_tmp() {
  local path mode owner group expected_uid expected_gid
  path=$(host_path /tmp)
  [[ ! -L "$path" && -d "$path" ]] || fail "/tmp must be a real directory"
  mode=$(path_mode "$path") || fail "cannot inspect /tmp mode"
  owner=$(path_owner "$path") || fail "cannot inspect /tmp owner"
  group=$(path_group "$path") || fail "cannot inspect /tmp group"
  if (( test_mode )); then
    expected_uid=$(path_owner "$host_root") || fail "cannot inspect test root owner"
    expected_gid=$test_owner_gid
    [[ "$mode" == 777 || "$mode" == 1777 ]] ||
      fail "/tmp must have mode 1777"
    [[ -k "$path" ]] || fail "/tmp must have the sticky bit"
  else
    expected_uid=0
    expected_gid=0
    [[ "$mode" == 1777 ]] || fail "/tmp must have mode 1777"
  fi
  [[ "$owner" == "$expected_uid" && "$group" == "$expected_gid" ]] ||
    fail "/tmp must be owned by root:root with mode 1777"
}

require_linux_amd64_elf() {
  local path=$1 header
  header=$(od -An -tx1 -N20 -- "$path" | tr -d '[:space:]')
  [[ "$header" =~ ^7f454c46020101[0-9a-f]{18}(0200|0300)3e00$ ]] ||
    fail "broker binary must be a Linux amd64 ELF executable"
}

install_directory() {
  local logical_path=$1
  local owner=$2
  local group=$3
  local mode=$4
  local physical_path
  physical_path=$(host_path "$logical_path")
  if (( test_mode )); then
    install -d -m "$mode" -- "$physical_path"
    chgrp "$test_owner_gid" "$physical_path"
  else
    install -d -o "$owner" -g "$group" -m "$mode" -- "$physical_path"
  fi
}

install_file() {
  local source=$1
  local logical_target=$2
  local owner=$3
  local group=$4
  local mode=$5
  local physical_target
  physical_target=$(host_path "$logical_target")
  if (( test_mode )); then
    install -m "$mode" -- "$source" "$physical_target"
    chgrp "$test_owner_gid" "$physical_target"
  else
    install -o "$owner" -g "$group" -m "$mode" -- \
      "$source" "$physical_target"
  fi
}

require_exact_user() {
  local name=$1
  local primary_group=$2
  local expected_home=$3
  local expected_shell=$4
  local entry group_entry entry_name password uid gid gecos home shell
  local group_name group_password group_gid group_memberships

  entry=$(getent passwd "$name") || fail "missing user after creation: $name"
  IFS=: read -r entry_name password uid gid gecos home shell <<<"$entry"
  [[ "$entry_name" == "$name" && "$uid" =~ ^[0-9]+$ && "$uid" != 0 ]] ||
    fail "invalid account identity: $name"
  [[ "$home" == "$expected_home" ]] || fail "$name has the wrong home"
  [[ "$shell" == "$expected_shell" ]] || fail "$name has the wrong login shell"

  group_entry=$(getent group "$primary_group") ||
    fail "missing primary group: $primary_group"
  IFS=: read -r group_name group_password group_gid group_memberships \
    <<<"$group_entry"
  [[ "$group_name" == "$primary_group" && "$group_gid" == "$gid" ]] ||
    fail "$name has the wrong primary group"

  local -a group_ids
  read -r -a group_ids <<<"$(id -G "$name")"
  (( ${#group_ids[@]} == 1 )) && [[ "${group_ids[0]}" == "$gid" ]] ||
    fail "$name must not have supplementary groups"
}

ensure_identity() {
  local name=$1
  local home=$2
  local shell=$3
  local description=$4

  if ! getent group "$name" >/dev/null; then
    groupadd "$name"
  fi
  if ! getent passwd "$name" >/dev/null; then
    useradd \
      --gid "$name" \
      --home-dir "$home" \
      --no-create-home \
      --shell "$shell" \
      --comment "$description" \
      --password '!' \
      "$name"
  fi
  require_exact_user "$name" "$name" "$home" "$shell"
}

require_password_state() {
  local name=$1 expected=$2 status state
  status=$(passwd -S "$name") || fail "cannot inspect password status for $name"
  read -r _ state _ <<<"$status"
  case "$expected" in
    locked)
      [[ "$state" == L || "$state" == LK ]] ||
        fail "$name password must be locked"
      ;;
    usable)
      [[ "$state" == P ]] || fail "$name account must be usable by OpenSSH"
      ;;
  esac
}

ensure_ci_account_usable() {
  local status state generated_password
  status=$(passwd -S "$ci_user") ||
    fail "cannot inspect password status for $ci_user"
  read -r _ state _ <<<"$status"
  if [[ "$state" != P ]]; then
    generated_password=$(openssl rand -hex 48) ||
      fail "cannot generate the CI account password hash input"
    printf '%s:%s\n' "$ci_user" "$generated_password" | chpasswd
    generated_password=""
  fi
  require_password_state "$ci_user" usable
}

require_subordinate_ids() {
  local file=$1
  local label=$2
  local physical_file
  physical_file=$(host_path "$file")
  [[ -f "$physical_file" ]] || fail "$label does not exist"
  awk -F: -v user="$runtime_user" '
    $1 == user {
      entries++
      if (NF == 3 && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ && $3 >= 65536) {
        valid++
      }
    }
    END { exit !(entries == 1 && valid == 1) }
  ' "$physical_file" ||
    fail "$label must contain one range of at least 65536 IDs for $runtime_user"
}

require_rootless_binary() {
  local logical_path=$1
  local physical_path
  physical_path=$(host_path "$logical_path")
  [[ -f "$physical_path" && -x "$physical_path" ]] ||
    fail "rootless Docker prerequisite is missing: $logical_path"
}

require_host_executable() {
  local logical_path=$1
  local physical_path
  physical_path=$(host_path "$logical_path")
  [[ -f "$physical_path" && -x "$physical_path" ]] ||
    fail "required host executable is missing: $logical_path"
}

require_id_map_helper() {
  local logical_path=$1
  local physical_path mode owner
  physical_path=$(host_path "$logical_path")
  require_rootless_binary "$logical_path"
  mode=$(path_mode "$physical_path") || fail "cannot inspect $logical_path"
  owner=$(path_owner "$physical_path") || fail "cannot inspect $logical_path"
  if (( test_mode )); then
    [[ "$mode" == 755 && "$owner" == "$EUID" ]] ||
      fail "$logical_path must emulate a trusted test helper"
  else
    [[ "$mode" == 4755 && "$owner" == 0 ]] ||
      fail "$logical_path must be root-owned mode 4755"
  fi
}

require_user_namespace_support() {
  local clone_file max_file
  clone_file=$(host_path /proc/sys/kernel/unprivileged_userns_clone)
  max_file=$(host_path /proc/sys/user/max_user_namespaces)
  if [[ -e "$clone_file" ]]; then
    [[ "$(<"$clone_file")" == 1 ]] ||
      fail "kernel.unprivileged_userns_clone must be 1"
  fi
  [[ -f "$max_file" && "$(<"$max_file")" =~ ^[0-9]+$ ]] ||
    fail "user.max_user_namespaces is unavailable"
  (( $(<"$max_file") > 0 )) || fail "user.max_user_namespaces must be positive"
}

require_exact_mode_owner() {
  local description=$1
  local logical_path=$2
  local expected_mode=$3
  local expected_uid=$4
  local physical_path mode owner
  physical_path=$(host_path "$logical_path")
  [[ ! -L "$physical_path" ]] || fail "$description must not be a symlink"
  mode=$(path_mode "$physical_path") || fail "cannot inspect $description mode"
  owner=$(path_owner "$physical_path") || fail "cannot inspect $description owner"
  [[ "$mode" == "$expected_mode" && "$owner" == "$expected_uid" ]] ||
    fail "$description has the wrong owner or mode"
}

require_rootless_engine() {
  local runtime_uid=$1
  local socket_logical="/run/user/$runtime_uid/docker.sock"
  local socket_path security_options docker_root
  socket_path=$(host_path "$socket_logical")
  is_socket "$socket_path" || fail "rootless Docker socket is unavailable"
  require_exact_mode_owner "rootless Docker socket" "$socket_logical" 600 "$runtime_uid"

  security_options=$(runuser -u "$runtime_user" -- env -i \
    HOME="$runtime_home" \
    XDG_RUNTIME_DIR="/run/user/$runtime_uid" \
    DOCKER_HOST="unix:///run/user/$runtime_uid/docker.sock" \
    PATH="$trusted_path" \
    docker info --format '{{json .SecurityOptions}}') ||
    fail "the fixed rootless Docker socket did not answer"
  [[ "$security_options" == *'"name=rootless"'* ]] ||
    fail "the fixed Docker daemon is not rootless"

  docker_root=$(runuser -u "$runtime_user" -- env -i \
    HOME="$runtime_home" \
    XDG_RUNTIME_DIR="/run/user/$runtime_uid" \
    DOCKER_HOST="unix:///run/user/$runtime_uid/docker.sock" \
    PATH="$trusted_path" \
    docker info --format '{{.DockerRootDir}}') ||
    fail "cannot inspect the rootless Docker data root"
  [[ "$docker_root" == "$runtime_home/docker-data" ]] ||
    fail "rootless Docker uses an unexpected data root"
}

require_effective_sshd_value() {
  local output=$1
  local key=$2
  local expected=$3
  local actual
  actual=$(awk -v key="$key" '$1 == key { $1=""; sub(/^ /, ""); print; exit }' \
    <<<"$output")
  [[ "$actual" == "$expected" ]] ||
    fail "effective sshd setting $key is not $expected"
}

broker_binary=""
contract_directory=""
contract_revision=""
ssh_public_key_file=""
while (( $# > 0 )); do
  case "$1" in
    --broker-binary | --contract-directory | --contract-revision | \
      --ssh-public-key-file)
      flag=$1
      (( $# >= 2 )) || {
        usage
        fail "$flag requires one value"
      }
      [[ -n "$2" ]] || fail "$flag must not be empty"
      case "$flag" in
        --broker-binary)
          [[ -z "$broker_binary" ]] || fail "$flag may be specified only once"
          broker_binary=$2
          ;;
        --contract-directory)
          [[ -z "$contract_directory" ]] || fail "$flag may be specified only once"
          contract_directory=$2
          ;;
        --contract-revision)
          [[ -z "$contract_revision" ]] || fail "$flag may be specified only once"
          contract_revision=$2
          ;;
        --ssh-public-key-file)
          [[ -z "$ssh_public_key_file" ]] || fail "$flag may be specified only once"
          ssh_public_key_file=$2
          ;;
      esac
      shift 2
      ;;
    *)
      usage
      fail "unsupported argument: $1"
      ;;
  esac
done

[[ -n "$broker_binary" && -n "$contract_directory" &&
   -n "$contract_revision" && -n "$ssh_public_key_file" ]] || {
  usage
  fail "all four arguments are required"
}
require_absolute_path --broker-binary "$broker_binary"
require_absolute_path --contract-directory "$contract_directory"
require_absolute_path --ssh-public-key-file "$ssh_public_key_file"
[[ "$contract_revision" =~ ^[0-9a-f]{40}$ ]] ||
  fail "--contract-revision must be exactly 40 lowercase hexadecimal characters"

[[ ! -L "$contract_directory" && -d "$contract_directory" ]] ||
  fail "contract directory must be a real directory"
require_source_file "broker binary" "$broker_binary"
[[ -x "$broker_binary" ]] || fail "broker binary must be executable"
require_source_file "SSH public key" "$ssh_public_key_file"
require_source_file "manage contract" "$contract_directory/manage.sh"
[[ -x "$contract_directory/manage.sh" ]] || fail "manage contract must be executable"
require_source_file "Compose contract" "$contract_directory/compose.yaml"

public_key=""
public_key_line_count=0
while IFS= read -r key_line || [[ -n "$key_line" ]]; do
  public_key_line_count=$((public_key_line_count + 1))
  public_key=${key_line%$'\r'}
done <"$ssh_public_key_file"
(( public_key_line_count == 1 )) || fail "SSH public key file must contain one line"
[[ "$public_key" =~ ^ssh-ed25519[[:space:]][A-Za-z0-9+/]+={0,3}([[:space:]][^[:cntrl:]]+)?$ ]] ||
  fail "SSH public key must contain one bare Ed25519 public key"

for command in \
  awk chgrp chmod chown chpasswd cmp env getent groupadd id install ln loginctl \
  mktemp mv od openssl passwd rm runuser sleep sshd stat systemctl \
  systemd-tmpfiles tr useradd \
  visudo; do
  require_command "$command"
done
require_linux_amd64_elf "$broker_binary"

for rootless_binary in \
  /usr/bin/docker \
  /usr/bin/dockerd-rootless.sh \
  /usr/bin/rootlesskit \
  /usr/bin/slirp4netns; do
  require_rootless_binary "$rootless_binary"
done
require_host_executable /bin/bash
require_host_executable /usr/sbin/nologin
require_id_map_helper /usr/bin/newuidmap
require_id_map_helper /usr/bin/newgidmap
require_user_namespace_support
require_shared_tmp

ensure_identity "$runtime_user" "$runtime_home" /usr/sbin/nologin \
  "SpeakUp Staging runtime"
ensure_identity "$ci_user" "$ci_home" /bin/bash \
  "SpeakUp Staging CI forced command"
require_password_state "$runtime_user" locked
ensure_ci_account_usable
runtime_uid=$(id -u "$runtime_user")
runtime_gid=$(id -g "$runtime_user")
[[ "$(id -u "$ci_user")" != "$runtime_uid" ]] ||
  fail "CI and runtime users must have distinct UIDs"
require_subordinate_ids /etc/subuid /etc/subuid
require_subordinate_ids /etc/subgid /etc/subgid

install_directory /var/empty root root 755
install_directory "$ci_home" root root 555
install_directory /var/lib/speakup root root 755
install_directory "$runtime_home" root "$runtime_user" 710
install_directory "$runtime_home/.config" root "$runtime_user" 750
install_directory "$runtime_home/.config/docker" root "$runtime_user" 750
install_directory "$runtime_home/.config/systemd" root "$runtime_user" 750
install_directory "$runtime_home/.config/systemd/user" root "$runtime_user" 750
install_directory "$rootless_wants_directory" root "$runtime_user" 750
install_directory "$runtime_home/.docker" "$runtime_user" "$runtime_user" 700
install_directory "$runtime_home/docker-data" "$runtime_user" "$runtime_user" 710
install_directory /var/lib/speakup/staging-broker "$runtime_user" "$runtime_user" 700
install_directory /var/lib/speakup/staging-broker/manifests "$runtime_user" "$runtime_user" 700
install_directory /var/lib/speakup/staging-broker/receipts "$runtime_user" "$runtime_user" 700
install_directory "$candidate_public_root" "$runtime_user" "$runtime_user" 755
install_directory "$candidate_public_root/downloads" "$runtime_user" "$runtime_user" 755
install_directory "$candidate_public_root/downloads/android" "$runtime_user" "$runtime_user" 755
install_directory "$candidate_public_root/downloads/android/candidates" "$runtime_user" "$runtime_user" 755
install_directory /etc/speakup root "$runtime_user" 710
install_directory /run/lock/xe3-speakup-staging "$runtime_user" "$runtime_user" 700
install_directory /usr/local/libexec root root 755
install_directory /etc/ssh/authorized_keys root root 755
install_directory /etc/ssh/sshd_config.d root root 755
install_directory /etc/sudoers.d root root 755
install_directory /etc/tmpfiles.d root root 755
install_directory "$control_root" root root 755
install_directory "$control_root/releases" root root 755

install_file "$script_directory/rootless-docker.service" \
  "$rootless_unit" root "$runtime_user" 440
rootless_wants_path=$(host_path "$rootless_wants_link")
if [[ -e "$rootless_wants_path" && ! -L "$rootless_wants_path" ]]; then
  fail "rootless Docker enablement path must be a symbolic link"
fi
ln -sfn "../$rootless_unit_name" "$rootless_wants_path"
if (( ! test_mode )); then
  chown -h root:root "$rootless_wants_path"
fi
install_file "$script_directory/daemon.json" \
  "$rootless_daemon_config" root "$runtime_user" 440
install_directory "$rootless_wants_directory" root "$runtime_user" 550
install_directory "$runtime_home/.config/docker" root "$runtime_user" 550
install_directory "$runtime_home/.config/systemd/user" root "$runtime_user" 550
install_directory "$runtime_home/.config/systemd" root "$runtime_user" 550
install_directory "$runtime_home/.config" root "$runtime_user" 550
install_file "$script_directory/tmpfiles.conf" "$tmpfiles_file" root root 444
install_file "$script_directory/ssh-gate" "$gate_path" root root 555
install_file "$broker_binary" "$broker_path" root root 555
install_file "$script_directory/sshd_config" "$sshd_drop_in" root root 644
visudo -cf "$script_directory/sudoers" >/dev/null || fail "invalid sudoers contract"
install_file "$script_directory/sudoers" "$sudoers_file" root root 440

authorized_keys_temporary=$(mktemp)
current_link_temporary=""
snapshot_temporary=""
cleanup() {
  rm -f -- "$authorized_keys_temporary"
  if [[ -n "$current_link_temporary" ]]; then
    rm -f -- "$current_link_temporary"
  fi
  if [[ -n "$snapshot_temporary" && -d "$snapshot_temporary" ]]; then
    rm -rf -- "$snapshot_temporary"
  fi
}
trap cleanup EXIT
printf 'restrict %s\n' "$public_key" >"$authorized_keys_temporary"
install_file "$authorized_keys_temporary" "$authorized_keys_file" root root 644

release_logical="$control_root/releases/$contract_revision"
release_path=$(host_path "$release_logical")
if [[ -e "$release_path" || -L "$release_path" ]]; then
  [[ ! -L "$release_path" && -d "$release_path" ]] ||
    fail "existing contract revision is not a real directory"
  cmp -s "$contract_directory/manage.sh" "$release_path/deploy/staging/manage.sh" &&
    cmp -s "$contract_directory/compose.yaml" "$release_path/deploy/staging/compose.yaml" ||
    fail "existing contract revision does not match the supplied snapshot"
else
  releases_path=$(host_path "$control_root/releases")
  snapshot_temporary=$(mktemp -d "$releases_path/.${contract_revision}.XXXXXX")
  install -d -m 755 -- "$snapshot_temporary/deploy/staging"
  install -m 555 -- "$contract_directory/manage.sh" \
    "$snapshot_temporary/deploy/staging/manage.sh"
  install -m 444 -- "$contract_directory/compose.yaml" \
    "$snapshot_temporary/deploy/staging/compose.yaml"
  if (( test_mode )); then
    chgrp -R "$test_owner_gid" "$snapshot_temporary"
  else
    chown -R root:root "$snapshot_temporary"
  fi
  chmod 555 "$snapshot_temporary" "$snapshot_temporary/deploy" \
    "$snapshot_temporary/deploy/staging"
  mv -- "$snapshot_temporary" "$release_path"
  snapshot_temporary=""
fi

current_path=$(host_path "$control_root/current")
if [[ -e "$current_path" && ! -L "$current_path" ]]; then
  fail "control current path must be a symbolic link"
fi
current_link_temporary=$(mktemp "$(host_path "$control_root")/.current.XXXXXX")
ln -sfn "releases/$contract_revision" "$current_link_temporary"
mv -Tf -- "$current_link_temporary" "$current_path"
current_link_temporary=""

systemd-tmpfiles --create "$(host_path "$tmpfiles_file")"
loginctl enable-linger "$runtime_user"

runtime_directory=$(host_path "/run/user/$runtime_uid")
for _ in {1..40}; do
  [[ -d "$runtime_directory" ]] && is_socket "$runtime_directory/bus" && break
  sleep 0.25
done
[[ -d "$runtime_directory" ]] && is_socket "$runtime_directory/bus" ||
  fail "runtime user manager did not create its private bus"
require_exact_mode_owner "runtime directory" "/run/user/$runtime_uid" 700 "$runtime_uid"

runtime_systemctl=(
  runuser -u "$runtime_user" -- env -i
  "HOME=$runtime_home"
  "XDG_RUNTIME_DIR=/run/user/$runtime_uid"
  "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$runtime_uid/bus"
  "PATH=$trusted_path"
  systemctl --user
)
"${runtime_systemctl[@]}" daemon-reload
"${runtime_systemctl[@]}" is-enabled --quiet "$rootless_unit_name" ||
  fail "rootless Docker user service is not enabled by the fixed host link"
"${runtime_systemctl[@]}" restart "$rootless_unit_name"

rootless_socket=$(host_path "/run/user/$runtime_uid/docker.sock")
for _ in {1..80}; do
  is_socket "$rootless_socket" && break
  sleep 0.25
done
require_rootless_engine "$runtime_uid"

sshd -t || fail "the complete sshd configuration is invalid"
effective_sshd=$(sshd -T -C \
  user="$ci_user",host=localhost,addr=127.0.0.1) ||
  fail "cannot inspect the effective CI SSH policy"
require_effective_sshd_value "$effective_sshd" forcecommand "$gate_path"
require_effective_sshd_value "$effective_sshd" passwordauthentication no
require_effective_sshd_value "$effective_sshd" kbdinteractiveauthentication no
require_effective_sshd_value "$effective_sshd" permittty no
require_effective_sshd_value "$effective_sshd" disableforwarding yes
systemctl reload ssh.service

printf '%s\n' \
  "Staging host access bootstrap installed revision $contract_revision." \
  "No runtime environment, Server environment, registry credential, or other Secret was created." \
  "Provision those runtime-owned inputs, then run deploy/staging/host/validate.sh as root."
