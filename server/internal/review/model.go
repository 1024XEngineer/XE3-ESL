package review

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidAnalysis      = errors.New("invalid turn analysis")
	ErrInvalidFeedback      = errors.New("invalid feedback item")
	ErrInvalidEvidence      = errors.New("invalid evidence")
	ErrInvalidRetryRequest  = errors.New("invalid retry request")
	ErrInvalidHistoryRecord = errors.New("invalid history record")
	ErrInvalidStateChange   = errors.New("invalid state change")
	ErrFeedbackNotRetryable = errors.New("feedback is not retryable")
)

// AnalysisStatus 表示一次分析的生命周期，不会改变来源 Turn 或
// PracticeSession 的生命周期。
type AnalysisStatus string

const (
	AnalysisStatusPending   AnalysisStatus = "pending"
	AnalysisStatusCompleted AnalysisStatus = "completed"
	AnalysisStatusFailed    AnalysisStatus = "failed"
)

// AnalysisKey 是保证评分幂等的稳定标识。
// Repository 不得为同一个 Key 创建两份分析。
type AnalysisKey struct {
	TurnID           string
	EvaluatorVersion string
}

// TurnAnalysis 是 Review 对一个已完成 Turn 的持久化分析结果。
// Score 使用指针，是因为 0 可能是有效分数，而 nil 表示尚未生成分数。
type TurnAnalysis struct {
	ID                 string
	TurnID             string
	EvaluatorVersion   string
	Status             AnalysisStatus
	Score              *int
	Summary            string
	AnalysisTranscript string
	FailureReason      string
	CreatedAt          time.Time
	CompletedAt        *time.Time
	FailedAt           *time.Time
}

// NewTurnAnalysis 创建一条待处理分析。
// 来源 Turn 仍归 Conversation 所有，Review 只通过稳定 ID 引用它。
func NewTurnAnalysis(id, turnID, evaluatorVersion string, createdAt time.Time) (TurnAnalysis, error) {
	analysis := TurnAnalysis{
		ID:               strings.TrimSpace(id),
		TurnID:           strings.TrimSpace(turnID),
		EvaluatorVersion: strings.TrimSpace(evaluatorVersion),
		Status:           AnalysisStatusPending,
		CreatedAt:        createdAt,
	}
	if err := analysis.Validate(); err != nil {
		return TurnAnalysis{}, err
	}
	return analysis, nil
}

// Key 返回该分析在 Repository 中使用的唯一键。
func (a TurnAnalysis) Key() AnalysisKey {
	return AnalysisKey{TurnID: a.TurnID, EvaluatorVersion: a.EvaluatorVersion}
}

// Complete 使用最终评分结果将待处理分析转换为已完成状态。
func (a *TurnAnalysis) Complete(score int, summary, analysisTranscript string, completedAt time.Time) error {
	if a == nil {
		return fmt.Errorf("%w: analysis is nil", ErrInvalidAnalysis)
	}
	if a.Status != AnalysisStatusPending {
		return fmt.Errorf("%w: analysis %q is %q", ErrInvalidStateChange, a.ID, a.Status)
	}
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("%w: completed analysis requires summary", ErrInvalidAnalysis)
	}
	if err := validateTerminalTime(a.CreatedAt, completedAt); err != nil {
		return fmt.Errorf("%w: completed_at: %v", ErrInvalidAnalysis, err)
	}

	candidate := *a
	candidate.Status = AnalysisStatusCompleted
	candidate.Score = intPointer(score)
	candidate.Summary = strings.TrimSpace(summary)
	candidate.AnalysisTranscript = analysisTranscript
	candidate.CompletedAt = timePointer(completedAt)
	candidate.FailureReason = ""
	candidate.FailedAt = nil
	if err := candidate.Validate(); err != nil {
		return err
	}

	*a = candidate
	return nil
}

// Fail 将待处理分析转换为失败状态，但不会修改来源 Turn。
func (a *TurnAnalysis) Fail(reason string, failedAt time.Time) error {
	if a == nil {
		return fmt.Errorf("%w: analysis is nil", ErrInvalidAnalysis)
	}
	if a.Status != AnalysisStatusPending {
		return fmt.Errorf("%w: analysis %q is %q", ErrInvalidStateChange, a.ID, a.Status)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: failed analysis requires reason", ErrInvalidAnalysis)
	}
	if err := validateTerminalTime(a.CreatedAt, failedAt); err != nil {
		return fmt.Errorf("%w: failed_at: %v", ErrInvalidAnalysis, err)
	}

	candidate := *a
	candidate.Status = AnalysisStatusFailed
	candidate.Score = nil
	candidate.Summary = ""
	candidate.FailureReason = strings.TrimSpace(reason)
	candidate.CompletedAt = nil
	candidate.FailedAt = timePointer(failedAt)
	if err := candidate.Validate(); err != nil {
		return err
	}

	*a = candidate
	return nil
}

// Validate 校验分析在各个持久化状态下必须满足的不变量。
func (a TurnAnalysis) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidAnalysis)
	}
	if strings.TrimSpace(a.TurnID) == "" {
		return fmt.Errorf("%w: turn_id is required", ErrInvalidAnalysis)
	}
	if strings.TrimSpace(a.EvaluatorVersion) == "" {
		return fmt.Errorf("%w: evaluator_version is required", ErrInvalidAnalysis)
	}
	if a.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidAnalysis)
	}

	switch a.Status {
	case AnalysisStatusPending:
		if a.Score != nil || strings.TrimSpace(a.Summary) != "" || strings.TrimSpace(a.FailureReason) != "" || a.CompletedAt != nil || a.FailedAt != nil {
			return fmt.Errorf("%w: pending analysis contains terminal fields", ErrInvalidAnalysis)
		}
	case AnalysisStatusCompleted:
		if a.Score == nil {
			return fmt.Errorf("%w: completed analysis requires score", ErrInvalidAnalysis)
		}
		if strings.TrimSpace(a.Summary) == "" {
			return fmt.Errorf("%w: completed analysis requires summary", ErrInvalidAnalysis)
		}
		if a.CompletedAt == nil {
			return fmt.Errorf("%w: completed analysis requires completed_at", ErrInvalidAnalysis)
		}
		if err := validateTerminalTime(a.CreatedAt, *a.CompletedAt); err != nil {
			return fmt.Errorf("%w: completed_at: %v", ErrInvalidAnalysis, err)
		}
		if strings.TrimSpace(a.FailureReason) != "" || a.FailedAt != nil {
			return fmt.Errorf("%w: completed analysis contains failure fields", ErrInvalidAnalysis)
		}
	case AnalysisStatusFailed:
		if strings.TrimSpace(a.FailureReason) == "" {
			return fmt.Errorf("%w: failed analysis requires reason", ErrInvalidAnalysis)
		}
		if a.FailedAt == nil {
			return fmt.Errorf("%w: failed analysis requires failed_at", ErrInvalidAnalysis)
		}
		if err := validateTerminalTime(a.CreatedAt, *a.FailedAt); err != nil {
			return fmt.Errorf("%w: failed_at: %v", ErrInvalidAnalysis, err)
		}
		if a.Score != nil || strings.TrimSpace(a.Summary) != "" || a.CompletedAt != nil {
			return fmt.Errorf("%w: failed analysis contains completion fields", ErrInvalidAnalysis)
		}
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalidAnalysis, a.Status)
	}

	return nil
}

// FeedbackCategory 是稳定的协议类型。
// 具体枚举值将在反馈契约中统一冻结，本模块不会提前猜测。
type FeedbackCategory string

// Evidence 指向用户的真实回答。TranscriptText 只保存必要的引用片段，
// 不能复制完整的来源 Transcript。StartMS 和 EndMS 必须同时存在或同时为空。
type Evidence struct {
	TranscriptText string
	StartMS        *int64
	EndMS          *int64
}

// Validate 校验证据能够通过文字、音频时间范围或二者共同定位。
func (e Evidence) Validate() error {
	hasText := strings.TrimSpace(e.TranscriptText) != ""
	hasStart := e.StartMS != nil
	hasEnd := e.EndMS != nil

	if !hasText && !hasStart && !hasEnd {
		return fmt.Errorf("%w: text or audio range is required", ErrInvalidEvidence)
	}
	if hasStart != hasEnd {
		return fmt.Errorf("%w: start_ms and end_ms must be provided together", ErrInvalidEvidence)
	}
	if hasStart {
		if *e.StartMS < 0 || *e.EndMS < 0 {
			return fmt.Errorf("%w: audio range cannot be negative", ErrInvalidEvidence)
		}
		if *e.EndMS < *e.StartMS {
			return fmt.Errorf("%w: end_ms cannot be before start_ms", ErrInvalidEvidence)
		}
	}
	return nil
}

// FeedbackItem 是根据 TurnAnalysis 生成的一条带证据反馈。
type FeedbackItem struct {
	ID         string
	AnalysisID string
	Category   FeedbackCategory
	Message    string
	Suggestion string
	Evidence   []Evidence
	Retryable  bool
	CreatedAt  time.Time
}

// NewFeedbackItem 创建反馈并复制证据切片，避免调用方后续追加元素时
// 意外改变反馈内部保存的证据集合。
func NewFeedbackItem(
	id, analysisID string,
	category FeedbackCategory,
	message, suggestion string,
	evidence []Evidence,
	retryable bool,
	createdAt time.Time,
) (FeedbackItem, error) {
	item := FeedbackItem{
		ID:         strings.TrimSpace(id),
		AnalysisID: strings.TrimSpace(analysisID),
		Category:   FeedbackCategory(strings.TrimSpace(string(category))),
		Message:    strings.TrimSpace(message),
		Suggestion: strings.TrimSpace(suggestion),
		Evidence:   append([]Evidence(nil), evidence...),
		Retryable:  retryable,
		CreatedAt:  createdAt,
	}
	if err := item.Validate(); err != nil {
		return FeedbackItem{}, err
	}
	return item, nil
}

// Validate 校验反馈已经关联分析，并且包含可定位的具体证据。
func (f FeedbackItem) Validate() error {
	if strings.TrimSpace(f.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidFeedback)
	}
	if strings.TrimSpace(f.AnalysisID) == "" {
		return fmt.Errorf("%w: analysis_id is required", ErrInvalidFeedback)
	}
	if strings.TrimSpace(string(f.Category)) == "" {
		return fmt.Errorf("%w: category is required", ErrInvalidFeedback)
	}
	if strings.TrimSpace(f.Message) == "" {
		return fmt.Errorf("%w: message is required", ErrInvalidFeedback)
	}
	if len(f.Evidence) == 0 {
		return fmt.Errorf("%w: evidence is required", ErrInvalidFeedback)
	}
	for i, evidence := range f.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("%w: evidence[%d]: %v", ErrInvalidFeedback, i, err)
		}
	}
	if f.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidFeedback)
	}
	return nil
}

// RetryStatus 表示 Review 中重答请求的状态。
// 它不表示新 Turn 的生命周期，新 Turn 仍归 Conversation 所有。
type RetryStatus string

const (
	RetryStatusPending     RetryStatus = "pending"
	RetryStatusTurnCreated RetryStatus = "turn_created"
	RetryStatusFailed      RetryStatus = "failed"
)

// RetryRequest 请求 Conversation 针对原问题创建一个新 Turn。
// ID 同时也是跨模块调用使用的幂等键。
type RetryRequest struct {
	ID             string
	OriginalTurnID string
	FeedbackID     string
	NewTurnID      string
	Status         RetryStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewRetryRequest 只允许根据可重答反馈创建待处理请求。
func NewRetryRequest(id, originalTurnID string, feedback FeedbackItem, createdAt time.Time) (RetryRequest, error) {
	if err := feedback.Validate(); err != nil {
		return RetryRequest{}, fmt.Errorf("%w: feedback: %v", ErrInvalidRetryRequest, err)
	}
	if !feedback.Retryable {
		return RetryRequest{}, ErrFeedbackNotRetryable
	}

	request := RetryRequest{
		ID:             strings.TrimSpace(id),
		OriginalTurnID: strings.TrimSpace(originalTurnID),
		FeedbackID:     feedback.ID,
		Status:         RetryStatusPending,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	if err := request.Validate(); err != nil {
		return RetryRequest{}, err
	}
	return request, nil
}

// MarkTurnCreated 记录 Conversation 创建的新 Turn 的稳定 ID。
func (r *RetryRequest) MarkTurnCreated(newTurnID string, updatedAt time.Time) error {
	if r == nil {
		return fmt.Errorf("%w: retry request is nil", ErrInvalidRetryRequest)
	}
	if r.Status != RetryStatusPending {
		return fmt.Errorf("%w: retry request %q is %q", ErrInvalidStateChange, r.ID, r.Status)
	}
	if strings.TrimSpace(newTurnID) == "" {
		return fmt.Errorf("%w: new_turn_id is required", ErrInvalidRetryRequest)
	}
	if err := validateUpdatedAt(r.CreatedAt, r.UpdatedAt, updatedAt); err != nil {
		return fmt.Errorf("%w: updated_at: %v", ErrInvalidRetryRequest, err)
	}

	candidate := *r
	candidate.Status = RetryStatusTurnCreated
	candidate.NewTurnID = strings.TrimSpace(newTurnID)
	candidate.FailureReason = ""
	candidate.UpdatedAt = updatedAt
	if err := candidate.Validate(); err != nil {
		return err
	}

	*r = candidate
	return nil
}

// MarkFailed 记录创建新 Turn 失败的结果。
func (r *RetryRequest) MarkFailed(reason string, updatedAt time.Time) error {
	if r == nil {
		return fmt.Errorf("%w: retry request is nil", ErrInvalidRetryRequest)
	}
	if r.Status != RetryStatusPending {
		return fmt.Errorf("%w: retry request %q is %q", ErrInvalidStateChange, r.ID, r.Status)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: failed retry requires reason", ErrInvalidRetryRequest)
	}
	if err := validateUpdatedAt(r.CreatedAt, r.UpdatedAt, updatedAt); err != nil {
		return fmt.Errorf("%w: updated_at: %v", ErrInvalidRetryRequest, err)
	}

	candidate := *r
	candidate.Status = RetryStatusFailed
	candidate.NewTurnID = ""
	candidate.FailureReason = strings.TrimSpace(reason)
	candidate.UpdatedAt = updatedAt
	if err := candidate.Validate(); err != nil {
		return err
	}

	*r = candidate
	return nil
}

// Validate 校验 RetryRequest 的状态和时间戳不变量。
func (r RetryRequest) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRetryRequest)
	}
	if strings.TrimSpace(r.OriginalTurnID) == "" {
		return fmt.Errorf("%w: original_turn_id is required", ErrInvalidRetryRequest)
	}
	if strings.TrimSpace(r.FeedbackID) == "" {
		return fmt.Errorf("%w: feedback_id is required", ErrInvalidRetryRequest)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: timestamps are required", ErrInvalidRetryRequest)
	}
	if r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("%w: updated_at cannot be before created_at", ErrInvalidRetryRequest)
	}

	switch r.Status {
	case RetryStatusPending:
		if strings.TrimSpace(r.NewTurnID) != "" || strings.TrimSpace(r.FailureReason) != "" {
			return fmt.Errorf("%w: pending retry contains terminal fields", ErrInvalidRetryRequest)
		}
	case RetryStatusTurnCreated:
		if strings.TrimSpace(r.NewTurnID) == "" {
			return fmt.Errorf("%w: created retry requires new_turn_id", ErrInvalidRetryRequest)
		}
		if strings.TrimSpace(r.FailureReason) != "" {
			return fmt.Errorf("%w: created retry contains failure reason", ErrInvalidRetryRequest)
		}
	case RetryStatusFailed:
		if strings.TrimSpace(r.FailureReason) == "" {
			return fmt.Errorf("%w: failed retry requires reason", ErrInvalidRetryRequest)
		}
		if strings.TrimSpace(r.NewTurnID) != "" {
			return fmt.Errorf("%w: failed retry contains new_turn_id", ErrInvalidRetryRequest)
		}
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalidRetryRequest, r.Status)
	}
	return nil
}

// HistoryKey 标识一条分析投影。History 可以通过来源 ID 重建，
// 不能成为第二个可写的权威数据源。
type HistoryKey struct {
	SessionID  string
	TurnID     string
	AnalysisID string
}

// HistoryRecord 只保存历史列表展示所需的最小字段。
type HistoryRecord struct {
	ID             string
	SessionID      string
	TurnID         string
	AnalysisID     string
	RetryRequestID string
	Score          *int
	Summary        string
	ReviewedAt     time.Time
}

// NewHistoryRecord 将一条已完成分析投影为历史只读模型。
func NewHistoryRecord(id, sessionID string, analysis TurnAnalysis, reviewedAt time.Time) (HistoryRecord, error) {
	if err := analysis.Validate(); err != nil {
		return HistoryRecord{}, fmt.Errorf("%w: analysis: %v", ErrInvalidHistoryRecord, err)
	}
	if analysis.Status != AnalysisStatusCompleted {
		return HistoryRecord{}, fmt.Errorf("%w: analysis must be completed", ErrInvalidHistoryRecord)
	}
	if reviewedAt.IsZero() || reviewedAt.Before(analysis.CreatedAt) {
		return HistoryRecord{}, fmt.Errorf("%w: invalid reviewed_at", ErrInvalidHistoryRecord)
	}

	record := HistoryRecord{
		ID:         strings.TrimSpace(id),
		SessionID:  strings.TrimSpace(sessionID),
		TurnID:     analysis.TurnID,
		AnalysisID: analysis.ID,
		Score:      intPointer(*analysis.Score),
		Summary:    analysis.Summary,
		ReviewedAt: reviewedAt,
	}
	if err := record.Validate(); err != nil {
		return HistoryRecord{}, err
	}
	return record, nil
}

// Key 返回用于幂等更新投影的稳定来源 ID。
func (h HistoryRecord) Key() HistoryKey {
	return HistoryKey{SessionID: h.SessionID, TurnID: h.TurnID, AnalysisID: h.AnalysisID}
}

// Validate 校验历史只读模型必须具备的最小字段。
func (h HistoryRecord) Validate() error {
	if strings.TrimSpace(h.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidHistoryRecord)
	}
	if strings.TrimSpace(h.SessionID) == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalidHistoryRecord)
	}
	if strings.TrimSpace(h.TurnID) == "" {
		return fmt.Errorf("%w: turn_id is required", ErrInvalidHistoryRecord)
	}
	if strings.TrimSpace(h.AnalysisID) == "" {
		return fmt.Errorf("%w: analysis_id is required", ErrInvalidHistoryRecord)
	}
	if h.Score == nil {
		return fmt.Errorf("%w: score is required", ErrInvalidHistoryRecord)
	}
	if strings.TrimSpace(h.Summary) == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalidHistoryRecord)
	}
	if h.ReviewedAt.IsZero() {
		return fmt.Errorf("%w: reviewed_at is required", ErrInvalidHistoryRecord)
	}
	return nil
}

func validateTerminalTime(createdAt, terminalAt time.Time) error {
	if terminalAt.IsZero() {
		return errors.New("timestamp is required")
	}
	if terminalAt.Before(createdAt) {
		return errors.New("timestamp cannot be before created_at")
	}
	return nil
}

func validateUpdatedAt(createdAt, currentUpdatedAt, nextUpdatedAt time.Time) error {
	if nextUpdatedAt.IsZero() {
		return errors.New("timestamp is required")
	}
	if nextUpdatedAt.Before(createdAt) || nextUpdatedAt.Before(currentUpdatedAt) {
		return errors.New("timestamp cannot move backwards")
	}
	return nil
}

func intPointer(value int) *int {
	return &value
}

func timePointer(value time.Time) *time.Time {
	return &value
}
