#!/usr/bin/env bash

set -euo pipefail

readonly ci_user="speakup-staging-ci"
readonly runtime_user="speakup-staging-runtime"
readonly ci_home="/var/empty/speakup-staging-ci"
readonly runtime_home="/var/lib/speakup/staging-runtime"
readonly broker_path="/usr/local/libexec/speakup-staging-broker"
readonly gate_path="/usr/local/libexec/speakup-staging-ssh-gate"
readonly control_root="/opt/xe3-speakup-staging-control"
readonly runtime_environment="/etc/speakup/staging-runtime.env"
readonly broker_state="/var/lib/speakup/staging-broker"
readonly lock_directory="/run/lock/xe3-speakup-staging"
readonly sshd_drop_in="/etc/ssh/sshd_config.d/60-speakup-staging-ci.conf"
readonly authorized_keys_file="/etc/ssh/authorized_keys/speakup-staging-ci"
readonly sudoers_file="/etc/sudoers.d/speakup-staging-ci"
readonly tmpfiles_file="/etc/tmpfiles.d/xe3-speakup-staging.conf"
readonly rootless_unit_name="speakup-staging-rootless-docker.service"
readonly rootless_unit="$runtime_home/.config/systemd/user/$rootless_unit_name"
readonly rootless_wants_link="$runtime_home/.config/systemd/user/default.target.wants/$rootless_unit_name"
readonly rootless_daemon_config="$runtime_home/.config/docker/daemon.json"
readonly registry_config="$runtime_home/.docker/config.json"
readonly trusted_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

test_mode=0
host_root=""
expected_root_uid=0
expected_root_gid=0
if [[ "${SPEAKUP_STAGING_HOST_TESTING:-0}" == 1 ]]; then
  (( EUID != 0 )) || {
    printf '%s\n' 'staging host validation: test mode is forbidden for root' >&2
    exit 1
  }
  test_mode=1
  host_root=${SPEAKUP_STAGING_HOST_ROOT:-}
  [[ "$host_root" == /* && -d "$host_root" ]] || {
    printf '%s\n' \
      'staging host validation: test root must be an existing absolute directory' >&2
    exit 1
  }
  expected_root_uid=$EUID
  PATH=${SPEAKUP_STAGING_HOST_TEST_PATH:-$PATH}
else
  (( EUID == 0 )) || {
    printf '%s\n' 'staging host validation: run as root' >&2
    exit 1
  }
  [[ -z "${SPEAKUP_STAGING_HOST_ROOT:-}" ]] || {
    printf '%s\n' 'staging host validation: host root override is test-only' >&2
    exit 1
  }
  PATH=$trusted_path
fi
export PATH

(( $# == 0 )) || {
  printf '%s\n' 'Usage: validate.sh' >&2
  exit 1
}

fail() {
  printf 'staging host validation: %s\n' "$*" >&2
  exit 1
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
  expected_root_gid=$(path_group "$host_root") ||
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
  local description=$1
  local value=$2
  [[ "$value" =~ ^/[A-Za-z0-9._/+@=-]+$ ]] &&
    [[ "$value" != *//* ]] &&
    [[ "$value" != */../* ]] &&
    [[ "$value" != */./* ]] &&
    [[ "$value" != */.. ]] &&
    [[ "$value" != */. ]] ||
    fail "$description must be a safe absolute path"
}

require_directory() {
  local description=$1
  local logical_path=$2
  local expected_uid=$3
  local expected_gid=$4
  local expected_mode=$5
  local path mode owner group
  path=$(host_path "$logical_path")
  [[ ! -L "$path" && -d "$path" ]] || fail "$description must be a real directory"
  mode=$(path_mode "$path") || fail "cannot inspect $description mode"
  owner=$(path_owner "$path") || fail "cannot inspect $description owner"
  group=$(path_group "$path") || fail "cannot inspect $description group"
  [[ "$mode" == "$expected_mode" && "$owner" == "$expected_uid" &&
     "$group" == "$expected_gid" ]] ||
    fail "$description has owner:group/mode $owner:$group/$mode; expected $expected_uid:$expected_gid/$expected_mode"
}

require_file() {
  local description=$1
  local logical_path=$2
  local expected_uid=$3
  local expected_gid=$4
  local expected_mode=$5
  local path mode owner group
  path=$(host_path "$logical_path")
  [[ ! -L "$path" && -f "$path" && -s "$path" ]] ||
    fail "$description must be a non-empty regular file"
  mode=$(path_mode "$path") || fail "cannot inspect $description mode"
  owner=$(path_owner "$path") || fail "cannot inspect $description owner"
  group=$(path_group "$path") || fail "cannot inspect $description group"
  [[ "$mode" == "$expected_mode" && "$owner" == "$expected_uid" &&
     "$group" == "$expected_gid" ]] ||
    fail "$description has owner:group/mode $owner:$group/$mode; expected $expected_uid:$expected_gid/$expected_mode"
}

require_private_runtime_file() {
  local description=$1
  local logical_path=$2
  local runtime_uid=$3
  local runtime_gid=$4
  local path mode owner group
  path=$(host_path "$logical_path")
  [[ ! -L "$path" && -f "$path" && -s "$path" ]] ||
    fail "$description must be a non-empty regular file"
  mode=$(path_mode "$path") || fail "cannot inspect $description mode"
  owner=$(path_owner "$path") || fail "cannot inspect $description owner"
  group=$(path_group "$path") || fail "cannot inspect $description group"
  case "$mode" in 400 | 600) ;; *) fail "$description must have mode 0400 or 0600" ;; esac
  [[ "$owner" == "$runtime_uid" && "$group" == "$runtime_gid" ]] ||
    fail "$description must be owned by $runtime_user"
}

require_safe_runtime_path() {
  local description=$1
  local logical_path=$2
  local runtime_uid=$3
  local component=""
  local relative=${logical_path#/}
  local old_ifs=$IFS
  local -a components
  IFS=/ read -r -a components <<<"$relative"
  IFS=$old_ifs
  for name in "${components[@]}"; do
    component="$component/$name"
    local path mode owner
    path=$(host_path "$component")
    [[ ! -L "$path" ]] || fail "$description has a symbolic-link path component"
    if [[ "$component" != "$logical_path" ]]; then
      [[ -d "$path" ]] || fail "$description ancestor is not a directory"
      mode=$(path_mode "$path") || fail "cannot inspect $description ancestor mode"
      owner=$(path_owner "$path") || fail "cannot inspect $description ancestor owner"
      [[ "$owner" == "$expected_root_uid" || "$owner" == "$runtime_uid" ]] ||
        fail "$description ancestor has an unexpected owner"
      (( (8#$mode & 0022) == 0 )) ||
        fail "$description ancestor is group- or world-writable"
    fi
  done
}

require_exact_user() {
  local name=$1
  local expected_home=$2
  local expected_shell=$3
  local entry group_entry entry_name password uid gid gecos home shell
  local group_name group_password group_gid group_memberships password_status
  local -a group_ids

  entry=$(getent passwd "$name") || fail "missing user: $name"
  IFS=: read -r entry_name password uid gid gecos home shell <<<"$entry"
  [[ "$entry_name" == "$name" && "$uid" =~ ^[0-9]+$ && "$uid" != 0 ]] ||
    fail "invalid account identity: $name"
  [[ "$home" == "$expected_home" ]] || fail "$name has the wrong home"
  [[ "$shell" == "$expected_shell" ]] || fail "$name has the wrong login shell"

  group_entry=$(getent group "$name") || fail "missing primary group: $name"
  IFS=: read -r group_name group_password group_gid group_memberships \
    <<<"$group_entry"
  [[ "$group_name" == "$name" && "$group_gid" == "$gid" ]] ||
    fail "$name has the wrong primary group"
  read -r -a group_ids <<<"$(id -G "$name")"
  (( ${#group_ids[@]} == 1 )) && [[ "${group_ids[0]}" == "$gid" ]] ||
    fail "$name must not have supplementary groups"

  password_status=$(passwd -S "$name") || fail "cannot inspect password status for $name"
  read -r _ password_state _ <<<"$password_status"
  [[ "$password_state" == L || "$password_state" == LK ]] ||
    fail "$name password must be locked"
  printf '%s %s\n' "$uid" "$gid"
}

require_subordinate_ids() {
  local file=$1
  local physical_file
  physical_file=$(host_path "$file")
  [[ -f "$physical_file" ]] || fail "$file does not exist"
  awk -F: -v user="$runtime_user" '
    $1 == user {
      entries++
      if (NF == 3 && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ && $3 >= 65536) {
        valid++
      }
    }
    END { exit !(entries == 1 && valid == 1) }
  ' "$physical_file" ||
    fail "$file must contain one range of at least 65536 IDs for $runtime_user"
}

require_rootless_prerequisites() {
  local logical_path physical_path mode owner
  for logical_path in \
    /usr/bin/docker \
    /usr/bin/dockerd-rootless.sh \
    /usr/bin/rootlesskit \
    /usr/bin/slirp4netns; do
    physical_path=$(host_path "$logical_path")
    [[ -f "$physical_path" && -x "$physical_path" ]] ||
      fail "rootless Docker prerequisite is missing: $logical_path"
  done
  for logical_path in /usr/bin/newuidmap /usr/bin/newgidmap; do
    physical_path=$(host_path "$logical_path")
    [[ -f "$physical_path" && -x "$physical_path" ]] ||
      fail "rootless Docker prerequisite is missing: $logical_path"
    mode=$(path_mode "$physical_path") || fail "cannot inspect $logical_path"
    owner=$(path_owner "$physical_path") || fail "cannot inspect $logical_path"
    if (( test_mode )); then
      [[ "$mode" == 755 && "$owner" == "$expected_root_uid" ]] ||
        fail "$logical_path must emulate a trusted test helper"
    else
      [[ "$mode" == 4755 && "$owner" == "$expected_root_uid" ]] ||
        fail "$logical_path must be root-owned mode 4755"
    fi
  done

  clone_file=$(host_path /proc/sys/kernel/unprivileged_userns_clone)
  max_file=$(host_path /proc/sys/user/max_user_namespaces)
  if [[ -e "$clone_file" ]]; then
    [[ "$(<"$clone_file")" == 1 ]] ||
      fail "kernel.unprivileged_userns_clone must be 1"
  fi
  [[ -f "$max_file" && "$(<"$max_file")" =~ ^[0-9]+$ ]] ||
    fail "user.max_user_namespaces is unavailable"
  (( $(<"$max_file") > 0 )) || fail "user.max_user_namespaces must be positive"
  require_subordinate_ids /etc/subuid
  require_subordinate_ids /etc/subgid
}

require_host_executable() {
  local logical_path=$1
  local physical_path
  physical_path=$(host_path "$logical_path")
  [[ -f "$physical_path" && -x "$physical_path" ]] ||
    fail "required host executable is missing: $logical_path"
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

assert_user_cannot_access() {
  local user=$1
  local description=$2
  local logical_path=$3
  local physical_path
  physical_path=$(host_path "$logical_path")
  if runuser -u "$user" -- test -r "$physical_path" 2>/dev/null ||
     runuser -u "$user" -- test -w "$physical_path" 2>/dev/null; then
    fail "$user can read or write $description"
  fi
}

assert_user_cannot_write() {
  local user=$1
  local description=$2
  local logical_path=$3
  local physical_path
  physical_path=$(host_path "$logical_path")
  if runuser -u "$user" -- test -w "$physical_path" 2>/dev/null; then
    fail "$user can write $description"
  fi
}

assert_user_can_read() {
  local user=$1
  local description=$2
  local logical_path=$3
  local physical_path
  physical_path=$(host_path "$logical_path")
  runuser -u "$user" -- test -r "$physical_path" ||
    fail "$user cannot read $description"
}

assert_user_can_write() {
  local user=$1
  local description=$2
  local logical_path=$3
  local physical_path
  physical_path=$(host_path "$logical_path")
  runuser -u "$user" -- test -w "$physical_path" ||
    fail "$user cannot write $description"
}

for command in \
  awk cmp docker env find getent id passwd readlink runuser sshd stat sudo \
  systemctl test tr visudo wc; do
  require_command "$command"
done

runtime_identity=$(
  require_exact_user "$runtime_user" "$runtime_home" /usr/sbin/nologin
) || exit 1
ci_identity=$(require_exact_user "$ci_user" "$ci_home" /bin/bash) || exit 1
read -r runtime_uid runtime_gid <<<"$runtime_identity"
read -r ci_uid ci_gid <<<"$ci_identity"
[[ "$runtime_uid" != "$ci_uid" ]] || fail "CI and runtime UIDs must differ"

require_directory "CI home" "$ci_home" "$expected_root_uid" "$expected_root_gid" 555
assert_user_cannot_write "$ci_user" "its home" "$ci_home"
require_directory "runtime home" "$runtime_home" "$expected_root_uid" "$runtime_gid" 710
require_directory "runtime Docker credential directory" "$runtime_home/.docker" \
  "$runtime_uid" "$runtime_gid" 700
require_directory "rootless Docker data directory" "$runtime_home/docker-data" \
  "$runtime_uid" "$runtime_gid" 700
require_directory "broker state" "$broker_state" "$runtime_uid" "$runtime_gid" 700
require_directory "broker manifest state" "$broker_state/manifests" \
  "$runtime_uid" "$runtime_gid" 700
require_directory "broker receipt state" "$broker_state/receipts" \
  "$runtime_uid" "$runtime_gid" 700
require_directory "deployment lock directory" "$lock_directory" \
  "$runtime_uid" "$runtime_gid" 700

require_file "broker executable" "$broker_path" \
  "$expected_root_uid" "$expected_root_gid" 555
require_file "SSH gate" "$gate_path" "$expected_root_uid" "$expected_root_gid" 555
cmp -s "$script_directory/ssh-gate" "$(host_path "$gate_path")" ||
  fail "installed SSH gate differs from the contract"
require_file "sshd drop-in" "$sshd_drop_in" \
  "$expected_root_uid" "$expected_root_gid" 644
cmp -s "$script_directory/sshd_config" "$(host_path "$sshd_drop_in")" ||
  fail "installed sshd drop-in differs from the contract"
require_file "sudoers policy" "$sudoers_file" \
  "$expected_root_uid" "$expected_root_gid" 440
cmp -s "$script_directory/sudoers" "$(host_path "$sudoers_file")" ||
  fail "installed sudoers policy differs from the contract"
visudo -cf "$(host_path "$sudoers_file")" >/dev/null || fail "sudoers policy is invalid"
require_file "tmpfiles policy" "$tmpfiles_file" \
  "$expected_root_uid" "$expected_root_gid" 444
cmp -s "$script_directory/tmpfiles.conf" "$(host_path "$tmpfiles_file")" ||
  fail "installed tmpfiles policy differs from the contract"

require_file "rootless Docker unit" "$rootless_unit" \
  "$expected_root_uid" "$runtime_gid" 440
cmp -s "$script_directory/rootless-docker.service" "$(host_path "$rootless_unit")" ||
  fail "installed rootless Docker unit differs from the contract"
rootless_wants_path=$(host_path "$rootless_wants_link")
[[ -L "$rootless_wants_path" ]] ||
  fail "rootless Docker enablement link is missing"
[[ "$(readlink "$rootless_wants_path")" == "../$rootless_unit_name" ]] ||
  fail "rootless Docker enablement link has an unexpected target"
rootless_wants_owner=$(path_owner "$rootless_wants_path") ||
  fail "cannot inspect rootless Docker enablement link owner"
[[ "$rootless_wants_owner" == "$expected_root_uid" ]] ||
  fail "rootless Docker enablement link must be root-owned"
require_file "rootless Docker daemon config" "$rootless_daemon_config" \
  "$expected_root_uid" "$runtime_gid" 440
cmp -s "$script_directory/daemon.json" "$(host_path "$rootless_daemon_config")" ||
  fail "installed rootless Docker daemon config differs from the contract"

authorized_keys_path=$(host_path "$authorized_keys_file")
require_file "CI authorized key" "$authorized_keys_file" \
  "$expected_root_uid" "$expected_root_gid" 600
authorized_key=""
authorized_key_line_count=0
while IFS= read -r key_line || [[ -n "$key_line" ]]; do
  authorized_key_line_count=$((authorized_key_line_count + 1))
  authorized_key=${key_line%$'\r'}
done <"$authorized_keys_path"
(( authorized_key_line_count == 1 )) || fail "CI authorized keys must contain one line"
[[ "$authorized_key" =~ ^restrict[[:space:]]ssh-ed25519[[:space:]][A-Za-z0-9+/]+={0,3}([[:space:]][^[:cntrl:]]+)?$ ]] ||
  fail "CI authorized key must be one restrict-prefixed Ed25519 key"

sshd -t || fail "the complete sshd configuration is invalid"
effective_sshd=$(sshd -T -C \
  user="$ci_user",host=localhost,addr=127.0.0.1) ||
  fail "cannot inspect effective CI SSH policy"
require_effective_sshd_value "$effective_sshd" authenticationmethods publickey
require_effective_sshd_value "$effective_sshd" pubkeyauthentication yes
require_effective_sshd_value "$effective_sshd" \
  authorizedkeysfile "$authorized_keys_file"
require_effective_sshd_value "$effective_sshd" passwordauthentication no
require_effective_sshd_value "$effective_sshd" kbdinteractiveauthentication no
require_effective_sshd_value "$effective_sshd" permittty no
require_effective_sshd_value "$effective_sshd" allowagentforwarding no
require_effective_sshd_value "$effective_sshd" allowtcpforwarding no
require_effective_sshd_value "$effective_sshd" allowstreamlocalforwarding no
require_effective_sshd_value "$effective_sshd" x11forwarding no
require_effective_sshd_value "$effective_sshd" permittunnel no
require_effective_sshd_value "$effective_sshd" permituserrc no
require_effective_sshd_value "$effective_sshd" permituserenvironment no
require_effective_sshd_value "$effective_sshd" permitopen none
require_effective_sshd_value "$effective_sshd" permitlisten none
require_effective_sshd_value "$effective_sshd" gatewayports no
require_effective_sshd_value "$effective_sshd" disableforwarding yes
require_effective_sshd_value "$effective_sshd" maxsessions 1
require_effective_sshd_value "$effective_sshd" forcecommand "$gate_path"

expected_sudo_rule="($runtime_user) NOPASSWD: $broker_path \"\""
sudo_rules=$(sudo -n -l -U "$ci_user" | awk '
  /^[[:space:]]*\(/ {
    sub(/^[[:space:]]*/, "")
    print
  }
') || fail "cannot inspect effective CI sudo policy"
[[ "$sudo_rules" == "$expected_sudo_rule" ]] ||
  fail "CI effective sudo policy is not the single no-argument broker command"
sudo -n -l -U "$ci_user" -u "$runtime_user" -- "$broker_path" >/dev/null ||
  fail "CI cannot invoke the no-argument broker command"
if sudo -n -l -U "$ci_user" -u "$runtime_user" -- "$broker_path" unexpected \
    >/dev/null 2>&1; then
  fail "CI sudo policy accepts broker arguments"
fi
if sudo -n -l -U "$ci_user" -u root -- /bin/sh >/dev/null 2>&1; then
  fail "CI sudo policy permits a root shell"
fi

current_path=$(host_path "$control_root/current")
[[ -L "$current_path" ]] || fail "control current must be a symbolic link"
current_target=$(readlink "$current_path") || fail "cannot inspect control current"
[[ "$current_target" =~ ^releases/[0-9a-f]{40}$ ]] ||
  fail "control current has an invalid revision target"
current_owner=$(path_owner "$current_path") || fail "cannot inspect control current owner"
[[ "$current_owner" == "$expected_root_uid" ]] || fail "control current must be root-owned"
release_revision=${current_target#releases/}
release_root="$control_root/releases/$release_revision"
require_directory "control release" "$release_root" \
  "$expected_root_uid" "$expected_root_gid" 555
require_directory "control deploy directory" "$release_root/deploy" \
  "$expected_root_uid" "$expected_root_gid" 555
require_directory "Staging control directory" "$release_root/deploy/staging" \
  "$expected_root_uid" "$expected_root_gid" 555
require_file "installed manage contract" "$release_root/deploy/staging/manage.sh" \
  "$expected_root_uid" "$expected_root_gid" 555
require_file "installed Compose contract" "$release_root/deploy/staging/compose.yaml" \
  "$expected_root_uid" "$expected_root_gid" 444

require_private_runtime_file "runtime environment" "$runtime_environment" \
  "$runtime_uid" "$runtime_gid"
require_safe_runtime_path "runtime environment" "$runtime_environment" "$runtime_uid"
server_environment=""
server_environment_count=0
while IFS= read -r line || [[ -n "$line" ]]; do
  line=${line%$'\r'}
  case "$line" in
    STAGING_SERVER_ENV_FILE=*)
      server_environment_count=$((server_environment_count + 1))
      server_environment=${line#*=}
      ;;
  esac
done <"$(host_path "$runtime_environment")"
(( server_environment_count == 1 )) ||
  fail "runtime environment must contain exactly one STAGING_SERVER_ENV_FILE"
require_absolute_path "STAGING_SERVER_ENV_FILE" "$server_environment"
require_private_runtime_file "Server environment" "$server_environment" \
  "$runtime_uid" "$runtime_gid"
require_safe_runtime_path "Server environment" "$server_environment" "$runtime_uid"
require_private_runtime_file "runtime registry config" "$registry_config" \
  "$runtime_uid" "$runtime_gid"
require_safe_runtime_path "runtime registry config" "$registry_config" "$runtime_uid"

for private_path in \
  "$runtime_environment" "$server_environment" "$registry_config" \
  "$broker_state" "$runtime_home/docker-data"; do
  assert_user_cannot_access "$ci_user" "runtime path $private_path" "$private_path"
done
assert_user_can_read "$runtime_user" "runtime environment" "$runtime_environment"
assert_user_can_read "$runtime_user" "Server environment" "$server_environment"
assert_user_can_read "$runtime_user" "runtime registry config" "$registry_config"
assert_user_can_write "$runtime_user" "broker state" "$broker_state"
assert_user_can_write "$runtime_user" "deployment lock directory" "$lock_directory"

for state_kind in manifests receipts; do
  state_directory=$(host_path "$broker_state/$state_kind")
  while IFS= read -r -d '' state_file; do
    state_name=${state_file##*/}
    [[ "$state_name" =~ ^[0-9a-f]{64}\.json$ ]] ||
      fail "$state_kind contains an unexpected object"
    require_file "$state_kind object" "$broker_state/$state_kind/$state_name" \
      "$runtime_uid" "$runtime_gid" 444
  done < <(find "$state_directory" -mindepth 1 -maxdepth 1 -print0)
done
state_current=$(host_path "$broker_state/current")
if [[ -e "$state_current" || -L "$state_current" ]]; then
  require_file "broker current pointer" "$broker_state/current" \
    "$runtime_uid" "$runtime_gid" 600
  [[ "$(wc -c <"$state_current" | tr -d '[:space:]')" == 64 ]] ||
    fail "broker current pointer must contain exactly 64 bytes"
  current_digest=$(<"$state_current")
  [[ "$current_digest" =~ ^[0-9a-f]{64}$ ]] ||
    fail "broker current pointer is invalid"
fi
lock_path=$(host_path "$lock_directory")
while IFS= read -r -d '' lock_file; do
  lock_name=${lock_file##*/}
  case "$lock_name" in broker.lock | deploy.lock) ;; *) fail "unexpected Staging lock file" ;; esac
  require_file "Staging lock" "$lock_directory/$lock_name" \
    "$runtime_uid" "$runtime_gid" 600
done < <(find "$lock_path" -mindepth 1 -maxdepth 1 -print0)

require_rootless_prerequisites
require_host_executable /bin/bash
require_host_executable /usr/sbin/nologin
runtime_directory="/run/user/$runtime_uid"
require_directory "runtime user directory" "$runtime_directory" \
  "$runtime_uid" "$runtime_gid" 700
linger_file="/var/lib/systemd/linger/$runtime_user"
linger_path=$(host_path "$linger_file")
[[ ! -L "$linger_path" && -f "$linger_path" ]] ||
  fail "runtime user lingering is not enabled"
rootless_socket="$runtime_directory/docker.sock"
rootless_socket_path=$(host_path "$rootless_socket")
is_socket "$rootless_socket_path" || fail "rootless Docker socket is unavailable"
socket_mode=$(path_mode "$rootless_socket_path") || fail "cannot inspect rootless socket mode"
socket_owner=$(path_owner "$rootless_socket_path") || fail "cannot inspect rootless socket owner"
socket_group=$(path_group "$rootless_socket_path") || fail "cannot inspect rootless socket group"
[[ "$socket_mode" == 600 && "$socket_owner" == "$runtime_uid" &&
   "$socket_group" == "$runtime_gid" ]] ||
  fail "rootless Docker socket has the wrong owner, group, or mode"
assert_user_cannot_access "$ci_user" "rootless Docker socket" "$rootless_socket"

runtime_systemctl=(
  runuser -u "$runtime_user" -- env -i
  "HOME=$runtime_home"
  "XDG_RUNTIME_DIR=/run/user/$runtime_uid"
  "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$runtime_uid/bus"
  "PATH=$trusted_path"
  systemctl --user
)
"${runtime_systemctl[@]}" is-enabled --quiet "$rootless_unit_name" ||
  fail "rootless Docker user service is not enabled"
"${runtime_systemctl[@]}" is-active --quiet "$rootless_unit_name" ||
  fail "rootless Docker user service is not active"

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

rootful_socket=$(host_path /var/run/docker.sock)
if [[ -e "$rootful_socket" ]] || is_socket "$rootful_socket"; then
  for user in "$ci_user" "$runtime_user"; do
    if runuser -u "$user" -- test -r "$rootful_socket" 2>/dev/null ||
       runuser -u "$user" -- test -w "$rootful_socket" 2>/dev/null; then
      fail "$user can access the rootful Docker socket"
    fi
  done
fi

printf '%s\n' \
  'Staging host access contract is valid.' \
  'CI has only the forced no-argument broker path; runtime inputs and rootless Docker remain isolated.'
