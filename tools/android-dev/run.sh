#!/bin/zsh

set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$tool_dir/../.." && pwd)"
server_dir="$repo_dir/server"
mobile_dir="$repo_dir/mobile"
server_port="${SPEAKUP_DEV_PORT:-18080}"
base_url="http://127.0.0.1:$server_port"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/xe3-android-dev.XXXXXX")"
server_binary="$run_dir/server"
server_log="$run_dir/server.log"
server_pid=""
device_id=""
reverse_added=0

cleanup() {
  if [[ -n "$device_id" && "$reverse_added" == "1" ]]; then
    adb -s "$device_id" reverse --remove "tcp:$server_port" >/dev/null 2>&1 || true
  fi
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$run_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for command_name in adb curl docker flutter go; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    print -u2 "缺少命令：$command_name"
    exit 1
  fi
done

if [[ ! -f "$repo_dir/.env" ]]; then
  print -u2 "缺少 $repo_dir/.env，无法启动真实后端。"
  exit 1
fi

set -a
source "$repo_dir/.env"
set +a

adb start-server >/dev/null

if [[ -n "${ANDROID_DEVICE_ID:-}" ]]; then
  device_id="$ANDROID_DEVICE_ID"
  if [[ "$(adb -s "$device_id" get-state 2>/dev/null || true)" != "device" ]]; then
    print -u2 "ANDROID_DEVICE_ID 指定的设备未连接或尚未授权：$device_id"
    exit 1
  fi
else
  connected_devices="$(
    adb devices |
      awk 'NR > 1 && $2 == "device" && $1 !~ /^emulator-/ {print $1}'
  )"
  device_count="$(
    print -r -- "$connected_devices" |
      sed '/^[[:space:]]*$/d' |
      wc -l |
      tr -d ' '
  )"
  if [[ "$device_count" == "0" ]]; then
    if adb devices | awk 'NR > 1 && $2 == "unauthorized" {found=1} END {exit !found}'; then
      print -u2 "手机尚未授权。请解锁手机并允许这台 Mac 进行 USB 调试。"
    else
      print -u2 "没有检测到已授权的 Android 真机。"
    fi
    exit 1
  fi
  if [[ "$device_count" != "1" ]]; then
    print -u2 "检测到多台 Android 真机，请设置 ANDROID_DEVICE_ID 后重试。"
    exit 1
  fi
  device_id="$(print -r -- "$connected_devices" | head -n 1)"
fi

abi_list="$(adb -s "$device_id" shell getprop ro.product.cpu.abilist | tr -d '\r')"
if [[ "$abi_list" != *"arm64-v8a"* ]]; then
  print -u2 "当前设备不支持 AvatarKit 所需的 arm64-v8a：$abi_list"
  exit 1
fi

device_model="$(
  adb -s "$device_id" shell getprop ro.product.model |
    tr -d '\r'
)"
print "使用 Android 真机：$device_model"

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

adb -s "$device_id" reverse "tcp:$server_port" "tcp:$server_port"
reverse_added=1

print "启动 SpeakUp；按 q 退出，修改代码后按 r 热重载。"
cd "$mobile_dir"
flutter build apk \
  --debug \
  --target-platform android-arm64 \
  --dart-define="SPEAKUP_API_BASE_URL=$base_url" \
  "$@"
adb -s "$device_id" install \
  --no-streaming \
  -t \
  -r \
  "$mobile_dir/build/app/outputs/flutter-apk/app-debug.apk"
adb -s "$device_id" shell am start \
  -S \
  -n com.xengineer.speakup/.MainActivity
flutter attach -d "$device_id"
