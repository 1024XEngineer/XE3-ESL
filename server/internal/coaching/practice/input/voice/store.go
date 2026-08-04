// Package persistence defines Conversation's durable PostgreSQL boundary.
// The parent conversation package maps its application models to these types.
package voice

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

var (
	ErrPersistenceInvalid     = errors.New("conversation_persistence_invalid")
	ErrPersistenceNotFound    = errors.New("conversation_persistence_not_found")
	ErrPersistenceConflict    = errors.New("conversation_persistence_conflict")
	ErrPersistenceUnavailable = errors.New("conversation_persistence_unavailable")
	ErrActorDeleted           = errors.New("conversation_actor_deleted")
)

// Actor is the trusted identity presented to Conversation by the application
// layer. Delivery code must derive it from authenticated server context, never
// from a request body, path, provider response, or model output.
type Actor struct {
	UserID    string
	SessionID string
}

func (a Actor) Valid() bool {
	return a.UserID != "" && a.SessionID != ""
}

// JobContext is created from a persisted reservation by trusted worker
// orchestration. It is intentionally distinct from an online Actor because a
// provider callback can arrive after the originating Session was revoked.
type JobContext struct {
	OwnerUserID        string
	DeletionGeneration int64
	ReservationID      string
	FencingToken       int64
}

func (c JobContext) Valid() bool {
	return c.OwnerUserID != "" &&
		c.DeletionGeneration >= 0 &&
		c.ReservationID != "" &&
		c.FencingToken > 0
}

type StoredTranscriptionStatus string

const (
	StoredTranscriptionProcessing StoredTranscriptionStatus = "processing"
	StoredTranscriptionCompleted  StoredTranscriptionStatus = "completed"
	StoredTranscriptionFailed     StoredTranscriptionStatus = "failed"
)

type StoredTranscriptionReservation struct {
	ID               string
	QuestionID       string
	SessionID        string
	IdempotencyKey   string
	InputFingerprint string
	// RespondentParticipantID is resolved from the trusted Actor by the
	// application layer; it is never accepted from a client request body.
	RespondentParticipantID string
	Status                  StoredTranscriptionStatus
	FencingToken            int64
	DeletionGeneration      int64
	LeaseAcquired           bool
	LeaseExpiresAt          time.Time
	CandidateID             string
	CurrentAttemptID        string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type StoreReserveTranscriptionCommand struct {
	QuestionID              string
	SessionID               string
	IdempotencyKey          string
	InputFingerprint        string
	RespondentParticipantID string
	LeaseDuration           time.Duration
}

type CandidateStatus string

const (
	CandidateReady     CandidateStatus = "ready"
	CandidateConfirmed CandidateStatus = "confirmed"
)

type StoredTranscriptCandidate struct {
	ID                      string
	ReservationID           string
	QuestionID              string
	SessionID               string
	RespondentParticipantID string
	TranscriptID            string
	EvidenceVersion         int64
	Provider                string
	Model                   string
	ProviderRequestID       string
	Text                    string
	Status                  CandidateStatus
	CreatedAt               time.Time
}

type StoreCompleteTranscriptionCommand struct {
	TranscriptID      string
	EvidenceVersion   int64
	Provider          string
	Model             string
	ProviderRequestID string
	Text              string
}

// ProcessingFailure is deliberately normalized. It cannot carry provider
// error text, headers, credentials, or audio content into durable audit data.
type ProcessingFailure struct {
	Code              string
	Retryable         bool
	ProviderRequestID string
	Duration          time.Duration
}

type ProcessingAttempt struct {
	ID                string
	ReservationID     string
	Operation         string
	FencingToken      int64
	Status            string
	LeaseExpiresAt    time.Time
	ErrorCode         string
	Retryable         bool
	ProviderRequestID string
	Duration          time.Duration
	StartedAt         time.Time
	FinishedAt        *time.Time
}

type ConfirmTurnCommand struct {
	CandidateID     string
	EvidenceVersion int64
	ConfirmedText   string
	IdempotencyKey  string
	// RetryTurnID is the Conversation-owned ANSWERING draft to confirm.
	// Empty means the ordinary EFFECTIVE Turn path.
	RetryTurnID string
}

// RecordingConfirmationStore is the narrow Conversation-owned transaction
// boundary used when a confirmed Turn also owns a durable recording. The
// implementation must create or replay the Turn and bind the staged recording
// atomically. A previously bound recording that was later deleted replays the
// Turn with an empty AudioAssetID.
type RecordingConfirmation struct {
	Turn             practice.Turn
	AudioAssetID     string
	RecordingDeleted bool
}

type RecordingConfirmationStore interface {
	ConfirmTurnWithRecording(
		context.Context,
		Actor,
		ConfirmTurnCommand,
		string,
	) (RecordingConfirmation, error)
}

// PersistenceStore is the Conversation-owned durable boundary. It returns
// only Conversation data and never imports Practice or Review repositories.
type PersistenceStore interface {
	SaveQuestion(context.Context, Actor, practice.Question) (practice.Question, error)
	GetQuestion(context.Context, Actor, string) (practice.Question, error)
	ReserveTranscription(context.Context, Actor, StoreReserveTranscriptionCommand) (StoredTranscriptionReservation, error)
	CompleteTranscription(context.Context, JobContext, StoreCompleteTranscriptionCommand) (StoredTranscriptCandidate, error)
	FailTranscription(context.Context, JobContext, ProcessingFailure) error
	GetReservation(context.Context, Actor, string) (StoredTranscriptionReservation, error)
	GetCandidate(context.Context, Actor, string) (StoredTranscriptCandidate, error)
	ListProcessingAttempts(context.Context, Actor, string) ([]ProcessingAttempt, error)
	ConfirmTurn(context.Context, Actor, ConfirmTurnCommand) (practice.Turn, error)
	GetTurn(context.Context, Actor, string) (practice.Turn, error)
	ListSessionTurns(context.Context, Actor, string) ([]practice.Turn, error)
}
