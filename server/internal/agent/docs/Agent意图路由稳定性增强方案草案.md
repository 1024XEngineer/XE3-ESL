# Agent 意图路由稳定性增强方案（草案）

> 状态：实现前评审草案，正式工程决策以 GitHub Issue 为准。
>
> 关联 Issue：#183「强化 Agent 意图路由与工具调用稳定性」
>
> 目标 Milestone：MS2「MVP 深化与架构落实」
>
> 范围说明：本文只描述 Agent 意图路由、工具候选、模型 Tool Calling 控制和本地假数据联调增强；不实现 Review、Scenario、Material、Mistake 的真实业务逻辑。

## 1. 本地联调结论

当前正常模式下：

```text
AGENT_TOOL_MODE=real
AGENT_TOOL_FIXTURES=1
```

后端可以注册本地假数据工具，并通过前端同路径接口：

```text
POST /v1/agent-threads/:thread_id/runs
```

完成自然语言到 Agent Run 的联调。

已观察到的行为：

| 用户输入类型 | 当前路由表现 | 当前工具调用表现 |
| --- | --- | --- |
| 翻译、润色、表达建议 | 能识别为 direct language help | 不暴露工具，符合预期 |
| 查询 Review | 能暴露 `review.search.v1` / `review.get.v1` | 部分请求能正确调用 |
| 带“上次/之前/继续”的 Review | 容易退化为 `missing_required_context` | 候选工具变宽，稳定性下降 |
| 查询简历/JD/材料 | 能暴露并调用 `material.search.v1` | 空结果持久化可能导致 fallback |
| 查询错题 | 能暴露 `mistake.search.v1` | 模型可能直接回答，不调用工具 |
| 创建场景 | 能识别为新场景方向 | `scenario.create.v1` 因确认策略不暴露，模型直接回答 |

结论：

- 工具注册链路已生效；
- 当前主要问题不是“完全识别不了”，而是“强业务意图下模型是否实际调用工具不稳定”；
- 部分问题属于路由策略和工具选择控制，不应只依赖更换模型解决。

## 2. 外部开源方案参考

主流开源 Agent 和对话框架通常不会完全依赖模型自由选择工具，而是引入独立路由层或工作流控制层。

### 2.1 Rasa

Rasa 使用显式 Intent、Entity、Story、Rule 和 Action 组合。适合业务流程清晰、需要可控动作选择的对话系统。

借鉴点：

- 意图与动作分开；
- 多轮状态由 dialogue policy 管理；
- action 是受控执行单元，而不是模型随意生成的行为。

### 2.2 LangGraph

LangGraph 使用状态图和 conditional edges 控制工作流。常见做法是先用 router 节点判断状态，再把请求送到不同节点。

借鉴点：

- 路由结果应是显式状态；
- 不同意图可进入不同执行节点；
- 适合有线程、有上下文、有确认流程的 Agent。

### 2.3 Dify Question Classifier

Dify 在 workflow 中提供 Question Classifier 节点，用 LLM 或分类配置把用户输入分到不同路径。

借鉴点：

- 分类节点和最终回答节点分离；
- 分类结果用于控制 workflow，而不是仅作为日志。

### 2.4 LlamaIndex Router / Selector

LlamaIndex Router Query Engine 使用 selector 在多个 query engine 或 retriever 中选择执行对象。

借鉴点：

- Router 输出可结构化；
- 可用 LLM 输出 JSON 或 function calling 得到选择结果；
- 路由评测可以只断言 selector 结果，不断言最终回答文案。

### 2.5 Semantic Router

Semantic Router 使用 embedding 相似度做快速语义路由。

借鉴点：

- 对稳定高频意图可用低成本语义路由；
- 路由层可以独立于最终生成模型；
- 适合后续替换关键词 guard，但本 Issue 不强制引入 embedding 路由。

### 2.6 Haystack ConditionalRouter

Haystack 在 pipeline 中通过条件路由选择下一段处理流程。

借鉴点：

- 工程可控优先；
- 条件路由和模型生成解耦。

### 2.7 Semantic Kernel

Semantic Kernel 依赖函数调用能力连接插件和模型，同时通过插件描述、过滤器和执行器控制边界。

借鉴点：

- 使用模型原生 function calling；
- 但工具暴露、权限、执行仍由服务端控制。

## 3. 设计方向

现有设计方向保留：

```text
Intent Guard / Router
  + Tool Policy
  + Model Tool Calling
  + Tool Executor
```

本次增强重点是把“路由结果”和“工具调用期望”变成可观测、可测试、可约束的结构化决策。

不建议本阶段直接把千问替换为其他模型作为主解法。更强模型可能提升 tool calling 服从度，但无法解决：

- 写工具被策略拦截时模型根本看不到工具；
- “上次/之前”导致 reason 退化；
- 缺少 provider-neutral tool choice 约束；
- 空工具结果持久化失败。

## 4. 拟议改动

### 4.1 引入结构化 RouteDecision

当前 `IntentDecision` 主要包含：

```go
type IntentDecision struct {
    Mode         IntentMode
    ReasonCode   string
    GuardVersion string
}
```

建议演进为更结构化的路由结果：

```go
type RouteDecision struct {
    Intent              string
    Confidence          string
    HasContextReference bool
    ToolUseMode         string
    PreferredTools      []string
    ReasonCode          string
    RouterVersion       string
}
```

建议枚举：

```text
Intent:
  language_help
  historical_review
  material_context
  historical_mistake
  scenario_create
  scenario_search
  ambiguous

Confidence:
  high
  medium
  low

ToolUseMode:
  none
  auto
  required
  specific
  confirm_required
```

兼容策略：

- 第一阶段可以保留 `IntentDecision`，新增 adapter 把它转换为 `RouteDecision`；
- 或直接在 runtime 内部替换为 `RouteDecision`，HTTP 和持久化字段保持向后兼容；
- `ReasonCode` 继续保留，避免破坏现有日志和 manifest 结构。

### 4.2 调整意图判断顺序

当前风险点：

```text
hasContextReference(text) -> missing_required_context
```

这会让“上次 PM interview 的 review”这类请求优先退化为宽泛意图。

建议顺序：

```text
1. 识别业务域信号：
   review / mistake / material / scenario / language help

2. 标记上下文引用：
   上次 / 之前 / 刚才 / 继续 / previous / last time / continue

3. 组合生成 RouteDecision：
   intent + has_context_reference + tool_use_mode + preferred_tools
```

示例：

```text
输入：帮我找一下上次 PM interview 的 review

RouteDecision:
  intent = historical_review
  has_context_reference = true
  tool_use_mode = specific 或 required
  preferred_tools = ["review.search.v1"]
```

### 4.3 增加 provider-neutral ToolChoice

当前 `ai.TextRequest` 只有工具定义列表：

```go
type TextRequest struct {
    Messages []TextMessage
    Tools    []ToolDefinition
}
```

这只能表达“模型可以使用这些工具”，不能表达“这类意图必须先使用某个工具”。

建议新增：

```go
type ToolChoiceMode string

const (
    ToolChoiceAuto     ToolChoiceMode = "auto"
    ToolChoiceNone     ToolChoiceMode = "none"
    ToolChoiceRequired ToolChoiceMode = "required"
    ToolChoiceSpecific ToolChoiceMode = "specific"
)

type ToolChoice struct {
    Mode ToolChoiceMode
    Name string
}

type TextRequest struct {
    Messages   []TextMessage
    Tools      []ToolDefinition
    ToolChoice ToolChoice
}
```

映射规则：

| RouteDecision | ToolChoice |
| --- | --- |
| `language_help` | `none` |
| `historical_mistake` | `specific(mistake.search.v1)` |
| `material_context` | `specific(material.search.v1)` |
| `historical_review` | `specific(review.search.v1)` 或 `required` |
| `scenario_create` | 不直接给模型执行，进入 `confirm_required` |
| `ambiguous` | `auto` |

供应商适配要求：

- `internal/ai` 只定义中立契约；
- 千问 adapter 负责映射到供应商协议；
- 如果供应商原生支持 `tool_choice`，优先使用原生参数；
- 如果供应商不支持，保留中立字段并通过 prompt 降级，但不得把供应商私有结构暴露到 Agent Runtime。

### 4.4 强业务意图下约束工具调用

对以下意图，模型不应随意直接回答：

```text
historical_review
material_context
historical_mistake
```

建议策略：

- 工具可用且 policy 允许时，设置 `ToolChoiceSpecific`；
- 若模型仍未返回 tool call，可返回追问或服务端 fallback，而不是让模型编造历史数据；
- 后续可考虑由 runtime 直接执行单一明确 read tool，再把 tool result 回填给模型生成最终回复。

本 Issue 推荐先走 `ToolChoiceSpecific`，避免一次改动过大。

### 4.5 场景创建走确认状态

`scenario.create.v1` 是写工具，当前风险等级为 `RiskRequiresConfirm`，不应在自然语言首次请求时直接执行。

目标行为：

```text
用户：帮我创建一个英文后端面试练习场景

RouteDecision:
  intent = scenario_create
  tool_use_mode = confirm_required
  preferred_tools = ["scenario.create.v1"]

Runtime:
  不执行 scenario.create.v1
  生成确认型回复或记录待确认动作
```

后续用户确认后：

```text
ConfirmedActions includes scenario.create.v1
```

再允许执行写工具。

本 Issue 可先完成路由状态、日志和 manifest 表达，不强制完成完整 UI 确认流。

### 4.6 修复空 source refs 持久化

当前工具执行成功但 `SourceRefs` 为空时，运行时可能向 repository 传入 nil，导致数据库写入 `null`，与约束期望的数组形态冲突。

建议：

```go
func toolSourceRefs(refs []tool.SourceRef) []core.ToolSourceRef {
    result := make([]core.ToolSourceRef, 0, len(refs))
    for _, ref := range refs {
        result = append(result, core.ToolSourceRef{
            Type: ref.Type,
            ID:   ref.ID,
        })
    }
    return result
}
```

这样空结果会保存为 `[]`，不会因为工具无命中而误判工具执行失败。

## 5. 评测方案

### 5.1 确定性路由评测

不调用真实模型，只验证：

```text
input -> RouteDecision -> ToolPolicy -> exposed tools / tool choice
```

建议新增或扩展：

```text
server/internal/agent/runtime/intent_guard_test.go
server/internal/agent/runtime/tool_policy_test.go
server/internal/agent/eval/evaluator_test.go
```

### 5.2 模型联调评测

通过真实 HTTP 链路验证：

```text
POST /v1/agent-threads/:thread_id/runs
GET  /v1/agent-runs/:run_id/context-manifest
```

只断言：

- route decision；
- exposed tools；
- selected tool calls；
- final decision；
- 不断言最终回复文案。

### 5.3 建议用例

| 输入 | 期望 |
| --- | --- |
| 把“我负责后端稳定性”翻译成自然英文 | `language_help`，不暴露工具 |
| 帮我找一下上次 PM interview 的 review | `historical_review`，优先 `review.search.v1` |
| 结合我的简历和后端岗位 JD，帮我准备英文自我介绍 | `material_context`，优先 `material.search.v1` |
| 看一下我以前的错题，找 recurring mistakes | `historical_mistake`，优先 `mistake.search.v1` |
| 帮我创建一个英文后端面试练习场景 | `scenario_create`，`confirm_required`，不直接执行写工具 |

## 6. 实施步骤

建议拆为以下小步提交，保持每一步可测：

1. 修复空 `source_refs` 持久化；
2. 引入 `RouteDecision`，保留现有 `ReasonCode` 兼容；
3. 调整上下文引用与业务域识别顺序；
4. 增加 `ToolChoice` 中立契约和校验；
5. 在千问 adapter 中映射 `ToolChoice`；
6. 将 RouteDecision 接入 ToolPolicy 和 RunService；
7. 增加确定性路由评测；
8. 增加或记录 HTTP 联调验收脚本。

## 7. 修改计划

> 进度规则：每完成并验证一步，就把该步骤前的 `[ ]` 改为 `[x]`，并在步骤下补充实际验收命令或联调结果。

- [x] 修复工具空结果持久化问题

  目标：工具执行成功但没有 `source_refs` 时，持久化为空数组 `[]`，不能因为空结果导致 Agent Run fallback。

  涉及范围：

  ```text
  server/internal/agent/runtime/run_service.go
  server/internal/agent/runtime/*_test.go
  ```

  验收：

  ```bash
  cd server
  go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai ./internal/ai/qianwen
  ```

- [x] 引入结构化路由决策并调整意图判断顺序

  目标：把 `review / material / mistake / scenario / language_help` 等业务域先识别出来，再叠加“上次/之前/继续”等上下文引用信息，避免高价值意图退化为宽泛 `missing_required_context`。

  涉及范围：

  ```text
  server/internal/agent/runtime/intent_guard.go
  server/internal/agent/runtime/tool_policy.go
  server/internal/agent/runtime/*_test.go
  ```

  验收：

  ```bash
  cd server
  go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai ./internal/ai/qianwen
  ```

- [x] 增加 provider-neutral ToolChoice 并接入千问适配

  目标：让 Runtime 能表达 `none / auto / required / specific`，在强业务意图下要求模型优先调用指定工具，而不是只把工具列表交给模型自由选择。

  涉及范围：

  ```text
  server/internal/ai/text.go
  server/internal/ai/text_test.go
  server/internal/ai/qianwen/*
  server/internal/agent/runtime/run_service.go
  ```

  验收：

  ```bash
  cd server
  go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai ./internal/ai/qianwen
  ```

- [x] 接入场景创建确认型路由

  目标：自然语言创建场景时识别为 `scenario_create` 和 `confirm_required`，不在未确认时执行 `scenario.create.v1`；后续确认流可在独立 Issue 中继续完善。

  涉及范围：

  ```text
  server/internal/agent/runtime/intent_guard.go
  server/internal/agent/runtime/tool_policy.go
  server/internal/agent/runtime/run_service.go
  ```

  验收：

  ```bash
  cd server
  go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai ./internal/ai/qianwen
  ```

- [x] 补充路由评测与单元测试

  目标：不依赖最终回复文案，只验证 route decision、exposed tools、tool choice 和 selected tool call 的稳定性。

  覆盖用例：

  ```text
  翻译/润色 -> 不暴露工具
  上次 PM interview review -> review.search.v1
  简历 + JD -> material.search.v1
  以前的错题 -> mistake.search.v1
  创建后端面试场景 -> confirm_required，不直接执行写工具
  ```

  验收命令：

  ```bash
  cd server
  go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai ./internal/ai/qianwen
  ```

- [x] 完成 AI HTTP 前后端联调测试

  目标：用正常 server 模式模拟前端发消息，验证接口链路、路由日志、工具调用和 context manifest，而不是只测 Go 单元。

  启动方式：

  ```bash
  cd server
  set -a
  source ../.env
  set +a
  AGENT_TOOL_MODE=real AGENT_TOOL_FIXTURES=1 go run ./cmd/server
  ```

  HTTP 链路：

  ```text
  POST /v1/auth/register
  POST /v1/auth/login
  POST /v1/agent-threads
  POST /v1/agent-threads/:thread_id/runs
  GET  /v1/agent-runs/:run_id/context-manifest
  GET  /v1/agent-threads/:thread_id/messages
  ```

  联调输入：

  ```text
  帮我找一下上次 PM interview 的 review
  结合我的简历和后端岗位 JD，帮我准备英文自我介绍
  看一下我以前的错题，找 recurring mistakes
  帮我创建一个英文后端面试练习场景
  把“我负责后端稳定性”翻译成自然英文
  ```

  验收结果需记录：

  ```text
  route decision
  exposed tools
  selected tool calls
  final decision
  assistant message 是否符合预期边界
  ```

  验收：

  ```bash
  cd server
  SERVER_HOST=127.0.0.1 AGENT_TOOL_MODE=real AGENT_TOOL_FIXTURES=1 go run ./cmd/server
  ```

  HTTP 联调结果：

  ```text
  帮我找一下上次 PM interview 的 review
    route_intent=historical_review
    allowed_tools=["review.search.v1"]
    tool_choice=specific(review.search.v1)
    selected_tools=["review.search.v1"]
    final_decision=tool_call_then_response

  结合我的简历和后端岗位 JD，帮我准备英文自我介绍
    route_intent=material_context
    allowed_tools=["material.search.v1"]
    tool_choice=specific(material.search.v1)
    selected_tools=["material.search.v1","material.search.v1"]
    final_decision=tool_call_then_response

  看一下我以前的错题，找 recurring mistakes
    route_intent=historical_mistake
    allowed_tools=["mistake.search.v1"]
    tool_choice=specific(mistake.search.v1)
    selected_tools=["mistake.search.v1"]
    final_decision=tool_call_then_response

  帮我创建一个英文后端面试练习场景
    route_intent=scenario_create
    tool_use_mode=confirm_required
    allowed_tools=[]
    tool_choice=none
    selected_tools=null
    final_decision=direct_response

  把“我负责后端稳定性”翻译成自然英文
    route_intent=language_help
    allowed_tools=null
    tool_choice=none
    selected_tools=null
    final_decision=direct_response
  ```

  备注：千问兼容模式在返回工具调用时可能仍给 `finish_reason=stop`，已在 adapter 内归一为内部 `tool_calls` 并补充回归测试。

## 8. 验收命令

最小验收：

```bash
cd server
go test ./internal/agent/runtime ./internal/agent/tool ./internal/ai
```

本地联调：

```bash
cd server
set -a
source ../.env
set +a
AGENT_TOOL_MODE=real AGENT_TOOL_FIXTURES=1 go run ./cmd/server
```

HTTP 路径：

```text
POST /v1/auth/register
POST /v1/auth/login
POST /v1/agent-threads
POST /v1/agent-threads/:thread_id/runs
GET  /v1/agent-runs/:run_id/context-manifest
```

## 9. 风险与取舍

### 9.1 为什么不先换模型

更强模型可能提升 tool calling 服从度，但不能解决代码层问题：

- 写工具被 policy 拦截时，模型看不到工具；
- 上下文引用导致 reason 退化是本地 guard 逻辑问题；
- 没有 `ToolChoice` 时，模型可以选择直接回答；
- 空结果持久化失败与模型无关。

### 9.2 为什么不把所有业务意图都强制工具调用

部分请求可能信息不足，强制调用会导致无效查询或错误结果。因此建议：

- 高置信 read 意图强制或指定工具；
- 写意图进入确认；
- 低置信或多意图请求保留 auto 或 clarify。

### 9.3 为什么不引入 embedding semantic router

Embedding router 适合后续替代关键词 signal，但本 Issue 范围内优先修复已有链路，避免新增模型依赖和评测复杂度。

## 10. 最终期望

完成后，开发者应能稳定回答三个问题：

```text
1. 用户这句话被识别成什么业务意图？
2. 服务端允许模型看到哪些工具，并希望它如何选择？
3. 模型最终有没有调用正确工具？
```

如果工具未调用，也应能从日志或 manifest 判断原因是：

```text
direct language help
policy rejected
confirm required
tool unavailable
model ignored required tool
tool execution failed
```
