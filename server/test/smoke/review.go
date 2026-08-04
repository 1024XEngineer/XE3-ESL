package smoke

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

type evaluationResult struct {
	Score      int
	Summary    string
	Transcript string
	Category   string
	Message    string
	Suggestion string
	Evidence   []map[string]any
}

type turnEvaluationInput struct {
	TurnID            string
	SessionID         string
	QuestionID        string
	AnswerText        string
	EffectiveSequence int
	CompletedAt       string
}

type reviewProvider interface {
	Evaluate(turnEvaluationInput) (evaluationResult, error)
}

type reviewStore interface {
	ListAnalyses(string) []Analysis
	ListFeedback(string) ([]Feedback, bool)
	SaveEvaluation(turnEvaluationInput, evaluationResult) (Analysis, Feedback, bool, error)
	StartRetry(string) (RetryRequest, error)
	CompleteRetry(string, string) (RetryRequest, error)
	GetRetry(string) (RetryRequest, bool)
	ListHistory(string) []HistoryRecord
}

type reviewService struct {
	store    reviewStore
	provider reviewProvider
}

func newReviewService(store reviewStore, provider reviewProvider) *reviewService {
	return &reviewService{store: store, provider: provider}
}

func (s *reviewService) ListAnalyses(turnID string) []Analysis {
	return s.store.ListAnalyses(turnID)
}

func (s *reviewService) ListFeedback(analysisID string) ([]Feedback, bool) {
	return s.store.ListFeedback(analysisID)
}

func (s *reviewService) Evaluate(
	turn turnEvaluationInput,
) (Analysis, Feedback, bool, error) {
	evaluation, err := s.provider.Evaluate(turn)
	if err != nil {
		return Analysis{}, Feedback{}, false, err
	}
	return s.store.SaveEvaluation(turn, evaluation)
}

func (s *reviewService) StartRetry(feedbackID string) (RetryRequest, error) {
	return s.store.StartRetry(feedbackID)
}

func (s *reviewService) CompleteRetry(
	retryID string,
	newTurnID string,
) (RetryRequest, error) {
	return s.store.CompleteRetry(retryID, newTurnID)
}

func (s *reviewService) GetRetry(id string) (RetryRequest, bool) {
	return s.store.GetRetry(id)
}

func (s *reviewService) ListHistory(sessionID string) []HistoryRecord {
	return s.store.ListHistory(sessionID)
}
