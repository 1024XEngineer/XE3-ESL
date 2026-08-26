#!/usr/bin/env bash

set -euo pipefail

staging_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
host_contract="$staging_directory/host"
temporary_directory="$(mktemp -d /private/tmp/xe3-staging-host.XXXXXX)"
fixture_root="$temporary_directory/root"
mock_bin="$temporary_directory/bin"

cleanup() {
  chmod -R u+w "$temporary_directory" >/dev/null 2>&1 || true
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

fail() {
  printf 'staging host access test: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local description=$1
  shift
  if "$@" >"$temporary_directory/failure.log" 2>&1; then
    fail "$description unexpectedly succeeded"
  fi
}

assert_line() {
  local expected=$1
  local file=$2
  grep -Fxq -- "$expected" "$file" ||
    fail "missing exact contract line in ${file##*/}: $expected"
}

for script in \
  "$host_contract/bootstrap.sh" \
  "$host_contract/validate.sh" \
  "$host_contract/ssh-gate"; do
  bash -n "$script"
done

[[ "$(<"$host_contract/sudoers")" == \
  'speakup-staging-ci ALL=(speakup-staging-runtime) NOPASSWD: /usr/local/libexec/speakup-staging-broker ""' ]] ||
  fail "sudoers must permit only the no-argument broker command"
assert_line 'Match User speakup-staging-ci' "$host_contract/sshd_config"
assert_line '    AuthenticationMethods publickey' "$host_contract/sshd_config"
assert_line '    PasswordAuthentication no' "$host_contract/sshd_config"
assert_line '    KbdInteractiveAuthentication no' "$host_contract/sshd_config"
assert_line '    PermitTTY no' "$host_contract/sshd_config"
assert_line '    AllowAgentForwarding no' "$host_contract/sshd_config"
assert_line '    AllowTcpForwarding no' "$host_contract/sshd_config"
assert_line '    AllowStreamLocalForwarding no' "$host_contract/sshd_config"
assert_line '    X11Forwarding no' "$host_contract/sshd_config"
assert_line '    PermitTunnel no' "$host_contract/sshd_config"
assert_line '    PermitUserRC no' "$host_contract/sshd_config"
assert_line '    DisableForwarding yes' "$host_contract/sshd_config"
assert_line '    ForceCommand /usr/local/libexec/speakup-staging-ssh-gate' \
  "$host_contract/sshd_config"
assert_line 'exec /usr/bin/sudo -n -u speakup-staging-runtime -- \' \
  "$host_contract/ssh-gate"
assert_line '  /usr/local/libexec/speakup-staging-broker' "$host_contract/ssh-gate"
assert_line 'ExecStart=/usr/bin/dockerd-rootless.sh --host=unix://%t/docker.sock --config-file=/var/lib/speakup/staging-runtime/.config/docker/daemon.json' \
  "$host_contract/rootless-docker.service"
if grep -Fq '/var/run/docker.sock' "$host_contract/rootless-docker.service"; then
  fail "rootless unit references the rootful Docker socket"
fi

expect_failure "SSH shell request" env SSH_ORIGINAL_COMMAND=/bin/sh \
  "$host_contract/ssh-gate"
expect_failure "SSH scp request" env SSH_ORIGINAL_COMMAND='scp -t /tmp/file' \
  "$host_contract/ssh-gate"
expect_failure "SSH sftp request" env SSH_ORIGINAL_COMMAND=internal-sftp \
  "$host_contract/ssh-gate"
expect_failure "SSH gate argument" "$host_contract/ssh-gate" unexpected

mkdir -p "$fixture_root" "$mock_bin"
fixture_uid=$(/usr/bin/id -u)
fixture_gid=$(stat -c '%g' "$fixture_root" 2>/dev/null || stat -f '%g' "$fixture_root")
ci_uid=$((fixture_uid + 1))
ci_gid=$((fixture_gid + 1))
export MOCK_FIXTURE_UID="$fixture_uid"
export MOCK_FIXTURE_GID="$fixture_gid"
export MOCK_CI_UID="$ci_uid"
export MOCK_CI_GID="$ci_gid"
export MOCK_HOST_ROOT="$fixture_root"

cat >"$mock_bin/getent" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1:$2" in
  passwd:speakup-staging-runtime)
    printf 'speakup-staging-runtime:x:%s:%s::/var/lib/speakup/staging-runtime:/usr/sbin/nologin\n' \
      "$MOCK_FIXTURE_UID" "$MOCK_FIXTURE_GID"
    ;;
  passwd:speakup-staging-ci)
    printf 'speakup-staging-ci:x:%s:%s::/var/empty/speakup-staging-ci:/bin/bash\n' \
      "$MOCK_CI_UID" "$MOCK_CI_GID"
    ;;
  group:speakup-staging-runtime)
    printf 'speakup-staging-runtime:x:%s:\n' "$MOCK_FIXTURE_GID"
    ;;
  group:speakup-staging-ci)
    printf 'speakup-staging-ci:x:%s:\n' "$MOCK_CI_GID"
    ;;
  *) exit 2 ;;
esac
EOF

cat >"$mock_bin/id" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}:${2:-}" in
  -u:speakup-staging-runtime) printf '%s\n' "$MOCK_FIXTURE_UID" ;;
  -g:speakup-staging-runtime) printf '%s\n' "$MOCK_FIXTURE_GID" ;;
  -G:speakup-staging-runtime) printf '%s\n' "$MOCK_FIXTURE_GID" ;;
  -u:speakup-staging-ci) printf '%s\n' "$MOCK_CI_UID" ;;
  -g:speakup-staging-ci) printf '%s\n' "$MOCK_CI_GID" ;;
  -G:speakup-staging-ci)
    if [[ "${MOCK_CI_EXTRA_GROUP:-0}" == 1 ]]; then
      printf '%s %s\n' "$MOCK_CI_GID" "$MOCK_FIXTURE_GID"
    else
      printf '%s\n' "$MOCK_CI_GID"
    fi
    ;;
  -g:) printf '%s\n' "$MOCK_FIXTURE_GID" ;;
  *) exit 2 ;;
esac
EOF

cat >"$mock_bin/passwd" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -S && -n "${2:-}" ]]
printf '%s L 2026-08-26 0 99999 7 -1\n' "$2"
EOF

cat >"$mock_bin/groupadd" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'groupadd must not run for existing fixture identities' >&2
exit 1
EOF

cat >"$mock_bin/useradd" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'useradd must not run for existing fixture identities' >&2
exit 1
EOF

cat >"$mock_bin/visudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -cf && -f "$2" ]]
grep -Fxq 'speakup-staging-ci ALL=(speakup-staging-runtime) NOPASSWD: /usr/local/libexec/speakup-staging-broker ""' "$2"
EOF

cat >"$mock_bin/sshd" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == -t ]]; then
  exit 0
fi
[[ "$1" == -T && "$2" == -C ]]
cat <<'OUTPUT'
authenticationmethods publickey
pubkeyauthentication yes
authorizedkeysfile /etc/ssh/authorized_keys/speakup-staging-ci
passwordauthentication no
kbdinteractiveauthentication no
permittty no
allowagentforwarding no
allowtcpforwarding no
allowstreamlocalforwarding no
x11forwarding no
permittunnel no
permituserrc no
permituserenvironment no
permitopen none
permitlisten none
gatewayports no
disableforwarding yes
maxsessions 1
forcecommand /usr/local/libexec/speakup-staging-ssh-gate
OUTPUT
EOF

cat >"$mock_bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *' unexpected '* | *' -u root '*) exit 1 ;;
esac
if [[ " $* " != *' -u speakup-staging-runtime '* ]]; then
  printf '%s\n' \
    'Matching Defaults entries for speakup-staging-ci on fixture:' \
    'User speakup-staging-ci may run the following commands on fixture:' \
    '    (speakup-staging-runtime) NOPASSWD: /usr/local/libexec/speakup-staging-broker ""'
fi
EOF

cat >"$mock_bin/runuser" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -u && -n "$2" && "$3" == -- ]]
user=$2
shift 3
if [[ "$1" == env ]]; then
  case " $* " in
    *' docker info --format {{json .SecurityOptions}}'*)
      printf '%s\n' '["name=rootless","name=cgroupns"]'
      ;;
    *' docker info --format {{.DockerRootDir}}'*)
      printf '%s\n' '/var/lib/speakup/staging-runtime/docker-data'
      ;;
    *' systemctl --user '*) exit 0 ;;
    *) exit 2 ;;
  esac
  exit 0
fi
if [[ "$1" == test ]]; then
  permission=$2
  path=$3
  if [[ "$user" == speakup-staging-ci ]]; then
    case "$path" in
      "$MOCK_HOST_ROOT/var/empty/speakup-staging-ci")
        [[ "$permission" == -r ]]
        ;;
      *) exit 1 ;;
    esac
  else
    exit 0
  fi
  exit
fi
exit 2
EOF

cat >"$mock_bin/loginctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == enable-linger && "$2" == speakup-staging-runtime ]]
EOF

cat >"$mock_bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == reload && "$2" == ssh.service ]]
EOF

cat >"$mock_bin/systemd-tmpfiles" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == --create && -f "$2" ]]
EOF

cat >"$mock_bin/mv" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == -Tf && "${2:-}" == -- && $# == 4 ]]; then
  source=$3
  destination=$4
  [[ ! -e "$destination" || -L "$destination" ]]
  /bin/rm -f -- "$destination"
  exec /bin/mv -f -- "$source" "$destination"
fi
exec /bin/mv "$@"
EOF

cat >"$mock_bin/docker" <<'EOF'
#!/usr/bin/env bash
exit 99
EOF

chmod 755 "$mock_bin"/*

for logical_directory in \
  /bin \
  /usr/bin \
  /usr/sbin \
  /proc/sys/kernel \
  /proc/sys/user \
  /etc \
  /var/lib/systemd/linger \
  "/run/user/$fixture_uid"; do
  mkdir -p "$fixture_root$logical_directory"
done
printf '#!/usr/bin/env sh\nexit 0\n' >"$fixture_root/bin/bash"
printf '#!/usr/bin/env sh\nexit 1\n' >"$fixture_root/usr/sbin/nologin"
chmod 755 "$fixture_root/bin/bash" "$fixture_root/usr/sbin/nologin"
for binary in docker dockerd-rootless.sh rootlesskit slirp4netns newuidmap newgidmap; do
  printf '#!/usr/bin/env sh\nexit 0\n' >"$fixture_root/usr/bin/$binary"
  chmod 755 "$fixture_root/usr/bin/$binary"
done
printf '1\n' >"$fixture_root/proc/sys/kernel/unprivileged_userns_clone"
printf '28633\n' >"$fixture_root/proc/sys/user/max_user_namespaces"
printf 'speakup-staging-runtime:100000:65536\n' >"$fixture_root/etc/subuid"
printf 'speakup-staging-runtime:100000:65536\n' >"$fixture_root/etc/subgid"
: >"$fixture_root/var/lib/systemd/linger/speakup-staging-runtime"
chmod 700 "$fixture_root/run/user/$fixture_uid"

: >"$fixture_root/run/user/$fixture_uid/bus"
: >"$fixture_root/run/user/$fixture_uid/docker.sock"
chmod 600 "$fixture_root/run/user/$fixture_uid/bus" \
  "$fixture_root/run/user/$fixture_uid/docker.sock"
chgrp "$fixture_gid" "$fixture_root/run/user/$fixture_uid" \
  "$fixture_root/run/user/$fixture_uid/bus" \
  "$fixture_root/run/user/$fixture_uid/docker.sock"

broker_binary="$temporary_directory/speakup-staging-broker"
cat >"$broker_binary" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
chmod 755 "$broker_binary"
public_key_file="$temporary_directory/staging.pub"
printf '%s\n' \
  'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFixtureKeyOnlyForContractTesting staging-ci' \
  >"$public_key_file"
contract_revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

host_environment=(
  env
  SPEAKUP_STAGING_HOST_TESTING=1
  "SPEAKUP_STAGING_HOST_ROOT=$fixture_root"
  "SPEAKUP_STAGING_HOST_TEST_PATH=$mock_bin:/usr/bin:/bin:/usr/sbin:/sbin"
)
bootstrap_command=(
  "$host_contract/bootstrap.sh"
  --broker-binary "$broker_binary"
  --contract-directory "$staging_directory"
  --contract-revision "$contract_revision"
  --ssh-public-key-file "$public_key_file"
)

expect_failure "unknown bootstrap flag" \
  "${host_environment[@]}" "${bootstrap_command[@]}" --unknown value
expect_failure "duplicate bootstrap flag" \
  "${host_environment[@]}" "${bootstrap_command[@]}" \
  --contract-revision "$contract_revision"
expect_failure "relative bootstrap input" \
  "${host_environment[@]}" "$host_contract/bootstrap.sh" \
  --broker-binary relative \
  --contract-directory "$staging_directory" \
  --contract-revision "$contract_revision" \
  --ssh-public-key-file "$public_key_file"

mv "$fixture_root/usr/bin/slirp4netns" "$temporary_directory/slirp4netns"
expect_failure "missing rootless prerequisite" \
  "${host_environment[@]}" "${bootstrap_command[@]}"
mv "$temporary_directory/slirp4netns" "$fixture_root/usr/bin/slirp4netns"

"${host_environment[@]}" "${bootstrap_command[@]}" \
  >"$temporary_directory/bootstrap.log"

rootless_enablement_link="$fixture_root/var/lib/speakup/staging-runtime/.config/systemd/user/default.target.wants/speakup-staging-rootless-docker.service"
[[ -L "$rootless_enablement_link" ]] ||
  fail "bootstrap did not install the fixed rootless Docker enablement link"
[[ "$(readlink "$rootless_enablement_link")" == '../speakup-staging-rootless-docker.service' ]] ||
  fail "rootless Docker enablement link has an unexpected target"

"${host_environment[@]}" "${bootstrap_command[@]}" \
  >"$temporary_directory/bootstrap-repeat.log"

runtime_env="$fixture_root/etc/speakup/staging-runtime.env"
server_env="$fixture_root/etc/speakup/staging-server.env"
registry_config="$fixture_root/var/lib/speakup/staging-runtime/.docker/config.json"
cat >"$runtime_env" <<'EOF'
STAGING_POSTGRES_DB=speakup_staging
STAGING_POSTGRES_USER=speakup_staging
STAGING_POSTGRES_PASSWORD=fixture-password-with-24-chars
PORTAL_ADMIN_PASSWORD=fixture-portal-password
STAGING_SERVER_ENV_FILE=/etc/speakup/staging-server.env
EOF
printf '%s\n' 'APP_ENV=staging-fixture' >"$server_env"
printf '%s\n' '{"auths":{"ghcr.io":{"auth":"fixture"}}}' >"$registry_config"
chmod 600 "$runtime_env" "$server_env" "$registry_config"
chgrp "$fixture_gid" "$runtime_env" "$server_env" "$registry_config"

"${host_environment[@]}" "$host_contract/validate.sh" \
  >"$temporary_directory/validate.log"

cp "$fixture_root/etc/sudoers.d/speakup-staging-ci" \
  "$temporary_directory/sudoers.good"
chmod 600 "$fixture_root/etc/sudoers.d/speakup-staging-ci"
printf '%s\n' \
  'speakup-staging-ci ALL=(speakup-staging-runtime) NOPASSWD: /usr/local/libexec/speakup-staging-broker *' \
  >"$fixture_root/etc/sudoers.d/speakup-staging-ci"
chmod 440 "$fixture_root/etc/sudoers.d/speakup-staging-ci"
expect_failure "arbitrary broker arguments" \
  "${host_environment[@]}" "$host_contract/validate.sh"
chmod 600 "$fixture_root/etc/sudoers.d/speakup-staging-ci"
cp "$temporary_directory/sudoers.good" \
  "$fixture_root/etc/sudoers.d/speakup-staging-ci"
chmod 440 "$fixture_root/etc/sudoers.d/speakup-staging-ci"

cp "$fixture_root/etc/ssh/authorized_keys/speakup-staging-ci" \
  "$temporary_directory/authorized-key.good"
sed 's/^restrict //' "$temporary_directory/authorized-key.good" \
  >"$fixture_root/etc/ssh/authorized_keys/speakup-staging-ci"
chmod 600 "$fixture_root/etc/ssh/authorized_keys/speakup-staging-ci"
expect_failure "unrestricted SSH key" \
  "${host_environment[@]}" "$host_contract/validate.sh"
cp "$temporary_directory/authorized-key.good" \
  "$fixture_root/etc/ssh/authorized_keys/speakup-staging-ci"
chmod 600 "$fixture_root/etc/ssh/authorized_keys/speakup-staging-ci"

chmod 644 "$runtime_env"
expect_failure "world-readable runtime environment" \
  "${host_environment[@]}" "$host_contract/validate.sh"
chmod 600 "$runtime_env"

chmod 660 "$fixture_root/run/user/$fixture_uid/docker.sock"
expect_failure "non-private rootless Docker socket" \
  "${host_environment[@]}" "$host_contract/validate.sh"
chmod 600 "$fixture_root/run/user/$fixture_uid/docker.sock"

expect_failure "CI supplementary group" env \
  MOCK_CI_EXTRA_GROUP=1 \
  SPEAKUP_STAGING_HOST_TESTING=1 \
  "SPEAKUP_STAGING_HOST_ROOT=$fixture_root" \
  "SPEAKUP_STAGING_HOST_TEST_PATH=$mock_bin:/usr/bin:/bin:/usr/sbin:/sbin" \
  "$host_contract/validate.sh"

if find "$fixture_root" -type f \( \
  -name '*.key' -o -name '*.pem' -o -name '.env' \) -print -quit | grep -q .; then
  fail "bootstrap created a Secret-like file"
fi
[[ -f "$runtime_env" && -f "$server_env" && -f "$registry_config" ]] ||
  fail "operator-provisioned fixture inputs disappeared"

printf '%s\n' 'Staging host access contract tests passed.'
