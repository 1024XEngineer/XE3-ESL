// Package review owns analysis, feedback, retries, and history projections.
package review

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "review" }

type Analysis struct {
	ID                 string `json:"turn_analysis_id"`
	TurnID             string `json:"turn_id"`
	EvaluatorVersion   string `json:"evaluator_version"`
	Status             string `json:"analysis_status"`
	Score              int    `json:"score"`
	Summary            string `json:"summary,omitempty"`
	AnalysisTranscript string `json:"analysis_transcript,omitempty"`
	CreatedAt          string `json:"created_at"`
	CompletedAt        string `json:"completed_at,omitempty"`
}

type Feedback struct {
	ID         string           `json:"feedback_item_id"`
	AnalysisID string           `json:"turn_analysis_id"`
	Category   string           `json:"feedback_category"`
	Message    string           `json:"message"`
	Suggestion string           `json:"suggestion"`
	Evidence   []map[string]any `json:"evidence"`
	Retryable  bool             `json:"retryable"`
	CreatedAt  string           `json:"created_at"`
}

type RetryRequest struct {
	ID             string `json:"retry_request_id"`
	OriginalTurnID string `json:"original_turn_id"`
	FeedbackID     string `json:"feedback_item_id"`
	NewTurnID      string `json:"new_turn_id,omitempty"`
	Status         string `json:"retry_status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	FailureReason  string `json:"failure_reason,omitempty"`
}

type HistoryRecord struct {
	ID             string `json:"history_record_id"`
	SessionID      string `json:"practice_session_id"`
	TurnID         string `json:"turn_id"`
	AnalysisID     string `json:"turn_analysis_id"`
	RetryRequestID string `json:"retry_request_id,omitempty"`
	Score          int    `json:"score"`
	Summary        string `json:"summary"`
	ReviewedAt     string `json:"reviewed_at"`
}

type Evaluation struct {
	Score      int
	Summary    string
	Transcript string
	Category   string
	Message    string
	Suggestion string
	Evidence   []map[string]any
}

type TurnInput struct {
	TurnID            string
	SessionID         string
	QuestionID        string
	AnswerText        string
	EffectiveSequence int
	CompletedAt       string
}

// Provider evaluates answer content but never owns Review resources.
type Provider interface {
	Evaluate(TurnInput) (Evaluation, error)
}

// Backend is the persistence/provider-facing boundary owned by Review.
type Backend interface {
	ListAnalyses(string) []Analysis
	ListFeedback(string) ([]Feedback, bool)
	SaveEvaluation(TurnInput, Evaluation) (Analysis, Feedback, bool, error)
	StartRetry(string) (RetryRequest, error)
	CompleteRetry(string, string) (RetryRequest, error)
	GetRetry(string) (RetryRequest, bool)
	ListHistory(string) []HistoryRecord
}

// Service is Review's formal application-service entry point.
type Service struct {
	backend  Backend
	provider Provider
}

func NewService(backend Backend, provider Provider) *Service {
	return &Service{backend: backend, provider: provider}
}

func (s *Service) ListAnalyses(turnID string) []Analysis {
	return s.backend.ListAnalyses(turnID)
}

func (s *Service) ListFeedback(analysisID string) ([]Feedback, bool) {
	return s.backend.ListFeedback(analysisID)
}

func (s *Service) Evaluate(
	turn TurnInput,
) (Analysis, Feedback, bool, error) {
	evaluation, err := s.provider.Evaluate(turn)
	if err != nil {
		return Analysis{}, Feedback{}, false, err
	}
	return s.backend.SaveEvaluation(turn, evaluation)
}

func (s *Service) StartRetry(feedbackID string) (RetryRequest, error) {
	return s.backend.StartRetry(feedbackID)
}

func (s *Service) CompleteRetry(
	retryID string,
	newTurnID string,
) (RetryRequest, error) {
	return s.backend.CompleteRetry(retryID, newTurnID)
}

func (s *Service) GetRetry(id string) (RetryRequest, bool) {
	return s.backend.GetRetry(id)
}

func (s *Service) ListHistory(sessionID string) []HistoryRecord {
	return s.backend.ListHistory(sessionID)
}
