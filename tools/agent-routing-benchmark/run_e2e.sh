#!/bin/zsh

set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$tool_dir/../.." && pwd)"
server_dir="$repo_dir/server"
report_dir="${BENCHMARK_REPORT_DIR:-$tool_dir/reports}"
server_port="${BENCHMARK_PORT:-18080}"
report_port="${BENCHMARK_REPORT_PORT:-0}"
base_url="http://127.0.0.1:$server_port"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/xe3-agent-routing-benchmark.XXXXXX")"
server_binary="$run_dir/server"
server_log="$run_dir/server.log"
report_server_log="$run_dir/report-server.log"
report_url_file="$run_dir/report-url"
server_pid=""
report_server_pid=""
benchmark_database_name=""
benchmark_database_created=0
benchmark_database_url=""

stop_process() {
  local process_id="$1"
  if [[ -n "$process_id" ]] && kill -0 "$process_id" 2>/dev/null; then
    kill "$process_id" 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
}

cleanup() {
  stop_process "$report_server_pid"
  stop_process "$server_pid"
  if [[ "$benchmark_database_created" == "1" &&
        "$benchmark_database_name" == xe3_benchmark_* ]]; then
    docker compose -f "$repo_dir/compose.yaml" exec -T postgres \
      sh -c 'dropdb --if-exists --force --username="$POSTGRES_USER" "$1"' \
      benchmark-cleanup "$benchmark_database_name" >/dev/null 2>&1 || true
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

benchmark_database_name="xe3_benchmark_$(date +%s)_$$"
benchmark_database_url="$(
  node "$tool_dir/database_config.mjs" \
    "$repo_dir" "$benchmark_database_name" url
)"
benchmark_database_owner="$(
  node "$tool_dir/database_config.mjs" \
    "$repo_dir" "$benchmark_database_name" owner
)"
print "创建隔离测试数据库..."
docker compose -f "$repo_dir/compose.yaml" exec -T postgres \
  sh -c 'createdb --username="$POSTGRES_USER" --owner="$2" "$1"' \
  benchmark-create "$benchmark_database_name" "$benchmark_database_owner"
benchmark_database_created=1

print "执行隔离数据库迁移..."
(cd "$server_dir" && DATABASE_URL="$benchmark_database_url" go run ./cmd/migrate up)

print "构建本地后端..."
(cd "$server_dir" && go build -o "$server_binary" ./cmd/server)

print "启动本地后端（AGENT_TOOL_FIXTURES=1，端口 $server_port）..."
(
  cd "$server_dir"
  SERVER_HOST=127.0.0.1 \
  SERVER_PORT="$server_port" \
  LOG_LEVEL=debug \
  AGENT_TOOL_FIXTURES=1 \
  DATABASE_URL="$benchmark_database_url" \
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

stop_process "$server_pid"
server_pid=""

latest_html="$report_dir/latest.html"
if [[ -f "$latest_html" && "${BENCHMARK_OPEN_REPORT:-1}" == "1" ]]; then
  interactive="${BENCHMARK_INTERACTIVE:-}"
  if [[ -z "$interactive" ]]; then
    if [[ -t 0 ]]; then interactive=1; else interactive=0; fi
  fi
  if [[ "$interactive" == "1" ]]; then
    node "$tool_dir/history_server.mjs" \
      --port "$report_port" \
      --report-dir "$report_dir" \
      --url-file "$report_url_file" >"$report_server_log" 2>&1 &
    report_server_pid=$!
    report_ready=0
    for _ in {1..40}; do
      if ! kill -0 "$report_server_pid" 2>/dev/null; then break; fi
      if [[ -s "$report_url_file" ]]; then
        report_base_url="$(<"$report_url_file")"
        if curl --silent --fail --max-time 2 \
          "$report_base_url/api/health" >/dev/null 2>&1; then
          report_ready=1
          break
        fi
      fi
      sleep 0.1
    done
    if [[ "$report_ready" == "1" ]]; then
      open "$report_base_url/latest.html"
      print
      print "报告已打开。点击“记录本次结果”可加入历史趋势。"
      print "完成查看和记录后，回到此窗口按回车关闭报告服务。"
      read -r
    else
      print -u2 "本地报告服务启动失败，将直接打开静态报告："
      tail -20 "$report_server_log" >&2
      open "$latest_html"
    fi
  else
    open "$latest_html"
  fi
fi

if [[ "$benchmark_status" == "2" ]]; then
  print
  print "Benchmark 已完成，但存在不符合预期的用例。"
elif [[ "$benchmark_status" != "0" ]]; then
  print -u2 "Benchmark 执行失败。"
fi
exit "$benchmark_status"
