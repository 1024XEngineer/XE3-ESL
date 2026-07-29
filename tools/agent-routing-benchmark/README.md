# Agent Routing Benchmark

该工具通过真实 HTTP 入口启动并测试本地 Agent 后端。它使用
`AGENT_TOOL_FIXTURES=1` 注册固定工具数据，但保留真实模型的工具选择过程。

## 双击运行

1. 确认仓库根目录的 `.env` 已配置文本生成模型，PostgreSQL 容器正在运行。
2. 双击仓库根目录的 `Agent Routing Benchmark.command`。
3. 运行结束后会自动打开 `reports/latest.html`。

每次运行会生成带时间戳的 Markdown、HTML、JSON 和服务日志，并更新
`latest.md`、`latest.html` 和 `latest.json`。报告目录已被 Git 忽略。
报告会记录当前 Git commit；工作区有未提交内容时会附加 `-dirty`。

## 命令行运行

```bash
./tools/agent-routing-benchmark/run_e2e.sh
```

可选环境变量：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `BENCHMARK_PORT` | `18080` | benchmark 独占的本地后端端口 |
| `BENCHMARK_CASES_FILE` | `cases.json` | 使用另一份用例文件 |
| `BENCHMARK_REPORT_DIR` | `reports/` | 报告输出目录 |
| `BENCHMARK_OPEN_REPORT` | `1` | 设为 `0` 时不自动打开 HTML |
| `BENCHMARK_RUN_MIGRATIONS` | `0` | 设为 `1` 时启动前执行迁移 |

脚本只停止自己启动的后端进程。若目标端口已被占用，它会直接退出。
全部用例通过时退出码为 `0`，报告生成但存在失败用例时为 `2`，基础设施或
执行错误时为 `1`。

## 用例格式

```json
{
  "name": "historical_review_search",
  "messages": ["看看我上次面试评价"],
  "expected_decision": "tool_call",
  "expected_tools": ["review.search.v1"],
  "forbidden_tools": ["scenario.create.v1"]
}
```

`messages` 可以包含多条用户消息。它们会在同一 Thread 中顺序发送，最后一条
消息对应的 Run 是评分目标。评分要求：

- 第一条 `agent.routing.decision` 与 `expected_decision` 一致；
- 目标 Run 的去重工具集合与 `expected_tools` 完全一致；
- 不调用 `forbidden_tools`；
- 每个 `agent.tool.call.started` 都有对应的 `succeeded`；
- 不重复调用同名工具。

修改提示词后直接重新运行，即可用相同用例比较总体准确率、决策准确率、工具
选择准确率、禁用工具安全率、工具执行成功率和重复调用率。

## 自测

```bash
node --test tools/agent-routing-benchmark/lib/report.test.mjs
```
