# 仿 AstrBot 意图识别修改计划

> 状态：实施中，前四个阶段已完成。
>
> 进度维护：每完成并通过验收一个阶段，将该阶段标题前的 `[ ]` 改为 `[x]`，并在阶段末补充实际验收命令和结果。
>
> 已关联 Issue：[#203「冻结全量 LLM Tool Calling 契约与回归基线」](https://github.com/1024XEngineer/XE3-ESL/issues/203)、[#204「移除关键词业务路由并向模型暴露全量工具」](https://github.com/1024XEngineer/XE3-ESL/issues/204)、[#207「强化 Agent 工具描述、参数 Schema 与执行校验」](https://github.com/1024XEngineer/XE3-ESL/issues/207)、[#209「完成有界 Agent Tool Calling Loop」](https://github.com/1024XEngineer/XE3-ESL/issues/209)，均关联 MS2。

## 目标

参考 AstrBot 的 LLM Tool Calling 思路，将当前“关键词识别业务意图并指定工具”改为“服务端提供全部已注册工具的 Schema，由模型根据自然语言自主选择工具”。

目标链路：

```text
用户输入 + 对话上下文 + 全量工具 Schema
  -> LLM
  -> 无 tool_call：直接返回模型回复
  -> 有 tool_call：查找工具、校验参数并执行
  -> Tool Result 回填 LLM
  -> 继续调用或生成最终回复
```

本次不做用户级工具权限鉴定，也不按用户身份、关键词或业务意图裁剪工具列表。工具 Registry 是可调用工具的唯一来源。

保留的运行时约束不属于权限控制：

- 工具名必须存在于服务端 Registry；
- 调用参数必须通过 JSON Schema 校验，未知参数不得传入工具；
- Agent Loop 必须限制最大调用步数；
- 写工具需要防止模型重试造成重复写入；
- 工具调用、参数校验、执行结果和耗时需要可观测。

## 修改步骤

### [x] 1. 冻结新链路契约与回归基线

- 为现有直接回复、单工具调用、多轮工具调用、非法工具名、非法参数和循环超限补充测试基线。
- 明确 Provider 中立的 `ToolDefinition`、`ToolCall`、`ToolResult` 和消息回填结构。
- 确认 Registry 可以稳定输出全部已注册工具的名称、描述和输入 Schema。
- 不在本阶段改变线上路由行为。

验收：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai/...
```

实际结果（2026-07-29）：

- 确认 Provider 中立的 `ToolDefinition`、`ToolCall`、`TextMessage` Tool Result 回填和 `TextResult` 契约已经存在。
- 补充同一轮多个 Tool Call 及多个 Tool Result 完整回填的契约与 Runtime 测试。
- 补充未知工具、非法工具名和非法参数不会进入工具实现的回归测试。
- 强化 Registry 定义快照：全量定义保持稳定排序，并对输入 Schema 做深拷贝，调用方不能污染后续快照。
- 保留现有 `RouteIntent`、`ToolPolicyBuilder` 和线上工具选择行为，未提前实施第二阶段。

验收命令通过：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai/...
go test ./...
```

### [x] 2. 移除关键词业务意图路由和工具裁剪

- 删除 `RouteIntent`、业务关键词 signal、`PreferredTools` 和基于关键词生成 `ToolChoice` 的逻辑。
- 移除 `ToolPolicyBuilder` 对候选工具、写工具确认和用户身份权限的筛选职责。
- 每次模型请求直接使用 Registry 的全部工具定义，并统一设置为模型自主选择工具。
- 显式命令若仍需支持，只作为独立命令入口，不参与自然语言意图识别。

验收：

- 同一轮请求中，所有已注册工具都进入 Provider 请求。
- 未出现工具关键词的自然语言也可以由模型选择正确工具。
- 生产 Runtime 代码中不再存在业务意图关键词到工具名的映射。

实际结果（2026-07-29）：

- 删除生产 Runtime 中的 `IntentGuard`、`RouteIntent`、业务关键词 signals、`PreferredTools` 和 `ToolPolicyBuilder`。
- 自然语言请求统一读取 Registry 的全部工具定义，并固定使用 `ToolChoiceAuto`。
- 全部已注册工具均可执行，不再按用户、关键词、写操作确认或功能候选进行裁剪；Registry 白名单、参数校验和循环预算继续有效。
- 显式斜杠命令仍先执行其指定工具，执行结果回填后由模型基于全量工具继续组织回复。
- Context Manifest 保留原数据库字段以兼容既有契约，固定记录 `model_tool_selection`、`disabled` 和 `model-tool-routing-v1`。
- 离线评测不再调用生产关键词路由；其中的确定性规则仅作为可复现的假模型，并已用中文注释标明边界。
- 关键入口、全量工具暴露和显式命令分流已添加中文注释。

验收命令通过：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/eval ./internal/agent/tool ./internal/ai/...
go test ./...
```

### [x] 3. 强化工具描述与参数 Schema

- 为每个工具补全清晰的用途、适用场景和不适用场景描述，让工具定义承担“意图说明书”的职责。
- 为输入参数补全类型、必填项、枚举、长度和格式约束。
- Executor 在执行前校验工具名和参数，只把 Schema 声明过的字段传给工具。
- 统一未知工具、参数错误、业务执行失败和空结果的结构化错误语义，供模型继续处理。

验收：

- 非法工具名不会进入工具实现。
- 缺失必填参数、类型错误和未知参数都有确定性测试。
- 工具执行结果能作为结构化 Tool Result 正确回填。

实际结果（2026-07-29）：

- 完善 Scenario Create/Search、Review Search/Get、Material Search 和 Mistake Search 共 6 个工具的用途、适用场景及不适用场景描述。
- 新增非空文本、Agent ID、字符串枚举和整数范围 Schema 构造器，并使用中文注释说明约束用途。
- 统一 Schema 层支持必填项、基础类型、枚举、Unicode 字符长度、自定义格式、整数/数值上下界、数组和嵌套对象。
- 工具注册时同步校验 Schema 本身，拒绝未知类型、无效格式、空枚举、缺失属性和倒置范围。
- Executor 在调用工具前统一归一化参数，递归删除 Schema 未声明字段；非法参数不会进入工具实现。
- 保留稳定错误分类：`invalid_input`、`unknown_tool`、`permission_denied` 和 `internal`，并验证对应重试语义。
- 工具返回 nil Content 时归一为空对象，搜索无命中时继续返回结构化空数组。
- 离线提示注入评测与新参数过滤语义对齐：假模型负责拒绝提示注入，Executor 测试负责验证不可信字段不会进入工具。

验收命令通过：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/eval ./internal/agent/tool ./internal/agent/mocktool ./internal/matter/agenttool ./internal/review/agenttool ./internal/ai/...
go test ./...
```

### [x] 4. 完成有界 Tool Calling Loop

- Provider 返回普通文本时直接结束本轮。
- Provider 返回一个或多个 Tool Call 时，按 Registry 查找、Schema 校验和执行，并把结果追加到消息上下文。
- 支持模型根据 Tool Result 继续调用工具或生成最终回复。
- 设置单轮最大工具调用步数，超过上限时返回明确的降级结果。
- 为写工具传递稳定的幂等标识，避免同一 Tool Call 因重试重复落库。

验收：

- 覆盖“直接回复”“一次调用后回复”“连续调用后回复”“工具报错后回复”和“循环超限”。
- Agent Run 中能关联模型请求、Tool Call、Tool Result 和最终回复。

实际结果（2026-07-29）：

- 将自然语言和显式斜杠命令统一到同一个 Tool Calling Loop；显式命令只预先确定第一个工具，模型仍可根据结果继续调用其他工具。
- `MaxIterations` 明确限制工具执行轮数，达到上限后仍允许模型生成一次最终回复；同时继续限制工具调用总数、写工具调用数、单工具超时、整轮超时和 Tool Result 大小。
- 同一批工具调用在执行前统一检查调用总数、写操作数和重复 Tool Call ID，避免部分写入后才发现超限。
- 每个已接受的 Tool Call 都会回填对应 Tool Result；非法参数、未知工具和业务执行失败返回稳定的结构化错误分类与 `retryable` 标记，由模型继续追问、换工具或解释。
- 工具执行失败与调用记录持久化失败分开处理：前者回填模型继续循环，后者中断 Run，避免审计记录与真实执行状态不一致。
- 写工具使用稳定的 `run_id + tool_call_id` 作为 Request ID；同一 Run 重放同一 Tool Call 时由工具层幂等去重。
- Tool Call 记录继续通过 `run_id` 关联 Agent Run，并记录输入、状态、结果或错误分类、Request ID 和来源引用。
- 关键循环边界、错误回填和幂等规则均已添加中文注释。

验收命令通过：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai/...
go test ./...
```

### [ ] 5. 端到端联调、清理旧实现

- 使用 Review、Material、Mistake、Scenario 等现有工具进行真实 HTTP 链路联调。
- 验证模型可以在没有关键词枚举的情况下选择工具，也可以判断无需调用工具。
- 删除不再使用的路由类型、策略字段、日志字段、配置项和测试夹具。
- 更新测试断言，使其关注工具选择与执行结果，不再断言固定 `RouteIntent`。

验收：

```bash
cd server
go test ./...
```

至少记录以下联调样例的实际 Tool Call：

- 直接翻译或润色，不调用工具；
- 查询上次 Review；
- 结合简历或 JD 生成内容；
- 查询历史错题；
- 搜索或创建练习场景；
- 一句话连续触发两个有依赖关系的工具。

## 完成标准

- 自然语言意图不再由关键词枚举和 `RouteIntent` 决定；
- Provider 每轮可看到 Registry 中的全部工具 Schema；
- 模型可以自主选择直接回复、追问或调用工具；
- 工具调用经过名称检查、Schema 校验、步数限制和幂等保护；
- 全量测试和真实 HTTP 联调通过；
- 五个阶段均已标记为 `[x]`，并记录实际验收结果。
