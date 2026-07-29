#!/bin/zsh

set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$tool_dir/../.." && pwd)"
server_dir="$repo_dir/server"
report_dir="${BENCHMARK_REPORT_DIR:-$tool_dir/reports}"
server_port="${BENCHMARK_PORT:-18080}"
base_url="http://127.0.0.1:$server_port"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/xe3-agent-routing-benchmark.XXXXXX")"
server_binary="$run_dir/server"
server_log="$run_dir/server.log"
server_pid=""

cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$run_dir"
}
trap cleanup EXIT INT TERM

for command_name in go node curl docker; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    print -u2 "缺少命令：$command_name"
    exit 1
  fi
done

if curl --silent --fail --max-time 2 "$base_url/health" >/dev/null 2>&1; then
  print -u2 "端口 $server_port 已有服务运行。请设置 BENCHMARK_PORT 使用其他端口。"
  exit 1
fi

running_services="$(
  docker compose -f "$repo_dir/compose.yaml" ps --status running --services
)"
if [[ $'\n'"$running_services"$'\n' != *$'\npostgres\n'* ]]; then
  print -u2 "PostgreSQL 容器未运行，请先执行 docker compose up -d postgres。"
  exit 1
fi

if [[ "${BENCHMARK_RUN_MIGRATIONS:-0}" == "1" ]]; then
  (cd "$server_dir" && go run ./cmd/migrate up)
fi

print "构建本地后端..."
(cd "$server_dir" && go build -o "$server_binary" ./cmd/server)

print "启动本地后端（AGENT_TOOL_FIXTURES=1，端口 $server_port）..."
(
  cd "$server_dir"
  SERVER_HOST=127.0.0.1 \
  SERVER_PORT="$server_port" \
  LOG_LEVEL=debug \
  AGENT_TOOL_FIXTURES=1 \
  "$server_binary"
) >"$server_log" 2>&1 &
server_pid=$!

ready=0
for _ in {1..120}; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    print -u2 "后端启动失败："
    tail -40 "$server_log" >&2
    exit 1
  fi
  if curl --silent --fail --max-time 2 "$base_url/health" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.25
done
if [[ "$ready" != "1" ]]; then
  print -u2 "等待后端健康检查超时。"
  tail -40 "$server_log" >&2
  exit 1
fi

set +e
node "$tool_dir/benchmark.mjs" \
  --base-url "$base_url" \
  --cases "${BENCHMARK_CASES_FILE:-$tool_dir/cases.json}" \
  --log-file "$server_log" \
  --report-dir "$report_dir"
benchmark_status=$?
set -e

latest_html="$report_dir/latest.html"
if [[ -f "$latest_html" && "${BENCHMARK_OPEN_REPORT:-1}" == "1" ]]; then
  open "$latest_html"
fi

if [[ "$benchmark_status" == "2" ]]; then
  print
  print "Benchmark 已完成，但存在不符合预期的用例。"
elif [[ "$benchmark_status" != "0" ]]; then
  print -u2 "Benchmark 执行失败。"
fi
exit "$benchmark_status"
