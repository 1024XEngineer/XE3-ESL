// Package conversation owns questions, turns, transcripts, and media capability ports.
package voice

import "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }

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

// QuestionProvider supplies generated question content without owning any
// Conversation resource or lifecycle state.
type QuestionProvider interface {
	BuildQuestion(int) (practice.QuestionDraft, error)
}

// Backend is the state/provider-facing boundary owned by Conversation.
type Backend interface {
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

func (s *Service) EnsureCurrentQuestion(sessionID string) (practice.Question, error) {
	if question, found, err := s.backend.CurrentQuestion(sessionID); err != nil || found {
		return question, err
	}
	return s.CreateNextQuestion(sessionID, 1)
}

func (s *Service) CreateNextQuestion(
	sessionID string,
	sequence int,
) (practice.Question, error) {
	draft, err := s.provider.BuildQuestion(sequence)
	if err != nil {
		return practice.Question{}, err
	}
	return s.backend.SaveQuestion(sessionID, sequence, draft)
}

func (s *Service) PrepareTurn(
	questionID string,
	request SubmitTurnRequest,
) (practice.Turn, error) {
	return s.backend.PrepareTurn(questionID, request)
}

func (s *Service) CommitTurn(turn practice.Turn) (practice.Turn, error) {
	return s.backend.CommitTurn(turn)
}

func (s *Service) CreateRetryTurn(retryID string, originalTurnID string) (practice.Turn, error) {
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

func (s *Service) GetQuestion(id string) (practice.Question, bool) {
	return s.backend.GetQuestion(id)
}

func (s *Service) GetTurn(id string) (practice.Turn, bool) {
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
