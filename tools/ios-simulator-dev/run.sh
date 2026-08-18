#!/bin/zsh

set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
script_path="$0"
repo_dir="$(cd "$tool_dir/../.." && pwd)"
server_dir="$repo_dir/server"
mobile_source_dir="$repo_dir/mobile"
server_port="${SPEAKUP_DEV_PORT:-18080}"
base_url="http://127.0.0.1:$server_port"
run_dir="${SPEAKUP_SIMULATOR_WORK_DIR:-${TMPDIR:-/tmp}/xe3-ios-simulator-dev-cache}"
mobile_dir="$run_dir/mobile"
avatar_stub_dir="$run_dir/avatar_kit_stub"
override_file="$mobile_dir/pubspec_overrides.yaml"
server_binary="$run_dir/server"
server_log="$run_dir/server.log"
server_revision_file="$run_dir/server.commit"
server_pid=""
device_id=""
backend_owned=0
flutter_mode="run"
launcher_mode="full"

usage() {
  print "Usage: $script_path [full|desktop|backend|frontend|status|test] [flutter arguments...]"
  print "  full      Start an owned backend, then Flutter in this terminal."
  print "  desktop   Open backend in another Terminal window, then start Flutter."
  print "  backend   Start PostgreSQL, migrate, and keep the backend attached."
  print "  frontend  Start only Flutter; report backend availability separately."
  print "  status    Check whether port $server_port belongs to a ready XE3-ESL backend."
  print "  test      Preserve the legacy integration-test entry point."
}

case "${1:-full}" in
  full | desktop | backend | frontend | status)
    launcher_mode="$1"
    shift
    ;;
  test)
    launcher_mode="full"
    flutter_mode="test"
    shift
    ;;
  -h | --help | help)
    usage
    exit 0
    ;;
  *)
    print -u2 "未知启动模式：$1"
    usage >&2
    exit 2
    ;;
esac

cleanup() {
  if [[ "$backend_owned" == "1" && -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

require_commands() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      print -u2 "缺少命令：$command_name"
      exit 1
    fi
  done
}

listener_pids() {
  lsof -nP -t -iTCP:"$server_port" -sTCP:LISTEN 2>/dev/null | sort -u
}

backend_http_ready() {
  local health ready
  health="$(curl --silent --fail --max-time 2 "$base_url/health" 2>/dev/null || true)"
  ready="$(curl --silent --fail --max-time 2 "$base_url/readyz" 2>/dev/null || true)"
  [[ "$health" == *'"status":"ok"'* ]] &&
    [[ "$ready" == *'"status":"ready"'* ]] &&
    [[ "$ready" == *'"database":"ready"'* ]]
}

pid_is_xe3_backend() {
  local pid="$1"
  local process_cwd process_command
  process_cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
  process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  if [[ "$process_command" == *"$server_binary"* ]]; then
    return 0
  fi
  [[ "$process_cwd" == */server &&
    -f "$process_cwd/../mobile/pubspec.yaml" &&
    -f "$process_cwd/../AGENTS.md" ]]
}

listener_is_xe3_backend() {
  local pid
  local -a pids
  pids=("${(@f)$(listener_pids)}")
  for pid in "${pids[@]}"; do
    [[ -n "$pid" ]] || continue
    pid_is_xe3_backend "$pid" && return 0
  done
  return 1
}

backend_status() {
  local pids
  pids="$(listener_pids)"
  if [[ -z "$pids" ]]; then
    return 1
  fi
  if listener_is_xe3_backend && backend_http_ready; then
    return 0
  fi
  return 2
}

backend_matches_checkout() {
  local current_revision pid process_command
  [[ -x "$server_binary" && -f "$server_revision_file" ]] || return 1
  current_revision="$(git -C "$repo_dir" rev-parse HEAD)"
  [[ "$(<"$server_revision_file")" == "$current_revision" ]] || return 1
  if find "$server_dir" -type f -newer "$server_binary" -print -quit | rg -q .; then
    return 1
  fi
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    [[ "$process_command" == *"$server_binary"* ]] && return 0
  done <<<"$(listener_pids)"
  return 1
}

stop_existing_backend() {
  local pid attempt
  local -a owned_pids
  owned_pids=()
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    if pid_is_xe3_backend "$pid"; then
      owned_pids+=("$pid")
      print "当前 XE3-ESL 后端不是本 checkout 的最新构建，正在安全停止（PID $pid）..."
      kill -INT "$pid"
    fi
  done <<<"$(listener_pids)"
  for pid in "${owned_pids[@]}"; do
    for attempt in {1..80}; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    if kill -0 "$pid" 2>/dev/null; then
      print -u2 "旧后端未在预期时间内退出，未强制终止：PID $pid"
      return 1
    fi
  done
}

describe_listener_conflict() {
  local pid process_cwd process_command
  print -u2 "端口 $server_port 已被其他或未就绪的进程占用："
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    process_cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
    process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    print -u2 "  PID $pid"
    [[ -n "$process_cwd" ]] && print -u2 "  工作目录：$process_cwd"
    [[ -n "$process_command" ]] && print -u2 "  命令：$process_command"
  done <<<"$(listener_pids)"
  print -u2 "未停止该进程。请确认后手动处理，或设置 SPEAKUP_DEV_PORT 使用其他端口。"
}

start_backend() {
  local status_code=0
  if backend_status; then
    if backend_matches_checkout; then
      print "XE3-ESL 后端已在 $base_url 运行，且代码版本一致，将直接复用。"
      return 0
    fi
    stop_existing_backend
  else
    status_code=$?
    if [[ "$status_code" == "2" ]]; then
      describe_listener_conflict
      return 1
    fi
  fi

  if [[ ! -f "$repo_dir/.env" ]]; then
    print -u2 "缺少 $repo_dir/.env，无法启动真实后端。"
    return 1
  fi
  set -a
  source "$repo_dir/.env"
  set +a

  mkdir -p "$run_dir"
  print "启动 PostgreSQL..."
  docker compose -p xe3-esl -f "$repo_dir/compose.yaml" up -d --wait postgres

  print "检查并执行数据库迁移..."
  (cd "$server_dir" && go run ./cmd/migrate status)
  (cd "$server_dir" && go run ./cmd/migrate up)
  (cd "$server_dir" && go run ./cmd/migrate status)

  print "构建并启动本地后端..."
  (cd "$server_dir" && go build -o "$server_binary" ./cmd/server)
  git -C "$repo_dir" rev-parse HEAD >"$server_revision_file"
  if [[ "$launcher_mode" == "backend" ]]; then
    (
      cd "$server_dir"
      SERVER_HOST=127.0.0.1 SERVER_PORT="$server_port" "$server_binary"
    ) > >(tee "$server_log") 2>&1 &
  else
    (
      cd "$server_dir"
      SERVER_HOST=127.0.0.1 SERVER_PORT="$server_port" "$server_binary"
    ) >"$server_log" 2>&1 &
  fi
  server_pid=$!
  backend_owned=1

  local ready=0
  local attempt
  for attempt in {1..120}; do
    if ! kill -0 "$server_pid" 2>/dev/null; then
      print -u2 "后端启动失败："
      tail -80 "$server_log" >&2
      return 1
    fi
    if rg -q '"msg":"server started"' "$server_log" 2>/dev/null && backend_http_ready; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [[ "$ready" != "1" ]]; then
    print -u2 "等待后端就绪超时："
    tail -80 "$server_log" >&2
    return 1
  fi
  print "后端已就绪：$base_url（数据库 ready）"
}

stop_existing_frontend() {
  local pid process_cwd process_command
  local -a flutter_pids owned_pids
  flutter_pids=("${(@f)$(ps -ax -o pid=,command= | awk '/flutter_tools\.snapshot run/ && !/awk/ {print $1}')}")
  owned_pids=()
  for pid in "${flutter_pids[@]}"; do
    [[ -n "$pid" ]] || continue
    process_cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
    process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    if [[ "$process_command" == *"SPEAKUP_API_BASE_URL=$base_url"* &&
          ( "$process_cwd" == "$mobile_dir" || "$process_cwd" == "$mobile_source_dir" ) ]]; then
      owned_pids+=("$pid")
      print "正在安全停止旧的 XE3-ESL Flutter 会话（PID $pid）..."
      kill -INT "$pid"
    fi
  done
  for pid in "${owned_pids[@]}"; do
    local attempt
    for attempt in {1..80}; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    if kill -0 "$pid" 2>/dev/null; then
      print -u2 "旧 Flutter 会话未在预期时间内退出，未强制终止：PID $pid"
      return 1
    fi
  done
}

select_simulator() {
  device_id="${IOS_SIMULATOR_ID:-}"
  if [[ -z "$device_id" ]]; then
    device_id="$(xcrun simctl list devices booted | awk -F '[()]' '/iPhone/ {print $2; exit}')"
  fi
  if [[ -z "$device_id" ]]; then
    device_id="$(xcrun simctl list devices available | awk -F '[()]' '/iPhone/ {print $2; exit}')"
    if [[ -z "$device_id" ]]; then
      print -u2 "没有可用的 iPhone Simulator。"
      return 1
    fi
    print "启动 iPhone Simulator：$device_id"
    xcrun simctl boot "$device_id" >/dev/null 2>&1 || true
  fi
  open -a Simulator
  xcrun simctl bootstatus "$device_id" -b
}

prepare_ios_runtime() {
  mkdir -p "$mobile_dir" "$avatar_stub_dir"
  rsync -a --delete \
    --exclude '/.dart_tool/' \
    --exclude '/build/' \
    --exclude '/ios/Pods/' \
    --exclude '/pubspec_overrides.yaml' \
    "$mobile_source_dir/" \
    "$mobile_dir/"

  if [[ "$(uname -m)" == "x86_64" ]]; then
    rsync -a --delete "$tool_dir/avatar_kit_stub/" "$avatar_stub_dir/"
    {
      print 'dependency_overrides:'
      print '  avatar_kit:'
      print "    path: $avatar_stub_dir"
    } >"$override_file"
    print "Intel Simulator 兼容模式已启用：临时副本禁用 Avatar 原生插件。"
  else
    rm -f "$override_file"
  fi
}

run_frontend() {
  stop_existing_frontend
  select_simulator
  prepare_ios_runtime
  if backend_http_ready; then
    print "后端连接正常：$base_url"
  else
    print -u2 "提示：后端 $base_url 当前不可用；前端仍会启动，可在后端就绪后重试请求。"
  fi
  if [[ "$flutter_mode" == "test" ]]; then
    print "在 iOS Simulator 运行 SpeakUp 集成测试；数字人已明确禁用。"
  else
    print "启动 SpeakUp iOS Simulator；按 q 退出，修改代码后按 r 热重载。"
  fi
  cd "$mobile_dir"
  flutter "$flutter_mode" \
    "$@" \
    -d "$device_id" \
    --dart-define="SPEAKUP_API_BASE_URL=$base_url" \
    --dart-define="SPEAKUP_AVATAR_ENABLED=false"
}

start_desktop_backend() {
  local status_code=0
  if backend_status; then
    if backend_matches_checkout; then
      print "XE3-ESL 后端已在 $base_url 运行，且代码版本一致，将直接复用。"
      return 0
    fi
    stop_existing_backend
  else
    status_code=$?
    if [[ "$status_code" == "2" ]]; then
      describe_listener_conflict
      return 1
    fi
  fi

  local backend_launcher="$repo_dir/Start SpeakUp Backend.command"
  if [[ ! -x "$backend_launcher" ]]; then
    print -u2 "后端启动器不存在或不可执行：$backend_launcher"
    return 1
  fi
  print "在新的 Terminal 窗口启动后端..."
  open -a Terminal "$backend_launcher"
  local attempt
  for attempt in {1..600}; do
    if backend_status; then
      print "后端已就绪，继续启动前端。"
      return 0
    fi
    sleep 0.5
  done
  print -u2 "等待后端就绪超时，请查看后端 Terminal 窗口中的日志。"
  return 1
}

case "$launcher_mode" in
  status)
    require_commands curl lsof ps sed sort
    if backend_status; then
      print "ready $base_url"
      exit 0
    else
      status_code=$?
    fi
    if [[ "$status_code" == "2" ]]; then
      describe_listener_conflict
    else
      print "stopped $base_url"
    fi
    exit "$status_code"
    ;;
  backend)
    require_commands curl docker find git go lsof ps rg sed sort tee
    start_backend
    if [[ "$backend_owned" == "1" ]]; then
      print "后端保持运行；按 Ctrl+C 安全退出。"
      wait "$server_pid"
    fi
    ;;
  frontend)
    require_commands awk curl flutter lsof open ps rsync sed uname xcrun
    run_frontend "$@"
    ;;
  desktop)
    require_commands awk curl find flutter git lsof open ps rg rsync sed sort uname xcrun
    stop_existing_frontend
    # A legacy full-stack session may stop its owned backend just after Flutter exits.
    sleep 1
    start_desktop_backend
    run_frontend "$@"
    ;;
  full)
    require_commands awk curl docker find flutter git go lsof open ps rg rsync sed sort tee uname xcrun
    start_backend
    run_frontend "$@"
    ;;
esac
