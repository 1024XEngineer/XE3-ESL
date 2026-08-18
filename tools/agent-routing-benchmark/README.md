# Agent Routing Benchmark

该工具通过真实 HTTP 入口启动并测试 Agent。专用测试 Server 从
`server/test/agent` 显式注册固定工具数据，同时保留真实模型的工具选择过程；
生产 Server 不加载这些 Fixture。

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
  "expected_tools": ["review.search.v2"],
  "forbidden_tools": ["practice.preview.v2"],
  "required_response_terms": ["评价"],
  "forbidden_response_terms": ["report_id"],
  "max_non_empty_paragraphs": 2,
  "max_sentences": 2
}
```

调用 `practice.preview.v2` 的用例还应声明服务端真正收到的场景决议，例如：

```json
"expected_preview_input": {
  "kind": "CATALOG",
  "catalog_scene_id": "scn_travel_hotel_checkin"
}
```

Benchmark 只记录并比对 `kind`、`catalog_scene_id` 和
`candidate_scene_ids`；不会把 `scene_query`、用户消息或用户背景写入验收日志和
生成的 JSON、Markdown、HTML 报告。报告通过 case 名、`thread_id` 与 `run_id`
关联结果。

`messages` 可以包含多条用户消息。它们会在同一 Thread 中顺序发送，最后一条
消息对应的 Run 是评分目标。Run 完成后，Benchmark 会通过正式消息接口读取该
Run 持久化的 Assistant 回复。评分要求：

- 第一条 `agent.routing.decision` 与 `expected_decision` 一致；
- 目标 Run 的去重工具集合与 `expected_tools` 完全一致；
- 不调用 `forbidden_tools`；
- 每个 `agent.tool.call.started` 都有对应的 `succeeded`；
- 不重复调用同名工具；
- 配置了 `expected_preview_input` 时，恰好存在一条结构化 Preview 输入记录，
  且场景决议类型及 Catalog 场景 ID 完全匹配；
- 回复包含全部 `required_response_terms`，且不包含任何
  `forbidden_response_terms`（均按不区分大小写的子串匹配）；
- 回复不超过可选的 `max_non_empty_paragraphs` 和 `max_sentences`。

段落按空行分隔；句数按中英文句末标点统计，连续英文省略号不会被算作多个
句子。没有配置回复字段的既有用例只验收路由与工具执行。

修改提示词后直接重新运行，即可用相同用例比较总体准确率、决策准确率、工具
选择准确率、禁用工具安全率、工具执行成功率、Preview 输入契约通过率、回复契约
通过率和重复调用率。

## 自测

```bash
node --test tools/agent-routing-benchmark/lib/report.test.mjs
```
