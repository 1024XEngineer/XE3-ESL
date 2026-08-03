#!/bin/zsh

set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$tool_dir/../.." && pwd)"
server_dir="$repo_dir/server"
mobile_source_dir="$repo_dir/mobile"
server_port="${SPEAKUP_DEV_PORT:-18080}"
base_url="http://127.0.0.1:$server_port"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/xe3-ios-simulator-dev.XXXXXX")"
mobile_dir="$run_dir/mobile"
override_file="$mobile_dir/pubspec_overrides.yaml"
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
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for command_name in curl docker flutter go lsof rsync xcrun; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    print -u2 "缺少命令：$command_name"
    exit 1
  fi
done

if lsof -nP -iTCP:"$server_port" -sTCP:LISTEN >/dev/null 2>&1; then
  print -u2 "端口 $server_port 已被占用。请先停止旧后端，或设置 SPEAKUP_DEV_PORT 使用其他端口。"
  exit 1
fi

if [[ ! -f "$repo_dir/.env" ]]; then
  print -u2 "缺少 $repo_dir/.env，无法启动真实后端。"
  exit 1
fi
set -a
source "$repo_dir/.env"
set +a

device_id="${IOS_SIMULATOR_ID:-}"
if [[ -n "$device_id" ]]; then
  xcrun simctl boot "$device_id" >/dev/null 2>&1 || true
else
  device_id="$(
    xcrun simctl list devices booted |
      awk -F '[()]' '/iPhone/ {print $2; exit}'
  )"
fi
if [[ -z "$device_id" ]]; then
  print -u2 "没有已启动的 iPhone 模拟器。请先打开 Simulator，或设置 IOS_SIMULATOR_ID。"
  exit 1
fi
xcrun simctl bootstatus "$device_id" -b

rsync -a \
  --exclude '/.dart_tool/' \
  --exclude '/build/' \
  --exclude '/ios/Pods/' \
  --exclude '/pubspec_overrides.yaml' \
  "$mobile_source_dir/" \
  "$mobile_dir/"
cat >"$override_file" <<EOF
dependency_overrides:
  avatar_kit:
    path: $tool_dir/avatar_kit_stub
EOF

print "启动 PostgreSQL..."
docker compose -p xe3-esl -f "$repo_dir/compose.yaml" up -d --wait postgres

print "执行数据库迁移..."
(cd "$server_dir" && go run ./cmd/migrate up)

print "构建并启动本地后端..."
(cd "$server_dir" && go build -o "$server_binary" ./cmd/server)
(
  cd "$server_dir"
  SERVER_HOST=127.0.0.1 \
  SERVER_PORT="$server_port" \
  "$server_binary"
) >"$server_log" 2>&1 &
server_pid=$!

ready=0
for _ in {1..120}; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    print -u2 "后端启动失败："
    tail -80 "$server_log" >&2
    exit 1
  fi
  if curl --silent --fail --max-time 2 "$base_url/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.25
done
if [[ "$ready" != "1" ]]; then
  print -u2 "等待后端就绪超时："
  tail -80 "$server_log" >&2
  exit 1
fi

print "启动 SpeakUp iOS Simulator；数字人已明确禁用，按 q 退出。"
cd "$mobile_dir"
flutter run \
  -d "$device_id" \
  --dart-define="SPEAKUP_API_BASE_URL=$base_url" \
  "$@"
