#!/bin/zsh

set -u

repo_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$repo_dir" || exit 1

./tools/agent-routing-benchmark/run_e2e.sh
status=$?

print
if [[ "$status" == "0" ]]; then
  print "Benchmark 全部通过。"
elif [[ "$status" == "2" ]]; then
  print "Benchmark 已生成报告，其中有用例未通过。"
else
  print "Benchmark 执行失败，请查看上方错误。"
fi
print "按回车键关闭窗口。"
read -r
exit "$status"
