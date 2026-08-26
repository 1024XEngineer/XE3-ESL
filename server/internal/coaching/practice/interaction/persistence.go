// This file defines the durable Practice Interaction persistence boundary. Concrete
// PostgreSQL mappings live in the postgres child package.
package interaction

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

var (
	ErrPersistenceInvalid     = errors.New("practice_interaction_persistence_invalid")
	ErrPersistenceNotFound    = errors.New("practice_interaction_persistence_not_found")
	ErrPersistenceConflict    = errors.New("practice_interaction_persistence_conflict")
	ErrPersistenceUnavailable = errors.New("practice_interaction_persistence_unavailable")
	ErrActorDeleted           = errors.New("practice_interaction_actor_deleted")
)

// Actor is the trusted identity presented to Practice Interaction by the application
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
	StoredTranscriptionConfirmed  StoredTranscriptionStatus = "confirmed"
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
	AudioAssetID            string
	CurrentAttemptID        string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type StoreReserveTranscriptionCommand struct {
	TurnID                  string
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

type ConfirmTurnCommand struct {
	CandidateID     string
	EvidenceVersion int64
	ConfirmedText   string
	IdempotencyKey  string
	// RetryTurnID is the Practice Interaction ANSWERING draft to confirm.
	// Empty means the ordinary EFFECTIVE Turn path.
	RetryTurnID string
}

// RecordingConfirmationStore creates or replays a Turn and binds its shared
// media recording in one transaction. A replay after explicit recording
// deletion returns the same Turn with an empty AudioAssetID.
type RecordingConfirmationStore interface {
	ConfirmTurnWithRecording(
		context.Context,
		Actor,
		ConfirmTurnCommand,
		string,
	) (practice.Turn, error)
}

// PersistenceStore is the Practice Interaction durable boundary. It returns only
// Practice Interaction data and never imports Review repositories.
type PersistenceStore interface {
	SaveQuestion(context.Context, Actor, practice.Question) (practice.Question, error)
	GetQuestion(context.Context, Actor, string) (practice.Question, error)
	ReserveTranscription(context.Context, Actor, StoreReserveTranscriptionCommand) (StoredTranscriptionReservation, error)
	AttachTranscriptionRecording(context.Context, Actor, string, string) error
	CompleteTranscription(context.Context, JobContext, StoreCompleteTranscriptionCommand) (StoredTranscriptCandidate, error)
	FailTranscription(context.Context, JobContext, ProcessingFailure) error
	GetReservation(context.Context, Actor, string) (StoredTranscriptionReservation, error)
	GetCandidate(context.Context, Actor, string) (StoredTranscriptCandidate, error)
	ConfirmTurn(context.Context, Actor, ConfirmTurnCommand) (practice.Turn, error)
	GetTurn(context.Context, Actor, string) (practice.Turn, error)
	ListSessionTurns(context.Context, Actor, string) ([]practice.Turn, error)
}

type QuestionTipStatus string

const (
	QuestionTipProcessing QuestionTipStatus = "processing"
	QuestionTipCompleted  QuestionTipStatus = "completed"
	QuestionTipFailed     QuestionTipStatus = "failed"
)

type QuestionTip struct {
	ID                 string
	SessionID          string
	QuestionID         string
	IdempotencyKey     string
	Status             QuestionTipStatus
	FencingToken       int64
	DeletionGeneration int64
	LeaseAcquired      bool
	LeaseExpiresAt     time.Time
	Content            string
	Translation        string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	CompletedAt        *time.Time
}

type ClaimQuestionTipCommand struct {
	SessionID      string
	QuestionID     string
	IdempotencyKey string
	LeaseDuration  time.Duration
}

type RenewQuestionTipLeaseCommand struct {
	TipID              string
	FencingToken       int64
	DeletionGeneration int64
	LeaseDuration      time.Duration
}

type CompleteQuestionTipCommand struct {
	TipID              string
	FencingToken       int64
	DeletionGeneration int64
	Content            string
	Translation        string
	Provider           string
	Model              string
	ProviderRequestID  string
}

type FailQuestionTipCommand struct {
	TipID              string
	FencingToken       int64
	DeletionGeneration int64
}

type QuestionTipStore interface {
	ClaimQuestionTip(
		context.Context,
		Actor,
		ClaimQuestionTipCommand,
	) (QuestionTip, error)
	RenewQuestionTipLease(
		context.Context,
		Actor,
		RenewQuestionTipLeaseCommand,
	) error
	GetQuestionTip(
		context.Context,
		Actor,
		string,
		string,
	) (QuestionTip, error)
	CompleteQuestionTip(
		context.Context,
		Actor,
		CompleteQuestionTipCommand,
	) (QuestionTip, error)
	FailQuestionTip(
		context.Context,
		Actor,
		FailQuestionTipCommand,
	) error
}
