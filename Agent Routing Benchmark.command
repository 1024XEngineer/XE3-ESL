#!/bin/zsh

set -u

repo_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$repo_dir" || exit 1

./tools/agent-routing-benchmark/run_e2e.sh
benchmark_exit_code=$?

print
if [[ "$benchmark_exit_code" == "0" ]]; then
  print "Benchmark 全部通过。"
elif [[ "$benchmark_exit_code" == "2" ]]; then
  print "Benchmark 已生成报告，其中有用例未通过。"
else
  print "Benchmark 执行失败，请查看上方错误。"
  print "按回车键关闭窗口。"
  read -r
fi
exit "$benchmark_exit_code"
