package smoke

import "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"

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

// voiceQuestionProvider supplies generated question content to the
// deterministic smoke runtime.
type voiceQuestionProvider interface {
	BuildQuestion(int) (practice.QuestionDraft, error)
}

// voiceBackendPort isolates the deterministic smoke state implementation.
type voiceBackendPort interface {
	Bootstrap(string) (map[string]any, error)
	CurrentQuestion(string) (practice.Question, bool, error)
	SaveQuestion(string, int, practice.QuestionDraft) (practice.Question, error)
	PrepareTurn(string, SubmitTurnRequest) (practice.Turn, error)
	CommitTurn(practice.Turn) (practice.Turn, error)
	CreateRetryTurn(string, string) (practice.Turn, error)
	PublishProcessingFailure(string)
	PublishReviewCompleted(string, string, int, string)
	PublishSessionStarted(int)
	PublishSessionCompleted(int, string)
	GetQuestion(string) (practice.Question, bool)
	GetTurn(string) (practice.Turn, bool)
	Subscribe(string, int) ([]Event, <-chan Event, func(), error)
	StreamReady(string) (Event, error)
}

// voiceService drives the legacy API contract exercised by smoke tests.
type voiceService struct {
	backend  voiceBackendPort
	provider voiceQuestionProvider
}

func newVoiceService(backend voiceBackendPort, provider voiceQuestionProvider) *voiceService {
	return &voiceService{backend: backend, provider: provider}
}

func (s *voiceService) Bootstrap(sessionID string) (map[string]any, error) {
	return s.backend.Bootstrap(sessionID)
}

func (s *voiceService) EnsureCurrentQuestion(sessionID string) (practice.Question, error) {
	if question, found, err := s.backend.CurrentQuestion(sessionID); err != nil || found {
		return question, err
	}
	return s.CreateNextQuestion(sessionID, 1)
}

func (s *voiceService) CreateNextQuestion(
	sessionID string,
	sequence int,
) (practice.Question, error) {
	draft, err := s.provider.BuildQuestion(sequence)
	if err != nil {
		return practice.Question{}, err
	}
	return s.backend.SaveQuestion(sessionID, sequence, draft)
}

func (s *voiceService) PrepareTurn(
	questionID string,
	request SubmitTurnRequest,
) (practice.Turn, error) {
	return s.backend.PrepareTurn(questionID, request)
}

func (s *voiceService) CommitTurn(turn practice.Turn) (practice.Turn, error) {
	return s.backend.CommitTurn(turn)
}

func (s *voiceService) CreateRetryTurn(retryID string, originalTurnID string) (practice.Turn, error) {
	return s.backend.CreateRetryTurn(retryID, originalTurnID)
}

func (s *voiceService) PublishProcessingFailure(questionID string) {
	s.backend.PublishProcessingFailure(questionID)
}

func (s *voiceService) PublishReviewCompleted(
	analysisID string,
	turnID string,
	score int,
	summary string,
) {
	s.backend.PublishReviewCompleted(analysisID, turnID, score, summary)
}

func (s *voiceService) PublishSessionStarted(version int) {
	s.backend.PublishSessionStarted(version)
}

func (s *voiceService) PublishSessionCompleted(version int, reason string) {
	s.backend.PublishSessionCompleted(version, reason)
}

func (s *voiceService) GetQuestion(id string) (practice.Question, bool) {
	return s.backend.GetQuestion(id)
}

func (s *voiceService) GetTurn(id string) (practice.Turn, bool) {
	return s.backend.GetTurn(id)
}

func (s *voiceService) Subscribe(
	sessionID string,
	afterSequence int,
) ([]Event, <-chan Event, func(), error) {
	return s.backend.Subscribe(sessionID, afterSequence)
}

func (s *voiceService) StreamReady(sessionID string) (Event, error) {
	return s.backend.StreamReady(sessionID)
}
