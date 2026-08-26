#!/usr/bin/env bash

set -euo pipefail

readonly directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly host="$directory/host"

fail() { printf 'production host contract test: %s\n' "$*" >&2; exit 1; }
for script in "$host/bootstrap.sh" "$host/validate.sh" "$host/ssh-gate"; do bash -n "$script"; done

[[ "$(< "$host/sudoers")" == 'speakup-production-ci ALL=(root) NOPASSWD: /usr/local/libexec/speakup-production-broker ""' ]] || fail "sudoers is not the exact no-argument broker rule"
grep -Fxq '    ForceCommand /usr/local/libexec/speakup-production-ssh-gate' "$host/sshd_config" || fail "forced command is missing"
grep -Fxq '    DisableForwarding yes' "$host/sshd_config" || fail "forwarding is not disabled"
grep -Fxq '    PermitTTY no' "$host/sshd_config" || fail "TTY is not disabled"
grep -Fq "printf 'restrict %s\\n'" "$host/bootstrap.sh" || fail "bootstrap does not restrict the authorized key"
grep -Fq 'deploy/production/manage.sh' "$host/bootstrap.sh" || fail "bootstrap does not snapshot the Production contract"
grep -Fq 'deploy/android-download/manage.sh' "$host/bootstrap.sh" || fail "bootstrap does not snapshot Android publication"

temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
mkdir -p "$temporary/bin"
cat > "$temporary/bin/sudo" <<'EOF'
#!/usr/bin/env bash
printf '%q\n' "$@" > "$PRODUCTION_TEST_SUDO_ARGS"
EOF
chmod 755 "$temporary/bin/sudo"
sed "s|/usr/bin/sudo|$temporary/bin/sudo|" "$host/ssh-gate" > "$temporary/ssh-gate"
chmod 755 "$temporary/ssh-gate"
PRODUCTION_TEST_SUDO_ARGS="$temporary/args" "$temporary/ssh-gate"
arguments=()
while IFS= read -r argument; do arguments+=("$argument"); done < "$temporary/args"
[[ "${arguments[*]}" == '-n -u root -- /usr/local/libexec/speakup-production-broker' ]] || fail "gate invokes an unexpected command"
if SSH_ORIGINAL_COMMAND='scp -t /tmp/file' PRODUCTION_TEST_SUDO_ARGS="$temporary/args" "$temporary/ssh-gate" >/dev/null 2>&1; then fail "gate accepted scp"; fi
if PRODUCTION_TEST_SUDO_ARGS="$temporary/args" "$temporary/ssh-gate" unexpected >/dev/null 2>&1; then fail "gate accepted arguments"; fi

printf '%s\n' 'Production host access contract tests passed.'
