# Agent Routing Benchmark

该工具通过真实 HTTP 入口启动并测试本地 Agent 后端。它使用
`AGENT_TOOL_FIXTURES=1` 注册固定工具数据，但保留真实模型的工具选择过程。

## 双击运行

1. 确认仓库根目录的 `.env` 已配置文本生成模型和 `DATABASE_URL`，PostgreSQL
   容器正在运行。
2. 双击仓库根目录的 `Agent Routing Benchmark.command`。
3. 运行结束后会自动打开 `reports/latest.html`。
4. 认为本次结果值得保留时，点击报告中的“记录本次结果”并填写可选备注。
5. 查看或记录完成后，回到终端窗口按回车关闭本地报告服务。

每次运行会生成带时间戳的 Markdown、HTML、JSON 和服务日志，并更新
`latest.md`、`latest.html` 和 `latest.json`。报告目录已被 Git 忽略。
报告会记录当前 Git commit；工作区有未提交内容时会附加 `-dirty`。
普通运行不会进入折线图。只有手动记录的结果会写入
`reports/history/index.json`，重复记录同一份报告不会创建重复节点。

历史趋势只比较用例集指纹相同的报告。修改 `cases.json` 后会自动开始一组新的
趋势，避免不同测试集之间产生误导性的准确率比较。

每次运行会创建名称以 `xe3_benchmark_` 开头的隔离数据库，执行当前代码内嵌的
完整迁移，并在退出时删除。Benchmark 不读取或污染日常开发数据库中的用户、
Thread 和工具数据。

## 命令行运行

```bash
./tools/agent-routing-benchmark/run_e2e.sh
```

可选环境变量：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `BENCHMARK_PORT` | `18080` | benchmark 独占的本地后端端口 |
| `BENCHMARK_REPORT_PORT` | 自动分配 | 本地报告与历史记录服务端口 |
| `BENCHMARK_CASES_FILE` | `cases.json` | 使用另一份用例文件 |
| `BENCHMARK_REPORT_DIR` | `reports/` | 报告输出目录 |
| `BENCHMARK_OPEN_REPORT` | `1` | 设为 `0` 时不自动打开 HTML |

脚本只停止自己启动的后端进程。若目标端口已被占用，它会直接退出。
业务后端在测试结束后立即关闭；本地报告服务只在报告页面使用期间运行。
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
