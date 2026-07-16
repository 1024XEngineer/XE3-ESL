# Review 模块跨模块接口需求

## 1. 文档目的

本文档用于说明 `review` 模块在实现 TurnAnalysis、证据反馈、同题重答、历史记录和 Session 级复盘报告时，需要其他业务模块提供的公开能力。

本文档只描述模块协作契约，不要求其他模块暴露内部领域模型、Repository、数据库表、本地文件路径或可变状态。最终接口名称和字段可以在联调时微调，但数据所有权和调用方向不应改变。

## 2. 已确认的职责分工

### Practice（成员 B）

- 拥有 `PracticeSession`、训练目标和 Session 生命周期。
- 向 Review 提供 Session 与训练目标的最小只读快照。
- 不负责评分、证据反馈和 Session 复盘报告生成。

### Conversation（成员 C）

- 拥有 `Question`、`Turn`、`Transcript`、原始语音和媒体处理状态。
- 向 Review 提供指定 Session 下已完成、可复盘的 Turn ID 列表。
- 向 Review 提供已经完成的 Turn 对应的题目信息、Transcript 只读快照和音频引用。
- 根据音频引用向 Review 提供只读音频流。
- 根据 Review 的重答请求创建新的同题 Turn。
- 不负责评分、评价、证据反馈和分析结果持久化。

### Review（成员 D）

- 获取已完成 Turn 的题目信息、Transcript 只读快照和音频数据。
- 选择评分实现路径：直接把音频提交给支持音频的模型/API，或者先将音频转写为文字，再调用语言模型。
- 创建并保存 `TurnAnalysis`。
- 根据评分结果创建并保存带证据的 `FeedbackItem`。
- 创建和管理 `RetryRequest`。
- 关联原 Turn 与同题重答产生的新 Turn。
- 建立和查询 History Read Model。
- 汇总 Session 下的 TurnAnalysis 和 Feedback，生成 Session 级复盘报告。

## 3. Review 模块边界

Review 不负责：

- 创建或修改原始 `Question`、`Turn` 和音频。
- 直接读取 Conversation 的文件目录、对象存储或数据库表。
- 把分析过程中生成的转写覆盖回 Conversation 的原始 Transcript。
- 直接创建同题重答产生的新 Turn。
- 修改 `PracticeSession` 的生命周期或训练进度。
- 修改场景、角色和背景快照。

## 4. 数据所有权

| 数据 | 唯一写入模块 | Review 的使用方式 |
|---|---|---|
| `Question`、`Turn`、`Transcript` | `conversation` | 通过公开接口读取最小只读快照 |
| 原始音频、音频媒体状态 | `conversation` | 先获取 `AudioID` 和元数据，再通过公开接口打开音频流 |
| 分析用临时转写 | `review` | 仅用于评分和证据生成，不覆盖 Conversation 数据 |
| `PracticeSession`、Session 进度 | `practice` | Review 不直接修改 |
| Session 与训练目标只读快照 | `practice` | Review 仅用于生成 Session 级复盘报告 |
| `TurnAnalysis`、`FeedbackItem` | `review` | Review 创建并保存 |
| `RetryRequest` | `review` | Review 创建并维护状态 |
| 同题重答产生的新 `Turn` | `conversation` | Review 只保存关联 ID |
| History Read Model | `review` | 只读投影，不作为源数据 |
| Session 级复盘只读视图 | `review` | Review 内部生成，只引用其他模块的稳定 ID；不新增跨模块公共核心对象 |

## 5. 依赖总览

```text
practice
  └── 提供 Session 与训练目标只读快照

conversation
  ├── 提供 Session 下已完成、可复盘的 Turn ID 列表
  ├── 提供已完成 Turn 的题目和 Transcript
  ├── 提供该 Turn 的 AudioID 与音频元数据
  ├── 根据 AudioID 打开只读音频流
  └── 根据 RetryRequest 创建同题重答 Turn
             ↓
review
  ├── 调用音频评分 API
  │        或
  ├── 音频转写 → 语言模型评分
  ├── TurnAnalysis
  ├── Evidence Feedback
  ├── RetryRequest
  ├── History Read Model
  └── Session Review Report
```

评分、评价和 Session 报告生成属于 Review，不要求 Practice 或 Conversation 提供评分接口。

## 6. 向 Practice 模块请求的能力

用途：Review 生成 Session 级报告时，需要知道本次练习的 Session 状态和训练目标，但不读取或修改 Practice 内部模型。

Review 不要求 Practice 新增一套专用的 Session 复盘公共对象或查询接口。D 复用 #34、#35 已冻结的公开能力：

```text
GetPracticeSessionSnapshot(practice_session_id) -> PracticeSessionSnapshot
```

Review 可在自己的 `ports.go` 中定义消费方窄 Port，由后续 Adapter 调用上述 Practice Service：

建议的 Review Port：

```go
type PracticeSessionSnapshotReader interface {
	GetPracticeSessionSnapshot(
		ctx context.Context,
		practiceSessionID string,
	) (PracticeSessionSnapshot, error)
}
```

`PracticeSessionSnapshot` 的字段以 #34、#35 的冻结契约为准。Review 只消费报告所需字段，不在本模块复制 Practice 的完整领域模型。

约束：

- 只返回生成复盘报告所需的最小只读数据。
- Review 不通过该接口修改 Session 状态、训练目标或主进度。
- Practice 提供“计划练什么”，Review 负责判断“完成得怎么样”。
- Session 不存在或当前不可生成报告时，需要返回可区分的错误。
- 不新增重复的 `SessionReviewContext` 公共对象；如需裁剪字段，由 Review Adapter 完成投影。

## 7. 向 Conversation 模块请求的能力

Review 复用 Conversation 已冻结的 `ListPracticeSessionTurns` 等公开 Service 能力。Review 可在自己的 `ports.go` 中定义更窄的消费方 Port，联调时由 Adapter 完成过滤和数据投影，不要求 Conversation 新增重复查询。

### 7.1 获取 Session 下可复盘的 Turn ID

用途：Review 从 `SessionID` 开始生成完整报告时，需要先获得该 Session 下已经完成且允许复盘的 Turn。

建议的 Review Port：

```go
type SessionReviewTurnReader interface {
	ListCompletedReviewTurnIDs(
		ctx context.Context,
		actorUserID string,
		practiceSessionID string,
	) ([]string, error)
}
```

后续 Adapter 调用 Conversation 的 `ListPracticeSessionTurns`，传入当前用户和 Practice Session 的稳定 ID，从结果中筛选已完成且可复盘的 Turn，并只向 Review 返回稳定 Turn ID。

约束：

- 只返回 Conversation 拥有的稳定 Turn ID，不返回可变 Turn 对象。
- 只包含已完成且允许复盘的 Turn。
- 返回顺序应稳定，建议按完成时间升序。
- Session 不存在或没有可复盘 Turn 时，需要返回可区分的错误或空结果语义。
- Review 获得 Turn ID 后，继续复用单 Turn 复盘接口读取详细资料。

### 7.2 获取已完成 Turn 的复盘数据

用途：Review 先获取评分所需的题目上下文、Transcript 只读快照、`AudioID` 和音频元数据；真正开始评分时，再根据 `AudioID` 打开音频流。

建议拆成两个 Review Port：

```go
type TurnReviewSourceReader interface {
	GetCompletedTurnReviewSource(
		ctx context.Context,
		turnID string,
	) (TurnReviewSource, error)
}

type AudioContentReader interface {
	OpenAudio(
		ctx context.Context,
		audioID string,
	) (io.ReadCloser, error)
}
```

建议的最小返回结构：

```go
type TurnReviewSource struct {
	TurnID       string
	SessionID    string
	QuestionID   string
	QuestionText string
	Transcript   TranscriptSnapshot
	Audio        AudioReference
	CompletedAt  time.Time
}

type AudioReference struct {
	AudioID     string
	ContentType string
	Format      string
	SizeBytes   int64
	DurationMS  int64
	Checksum    string
}

type TranscriptSnapshot struct {
	TranscriptID string
	Text         string
	Language     string
	Status       string
	Segments     []TranscriptSegment
}

type TranscriptSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}
```

字段说明：

- `QuestionText`：评分时需要知道用户在回答什么问题。
- `Transcript`：C 生成并拥有的对话文字只读快照；D 可以直接用于内容、语法和词汇评价。
- `Segments`：带时间范围的转写分段，便于 D 把文字证据定位到原始音频。
- `AudioID`：C 提供的稳定音频引用，不是对象存储路径。
- `ContentType`：例如 `audio/wav`、`audio/mpeg` 或 `audio/webm`。
- `Format`：明确容器或编码格式。
- `SizeBytes`：用于调用外部 API 前校验大小限制。
- `DurationMS`：用于超长音频校验、统计和超时设置。
- `Checksum`：可选，用于完整性检查和幂等缓存。

接口约束：

- 只能返回已完成、可复盘的 Turn。
- 获取复盘数据时不直接打开或传输大音频文件。
- Review 需要音频时，通过 `AudioContentReader.OpenAudio` 取得只读流，使用完成后必须关闭。
- 相同 `AudioID` 应支持重新调用 `OpenAudio`，以便外部评分失败后安全重试；每次调用返回新的流。
- Turn 不存在、Turn 未完成、Transcript 未就绪、音频未就绪和音频已删除应返回可区分的错误或状态。
- Conversation 不向 Review 暴露本地文件路径、存储桶凭证、Repository、ORM Model 或可变领域对象。
- Review 只能读取音频，不能通过该接口修改或删除原始音频。
- Review 只能读取 TranscriptSnapshot，不能覆盖 C 的原始 Transcript。
- 双方需要统一 MS1 支持的音频格式、大小上限和时长上限。
- D 不保存或持久化对象存储位置；对象存储迁移不应影响 Review。

### 7.3 创建同题重答 Turn

用途：Review 创建 `RetryRequest` 后，请求 Conversation 基于原 Question 创建新的 Turn。

建议的 Review Port：

```go
type RetryTurnPort interface {
	CreateRetryTurn(
		ctx context.Context,
		command CreateRetryTurnCommand,
	) (RetryTurnResult, error)
}
```

建议命令：

```go
type CreateRetryTurnCommand struct {
	RetryRequestID string
	OriginalTurnID string
	Reason         string
}
```

建议结果：

```go
type RetryTurnResult struct {
	NewTurnID string
}
```

约束：

- 新 Turn 必须复用原 Turn 对应的 Question。
- 新 Turn 由 Conversation 创建并拥有。
- Review 只保存 `RetryRequestID`、`OriginalTurnID` 和 `NewTurnID` 的关联。
- `RetryRequestID` 必须作为幂等键；相同请求重复调用不能创建多个新 Turn。
- 原 Turn 不存在、原 Turn 不可重答和创建失败需要返回可区分的错误。
- `Reason` 在 MS1 中建议使用稳定值 `review_retry`。

## 8. Review 内部评分与报告能力

评分、评价和证据生成属于 Review 内部能力，不要求 Conversation 实现。

Session 级报告也由 Review 生成：Review 获取 Practice 的 Session/目标快照和 Conversation 的 Turn ID 列表，再汇总自己的 TurnAnalysis、Feedback 和 History。该报告是 Review 内部的只读查询视图，不新增名为 `SessionReport` 的跨模块公共核心对象。Review 不复制或修改 PracticeSession 进度，也不复制完整 Conversation 对话历史。

Review 可在 `ports.go` 中定义统一的评分端口：

```go
type TurnEvaluator interface {
	EvaluateTurn(
		ctx context.Context,
		input EvaluationInput,
	) (EvaluationResult, error)
}
```

建议输入：

```go
type EvaluationInput struct {
	TurnID       string
	QuestionID   string
	QuestionText string
	Transcript   TranscriptSnapshot
	Audio        io.Reader
	AudioMetadata AudioReference
}
```

建议结果：

```go
type EvaluationResult struct {
	Score              int
	Summary            string
	AnalysisTranscript string
	Suggestions        []EvaluationSuggestion
}

type EvaluationSuggestion struct {
	Category    string
	Message     string
	Evidence    string
	StartMS     *int64
	EndMS       *int64
	Retryable   bool
}
```

内部边界要求：

- Review Service 只依赖 `TurnEvaluator`，不直接写死某家 API SDK。
- 真实 API 和 Mock 的实现通过 Port 注入。
- Review Service 通过 `AudioID` 打开音频流，并负责在评分结束后关闭该流；Evaluator 只消费 `io.Reader`。
- 优先使用 C 提供的 `TranscriptSnapshot`；只有 Transcript 不可用或需要重新转写时，D 才执行备用 ASR。
- `AnalysisTranscript` 是 D 备用转写产生的派生数据，不是 Conversation 原始 Transcript。
- 如果保存 `AnalysisTranscript`，它应作为 Analysis 的附属数据管理，并遵守隐私和删除规则。
- 证据优先使用音频时间范围 `StartMS`、`EndMS`；如果有分析用转写，也可以同时保存文字证据。
- 相同 Turn 和相同评分版本的重复调用应由 Review 幂等处理。

## 9. 错误和幂等语义

跨模块联调时至少需要区分以下错误：

| 场景 | 建议语义 |
|---|---|
| Session 不存在 | `session_not_found` |
| Session 当前不可生成报告 | `session_not_reviewable` |
| Session 没有可复盘 Turn | `session_has_no_reviewable_turns` |
| Turn 不存在 | `turn_not_found` |
| Turn 尚未完成 | `turn_not_completed` |
| Transcript 尚未就绪 | `turn_transcript_not_ready` |
| 音频尚未就绪 | `turn_audio_not_ready` |
| 音频已经删除 | `turn_audio_not_found` |
| 音频格式不支持 | `unsupported_audio_format` |
| 音频超过评分限制 | `audio_limit_exceeded` |
| 转写失败 | `transcription_failed` |
| 评分/评价失败 | `evaluation_failed` |
| 原 Turn 不允许重答 | `retry_not_allowed` |
| RetryRequest 已成功处理 | 返回原 `NewTurnID`，不重复创建 |
| 创建重答 Turn 失败 | `retry_turn_creation_failed` |

幂等要求：

- Review 使用 `turnID + 评分实现版本` 避免重复创建相同 Analysis。
- Conversation 使用 `RetryRequestID` 避免重复创建重答 Turn。
- History 使用稳定源 ID 更新投影，不能因重复调用产生重复记录。
- Session 级报告使用 `SessionID + 报告版本` 保持幂等，不因重复调用产生重复报告。

## 10. 联调验收条件

### Practice 与 Review

- Review 能复用 `GetPracticeSessionSnapshot`，根据 `PracticeSessionID` 获取 Session 状态和训练目标只读快照。
- Review 不能通过该能力修改 Session 生命周期、目标或训练进度。
- Session 不存在或不可生成报告时能够被明确识别。

### Conversation 与 Review

- Review 能通过 Adapter 复用 `ListPracticeSessionTurns`，根据 `PracticeSessionID` 获取已完成且可复盘的 Turn ID 列表。
- Review 能读取一个已完成 Turn 的题目信息、Transcript、`AudioID` 和音频元数据。
- Review 能根据 `AudioID` 打开只读音频流，并在使用后关闭。
- 相同 `AudioID` 可以重新打开新的音频流用于失败重试。
- 未完成 Turn、Transcript 未就绪或音频未就绪会被明确识别。
- Review 能识别 Content-Type、格式、大小和时长。
- Review 使用结束后能够正确关闭音频流。
- Review 能独立完成音频评分或“转写后评分”。
- Review 能把评分结果保存为自己的 Analysis 和 Feedback。
- Review 能发起同题重答并取得新的 Turn ID。
- 相同 `RetryRequestID` 重复调用只会得到同一个新 Turn。
- 两个模块之间没有跨模块 Repository、文件路径或存储凭证共享。

### Session 级报告

- Review 能使用 Session/目标快照和 Turn ID 列表生成自己的 Session 级复盘报告。
- 报告中的评价、目标达成判断和改进建议由 Review 负责。
- 报告只引用 Practice 和 Conversation 的稳定 ID，不成为 Session、Turn 或 Transcript 的第二份权威来源。
- 报告仅作为 Review 内部查询视图，不新增跨模块共享的 `SessionReport` 核心对象。
