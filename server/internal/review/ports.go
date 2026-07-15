package review

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	// ErrTurnReviewSourceNotFound 表示 Conversation 中不存在指定 Turn。
	ErrTurnReviewSourceNotFound = errors.New("turn review source not found")
	// ErrTurnNotCompleted 表示 Turn 尚未完成，当前不能用于复盘。
	ErrTurnNotCompleted = errors.New("turn is not completed")
	// ErrTranscriptNotReady 表示 Turn 的 Transcript 尚不可用。
	ErrTranscriptNotReady = errors.New("transcript is not ready")
	// ErrAudioNotReady 表示 Turn 的音频引用或元数据尚不可用。
	ErrAudioNotReady = errors.New("audio is not ready")
	// ErrAudioNotFound 表示音频不存在或已被删除。
	ErrAudioNotFound = errors.New("audio not found")
	// ErrUnsupportedAudioFormat 表示当前 Review 评分链路不支持该音频格式。
	ErrUnsupportedAudioFormat = errors.New("unsupported audio format")
	// ErrAudioLimitExceeded 表示音频大小或时长超过评分限制。
	ErrAudioLimitExceeded = errors.New("audio limit exceeded")
	// ErrRetryNotAllowed 表示原 Turn 不允许创建同题重答。
	ErrRetryNotAllowed = errors.New("retry not allowed")
	// ErrRetryTurnCreationFailed 表示 Conversation 未能创建重答 Turn。
	ErrRetryTurnCreationFailed = errors.New("retry turn creation failed")
	// ErrEvaluationFailed 表示评分或证据生成失败。
	ErrEvaluationFailed = errors.New("evaluation failed")
)

// RetryReasonReview 是 Review 请求 Conversation 创建同题重答的稳定原因值。
const RetryReasonReview = "review_retry"

// TurnReviewSourceReader 是 Review 获取已完成 Turn 复盘资料的读取端口。
//
// 实现方应返回与 Conversation 内部可变状态脱离的快照，包括独立的
// TranscriptSegment 切片。该端口不传输音频内容；Review 后续使用
// AudioReference.AudioID 通过独立的音频读取端口打开只读流。
type TurnReviewSourceReader interface {
	GetCompletedTurnReviewSource(ctx context.Context, turnID string) (TurnReviewSource, error)
}

// TurnReviewSource 是 Review 评分和生成历史投影所需的最小只读快照。
// 它不是 Conversation 的领域对象，不得用于修改来源 Turn、
// Question、Transcript 或音频。
type TurnReviewSource struct {
	TurnID       string
	SessionID    string
	QuestionID   string
	QuestionText string
	Transcript   TranscriptSnapshot
	Audio        AudioReference
	CompletedAt  time.Time
}

// TranscriptSnapshot 是 Conversation 产生的 Transcript 只读快照。
// Status 保留 Conversation 公开的稳定状态值；Review 不在此复制其完整状态机。
type TranscriptSnapshot struct {
	TranscriptID string
	Text         string
	Language     string
	Status       string
	Segments     []TranscriptSegment
}

// TranscriptSegment 将 Transcript 中的必要文字片段定位到原始音频时间范围。
type TranscriptSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}

// AudioReference 是 Conversation 所拥有音频的稳定引用和最小元数据。
// AudioID 不是本地文件路径或对象存储位置；Checksum 为空表示来源未提供校验值。
type AudioReference struct {
	AudioID     string
	ContentType string
	Format      string
	SizeBytes   int64
	DurationMS  int64
	Checksum    string
}

// AudioContentReader 根据 Conversation 提供的稳定 AudioID 打开音频。
//
// 相同 AudioID 的每次成功调用都必须返回一个新的只读流，以支持
// 失败后重试。调用方 Review Service 拥有返回流的关闭责任。
type AudioContentReader interface {
	OpenAudio(ctx context.Context, audioID string) (io.ReadCloser, error)
}

// RetryTurnPort 请求 Conversation 为原 Turn 创建同题重答 Turn。
type RetryTurnPort interface {
	CreateRetryTurn(ctx context.Context, command CreateRetryTurnCommand) (RetryTurnResult, error)
}

// CreateRetryTurnCommand 是 Review 发送给 Conversation 的重答命令。
// RetryRequestID 是跨模块幂等键；Conversation 对相同键必须返回
// 同一 NewTurnID，不得创建多个 Turn。
type CreateRetryTurnCommand struct {
	RetryRequestID string
	OriginalTurnID string
	Reason         string
}

// RetryTurnResult 只返回 Conversation 所拥有的新 Turn 稳定 ID。
type RetryTurnResult struct {
	NewTurnID string
}

// TurnEvaluator 封装真实或 Mock 的评分和证据生成实现。
// Version 必须返回能区分评分行为的稳定版本，Review 使用
// TurnID + Version 保证 TurnAnalysis 幂等。
type TurnEvaluator interface {
	Version() string
	EvaluateTurn(ctx context.Context, input EvaluationInput) (EvaluationResult, error)
}

// EvaluationInput 是评分实现所需的最小输入。
// Audio 由 Review Service 打开和关闭，Evaluator 只负责读取。
type EvaluationInput struct {
	TurnID        string
	QuestionID    string
	QuestionText  string
	Transcript    TranscriptSnapshot
	Audio         io.Reader
	AudioMetadata AudioReference
}

// EvaluationResult 是与具体模型供应商无关的结构化评分结果。
type EvaluationResult struct {
	Score              int
	Summary            string
	AnalysisTranscript string
	Suggestions        []EvaluationSuggestion
}

// EvaluationSuggestion 描述一条可转换为 FeedbackItem 的建议和证据。
// Evidence 是必要的 Transcript 文字片段；StartMS 和 EndMS 用于
// 可选的音频时间定位。
type EvaluationSuggestion struct {
	Category  FeedbackCategory
	Message   string
	Evidence  string
	StartMS   *int64
	EndMS     *int64
	Retryable bool
}

// AnalysisRepository 持久化 Review 拥有的 TurnAnalysis。
// Save 必须维护 AnalysisKey 的唯一性，并能保存合法的状态转换。
type AnalysisRepository interface {
	FindByID(ctx context.Context, analysisID string) (TurnAnalysis, bool, error)
	FindByKey(ctx context.Context, key AnalysisKey) (TurnAnalysis, bool, error)
	Save(ctx context.Context, analysis TurnAnalysis) error
}

// FeedbackRepository 持久化和读取 Review 生成的 FeedbackItem。
// SaveAll 用于保存同一次分析产生的反馈集合。
type FeedbackRepository interface {
	FindByID(ctx context.Context, feedbackID string) (FeedbackItem, bool, error)
	ListByAnalysisID(ctx context.Context, analysisID string) ([]FeedbackItem, error)
	SaveAll(ctx context.Context, feedback []FeedbackItem) error
}

// RetryRepository 持久化 Review 拥有的 RetryRequest。
// FindByFeedbackID 用于保证对同一条反馈的重复请求保持幂等。
type RetryRepository interface {
	FindByID(ctx context.Context, retryRequestID string) (RetryRequest, bool, error)
	FindByFeedbackID(ctx context.Context, feedbackID string) (RetryRequest, bool, error)
	Save(ctx context.Context, request RetryRequest) error
	Update(ctx context.Context, request RetryRequest) error
}

// HistoryRepository 管理可重建的 HistoryRecord 只读投影。
// Upsert 使用 HistoryRecord.Key 保持幂等；ListBySession 必须按
// ReviewedAt 倒序返回，limit 和 offset 用于分页。
type HistoryRepository interface {
	Upsert(ctx context.Context, record HistoryRecord) error
	FindByAnalysisID(ctx context.Context, analysisID string) (HistoryRecord, bool, error)
	ListBySession(ctx context.Context, sessionID string, limit, offset int) ([]HistoryRecord, error)
}

// IDGenerator 为 Review 领域对象生成稳定唯一 ID。
// 这是团队统一实现出现前的模块内最小端口。
type IDGenerator interface {
	NewID() string
}

// Clock 为 Review Service 提供可测试的当前时间。
// 这是团队统一实现出现前的模块内最小端口。
type Clock interface {
	Now() time.Time
}
