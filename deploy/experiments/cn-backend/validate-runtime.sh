#!/usr/bin/env bash
set -euo pipefail

runtime_env="${1:-}"
if [[ -z "$runtime_env" || "$#" -ne 1 ]]; then
  printf 'usage: %s RUNTIME_ENV\n' "$0" >&2
  exit 64
fi
if [[ ! -f "$runtime_env" || -L "$runtime_env" ]]; then
  printf 'runtime environment must be a regular file\n' >&2
  exit 1
fi

read_value() {
  local key="$1"
  local value
  local count
  count="$(awk -F= -v key="$key" '$1 == key {count++} END {print count+0}' "$runtime_env")"
  if [[ "$count" -ne 1 ]]; then
    printf '%s must occur exactly once\n' "$key" >&2
    exit 1
  fi
  value="$(awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print}' "$runtime_env")"
  if [[ -z "$value" ]]; then
    printf '%s must not be empty\n' "$key" >&2
    exit 1
  fi
  printf '%s' "$value"
}

server_repository="$(read_value SERVER_IMAGE_REPOSITORY)"
server_digest="$(read_value SERVER_IMAGE_DIGEST)"
postgres_database="$(read_value EXPERIMENT_POSTGRES_DB)"
postgres_user="$(read_value EXPERIMENT_POSTGRES_USER)"
postgres_password="$(read_value EXPERIMENT_POSTGRES_PASSWORD)"
server_env="$(read_value EXPERIMENT_SERVER_ENV_FILE)"

case "$server_repository" in
  ghcr.io/1024xengineer/xe3-esl-server) ;;
  crpi-uzndbvgv3nza56mp.cn-beijing.personal.cr.aliyuncs.com/speakup/xe3-esl-server) ;;
  *)
    printf 'SERVER_IMAGE_REPOSITORY is not an approved Server repository\n' >&2
    exit 1
    ;;
esac
if [[ ! "$server_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  printf 'SERVER_IMAGE_DIGEST must be a sha256 digest\n' >&2
  exit 1
fi
for identifier in "$postgres_database" "$postgres_user"; do
  if [[ ! "$identifier" =~ ^[a-z][a-z0-9_]{0,62}$ ]]; then
    printf 'PostgreSQL identifiers must be lowercase and URL-safe\n' >&2
    exit 1
  fi
done
if [[ "$postgres_password" == "replace-with-at-least-24-url-safe-characters" ||
      ${#postgres_password} -lt 24 || ${#postgres_password} -gt 128 ||
      ! "$postgres_password" =~ ^[A-Za-z0-9_-]+$ ]]; then
  printf 'EXPERIMENT_POSTGRES_PASSWORD must contain 24-128 URL-safe characters\n' >&2
  exit 1
fi
if [[ "$server_env" != /* || ! -f "$server_env" || -L "$server_env" ]]; then
  printf 'EXPERIMENT_SERVER_ENV_FILE must be an absolute regular file\n' >&2
  exit 1
fi
if grep -Eq '^(DATABASE_URL|SERVER_HOST|SERVER_PORT)=' "$server_env"; then
  printf 'server environment contains a Compose-owned key\n' >&2
  exit 1
fi

printf 'runtime environment is valid\n'
