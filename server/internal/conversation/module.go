// Package conversation owns questions, turns, transcripts, and media capability ports.
package conversation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }

type Question struct {
	ID               string   `json:"question_id"`
	SessionID        string   `json:"practice_session_id"`
	SpeakerID        string   `json:"speaker_participant_id"`
	AddresseeIDs     []string `json:"addressee_participant_ids"`
	ObjectiveID      string   `json:"objective_id"`
	Type             string   `json:"question_type"`
	ParentQuestionID string   `json:"parent_question_id,omitempty"`
	Content          string   `json:"content"`
	Sequence         int      `json:"sequence"`
	CreatedAt        string   `json:"created_at"`
}

type Turn struct {
	ID              string `json:"turn_id"`
	SessionID       string `json:"practice_session_id"`
	QuestionID      string `json:"question_id"`
	RespondentID    string `json:"respondent_participant_id"`
	Sequence        int    `json:"sequence"`
	InteractionMode string `json:"interaction_mode,omitempty"`
	AnswerText      string `json:"answer_text,omitempty"`
	AudioAssetID    string `json:"audio_asset_id,omitempty"`
	Status          string `json:"turn_status"`
	IsRetry         bool   `json:"-"`
	SubmittedAt     string `json:"submitted_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	CompletedAt     string `json:"completed_at,omitempty"`
}

type Event struct {
	ID            string         `json:"event_id"`
	Type          string         `json:"event_type"`
	Version       int            `json:"event_version"`
	OccurredAt    string         `json:"occurred_at"`
	SessionID     string         `json:"practice_session_id"`
	Sequence      int            `json:"sequence,omitempty"`
	CorrelationID string         `json:"correlation_id"`
	CausationID   string         `json:"causation_id,omitempty"`
	Replayable    bool           `json:"replayable"`
	Payload       map[string]any `json:"payload"`
}

type SubmitTurnRequest struct {
	InteractionMode string `json:"interaction_mode"`
	AnswerText      string `json:"answer_text"`
	AudioAssetID    string `json:"audio_asset_id,omitempty"`
	RetryRequestID  string `json:"retry_request_id,omitempty"`
}

type QuestionDraft struct {
	ObjectiveID      string
	Type             string
	ParentQuestionID string
	Content          string
}

// QuestionProvider supplies generated question content without owning any
// Conversation resource or lifecycle state.
type QuestionProvider interface {
	BuildQuestion(int) (QuestionDraft, error)
}

// Backend is the state/provider-facing boundary owned by Conversation.
type Backend interface {
	Bootstrap(string) (map[string]any, error)
	CurrentQuestion(string) (Question, bool, error)
	SaveQuestion(string, int, QuestionDraft) (Question, error)
	PrepareTurn(string, SubmitTurnRequest) (Turn, error)
	CommitTurn(Turn) (Turn, error)
	CreateRetryTurn(string, string) (Turn, error)
	PublishProcessingFailure(string)
	PublishReviewCompleted(string, string, int, string)
	PublishSessionStarted(int)
	PublishSessionCompleted(int, string)
	GetQuestion(string) (Question, bool)
	GetTurn(string) (Turn, bool)
	Subscribe(string, int) ([]Event, <-chan Event, func(), error)
	StreamReady(string) (Event, error)
}

// Service is Conversation's formal application-service entry point.
type Service struct {
	backend  Backend
	provider QuestionProvider
}

func NewService(backend Backend, provider QuestionProvider) *Service {
	return &Service{backend: backend, provider: provider}
}

func (s *Service) Bootstrap(sessionID string) (map[string]any, error) {
	return s.backend.Bootstrap(sessionID)
}

func (s *Service) EnsureCurrentQuestion(sessionID string) (Question, error) {
	if question, found, err := s.backend.CurrentQuestion(sessionID); err != nil || found {
		return question, err
	}
	return s.CreateNextQuestion(sessionID, 1)
}

func (s *Service) CreateNextQuestion(
	sessionID string,
	sequence int,
) (Question, error) {
	draft, err := s.provider.BuildQuestion(sequence)
	if err != nil {
		return Question{}, err
	}
	return s.backend.SaveQuestion(sessionID, sequence, draft)
}

func (s *Service) PrepareTurn(
	questionID string,
	request SubmitTurnRequest,
) (Turn, error) {
	return s.backend.PrepareTurn(questionID, request)
}

func (s *Service) CommitTurn(turn Turn) (Turn, error) {
	return s.backend.CommitTurn(turn)
}

func (s *Service) CreateRetryTurn(retryID string, originalTurnID string) (Turn, error) {
	return s.backend.CreateRetryTurn(retryID, originalTurnID)
}

func (s *Service) PublishProcessingFailure(questionID string) {
	s.backend.PublishProcessingFailure(questionID)
}

func (s *Service) PublishReviewCompleted(
	analysisID string,
	turnID string,
	score int,
	summary string,
) {
	s.backend.PublishReviewCompleted(analysisID, turnID, score, summary)
}

func (s *Service) PublishSessionStarted(version int) {
	s.backend.PublishSessionStarted(version)
}

func (s *Service) PublishSessionCompleted(version int, reason string) {
	s.backend.PublishSessionCompleted(version, reason)
}

func (s *Service) GetQuestion(id string) (Question, bool) {
	return s.backend.GetQuestion(id)
}

func (s *Service) GetTurn(id string) (Turn, bool) {
	return s.backend.GetTurn(id)
}

func (s *Service) Subscribe(
	sessionID string,
	afterSequence int,
) ([]Event, <-chan Event, func(), error) {
	return s.backend.Subscribe(sessionID, afterSequence)
}

func (s *Service) StreamReady(sessionID string) (Event, error) {
	return s.backend.StreamReady(sessionID)
}
