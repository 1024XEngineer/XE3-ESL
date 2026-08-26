#!/usr/bin/env bash
set -euo pipefail

directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

if [[ "$(grep -Ec '^[[:space:]]+ports:$' "$directory/compose.yaml")" -ne 1 ]] ||
  ! grep -Fqx '      - "127.0.0.1:28083:8080"' "$directory/compose.yaml"; then
  printf 'Server API must have exactly one loopback-only host port mapping\n' >&2
  exit 1
fi

server_env="$temporary_directory/server.env"
runtime_env="$temporary_directory/runtime.env"
printf '%s\n' 'TEXT_GENERATION_PROVIDER=test-fixture' >"$server_env"

write_runtime() {
  local password="$1"
  printf '%s\n' \
    'SERVER_IMAGE_DIGEST=sha256:77718703587e0ad027c7b4d15856dbddfb40554946a935de26dc4ac6500428c5' \
    'EXPERIMENT_POSTGRES_DB=speakup_cn_experiment' \
    'EXPERIMENT_POSTGRES_USER=speakup_cn_experiment' \
    "EXPERIMENT_POSTGRES_PASSWORD=$password" \
    "EXPERIMENT_SERVER_ENV_FILE=$server_env" >"$runtime_env"
}

write_runtime '0123456789abcdef_ABCDEF-'
"$directory/validate-runtime.sh" "$runtime_env" >/dev/null

write_runtime 'contains-url-unsafe-@-delimiter'
if "$directory/validate-runtime.sh" "$runtime_env" >/dev/null 2>&1; then
  printf 'validator accepted a URL-unsafe password\n' >&2
  exit 1
fi

write_runtime 'too-short'
if "$directory/validate-runtime.sh" "$runtime_env" >/dev/null 2>&1; then
  printf 'validator accepted a short password\n' >&2
  exit 1
fi

write_runtime 'replace-with-at-least-24-url-safe-characters'
if "$directory/validate-runtime.sh" "$runtime_env" >/dev/null 2>&1; then
  printf 'validator accepted the documented placeholder password\n' >&2
  exit 1
fi

printf '%s\n' 'DATABASE_URL=postgres://forbidden' >>"$server_env"
write_runtime '0123456789abcdef_ABCDEF-'
if "$directory/validate-runtime.sh" "$runtime_env" >/dev/null 2>&1; then
  printf 'validator accepted a Compose-owned server key\n' >&2
  exit 1
fi

printf 'China backend experiment contract tests passed\n'
