#!/bin/zsh

script_dir="$(cd "$(dirname "$0")" && pwd)"
"$script_dir/tools/ios-simulator-dev/run.sh" frontend
status_code=$?
if [[ "$status_code" != "0" && -t 0 ]]; then
  print
  read -k 1 "?前端启动失败，按任意键关闭窗口..."
  print
fi
exit "$status_code"
