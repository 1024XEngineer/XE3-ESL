#!/usr/bin/env bash

set -euo pipefail
umask 022

readonly metrics_directory="/var/lib/speakup-cn-experiment/metrics"
readonly metrics_file="$metrics_directory/speakup-cn-experiment.prom"
readonly api_origin="http://127.0.0.1:28083"

fail() {
  printf 'China experiment metrics export error: %s\n' "$*" >&2
  exit 1
}

for command in chmod curl df docker install jq mktemp mv rm stat tail; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done

[[ -d "$metrics_directory" && ! -L "$metrics_directory" ]] ||
  fail "$metrics_directory must be a real directory"
[[ "$(stat --format='%U:%G:%a' "$metrics_directory")" == "root:root:755" ]] ||
  fail "$metrics_directory must be root:root mode 0755"

temporary_file="$(mktemp "$metrics_directory/.speakup-cn-experiment.XXXXXX")"
ready_body="$(mktemp)"
trap 'rm -f -- "$temporary_file" "$ready_body"' EXIT

health_up=0
health_duration=0
if measured="$(curl --fail --silent --show-error --max-time 3 \
  --output /dev/null --write-out '%{time_total}' "$api_origin/health")"; then
  health_up=1
  health_duration="$measured"
fi

ready_up=0
ready_duration=0
if measured="$(curl --fail --silent --show-error --max-time 3 \
  --output "$ready_body" --write-out '%{time_total}' "$api_origin/readyz")"; then
  ready_duration="$measured"
  if jq --exit-status \
    '.status == "ready" and .checks.database == "ready"' \
    "$ready_body" >/dev/null; then
    ready_up=1
  fi
fi

read -r filesystem_size filesystem_available < <(
  df --block-size=1 --output=size,avail / | tail -n 1
)
read -r filesystem_files filesystem_files_free < <(
  df --output=itotal,iavail / | tail -n 1
)
for value in "$filesystem_size" "$filesystem_available" \
  "$filesystem_files" "$filesystem_files_free"; do
  [[ "$value" =~ ^[0-9]+$ ]] || fail "df returned an invalid value"
done

{
  printf '# HELP speakup_cn_experiment_api_up Whether the local API endpoint is healthy.\n'
  printf '# TYPE speakup_cn_experiment_api_up gauge\n'
  printf 'speakup_cn_experiment_api_up{endpoint="health"} %s\n' "$health_up"
  printf 'speakup_cn_experiment_api_up{endpoint="readyz"} %s\n' "$ready_up"
  printf '# HELP speakup_cn_experiment_api_probe_duration_seconds Local API probe duration.\n'
  printf '# TYPE speakup_cn_experiment_api_probe_duration_seconds gauge\n'
  printf 'speakup_cn_experiment_api_probe_duration_seconds{endpoint="health"} %s\n' "$health_duration"
  printf 'speakup_cn_experiment_api_probe_duration_seconds{endpoint="readyz"} %s\n' "$ready_duration"

  for service in server postgres; do
    container="xe3-speakup-cn-experiment-${service}-1"
    running=0
    healthy=0
    restarts=0
    if state="$(docker container inspect "$container" 2>/dev/null)"; then
      if jq --exit-status 'length == 1 and .[0].State.Status == "running"' \
        <<<"$state" >/dev/null; then
        running=1
      fi
      if jq --exit-status 'length == 1 and .[0].State.Health.Status == "healthy"' \
        <<<"$state" >/dev/null; then
        healthy=1
      fi
      restarts="$(jq --raw-output '.[0].RestartCount' <<<"$state")"
      [[ "$restarts" =~ ^[0-9]+$ ]] || fail "Docker returned an invalid restart count"
    fi
    printf 'speakup_cn_experiment_container_running{service="%s"} %s\n' \
      "$service" "$running"
    printf 'speakup_cn_experiment_container_healthy{service="%s"} %s\n' \
      "$service" "$healthy"
    printf 'speakup_cn_experiment_container_restart_count{service="%s"} %s\n' \
      "$service" "$restarts"
  done

  printf '# HELP speakup_cn_experiment_filesystem_size_bytes Root filesystem size.\n'
  printf '# TYPE speakup_cn_experiment_filesystem_size_bytes gauge\n'
  printf 'speakup_cn_experiment_filesystem_size_bytes %s\n' "$filesystem_size"
  printf '# HELP speakup_cn_experiment_filesystem_available_bytes Root filesystem available bytes.\n'
  printf '# TYPE speakup_cn_experiment_filesystem_available_bytes gauge\n'
  printf 'speakup_cn_experiment_filesystem_available_bytes %s\n' "$filesystem_available"
  printf '# HELP speakup_cn_experiment_filesystem_files Root filesystem inode count.\n'
  printf '# TYPE speakup_cn_experiment_filesystem_files gauge\n'
  printf 'speakup_cn_experiment_filesystem_files %s\n' "$filesystem_files"
  printf '# HELP speakup_cn_experiment_filesystem_files_free Root filesystem free inodes.\n'
  printf '# TYPE speakup_cn_experiment_filesystem_files_free gauge\n'
  printf 'speakup_cn_experiment_filesystem_files_free %s\n' "$filesystem_files_free"
} >"$temporary_file"

chmod 0644 "$temporary_file"
mv -- "$temporary_file" "$metrics_file"
trap 'rm -f -- "$ready_body"' EXIT
