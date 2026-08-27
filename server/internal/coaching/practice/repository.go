package practice

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument     = errors.New("practice: invalid argument")
	ErrNotFound            = errors.New("practice: not found")
	ErrConflict            = errors.New("practice: conflict")
	ErrIdempotencyConflict = errors.New("practice: idempotency conflict")
	ErrSessionCompleted    = errors.New("practice: session completed")
	ErrDeletionGeneration  = errors.New("practice: stale deletion generation")
)

// Actor is produced by the trusted authentication boundary. Repository
// methods intentionally have no client-supplied owner identifier.
type Actor struct {
	UserID    string
	SessionID string
}

// DeletionContext is issued by the trusted account-deletion coordinator after
// interactive sessions have been revoked. Generation identifies the current
// deletion attempt and must never come from an online client request.
type DeletionContext struct {
	UserID     string
	Generation uint64
}

type SubjectRef struct {
	Namespace string `json:"namespace"`
	SubjectID string `json:"subject_id"`
}

type ConsumeTurnCommand struct {
	SessionID             string
	TurnID                string
	CountsTowardTurnLimit bool
	// DeferEvaluation advances a durably recorded turn before asynchronous
	// transcription is available. A later idempotent consume schedules the
	// evaluation boundaries after the transcript has been confirmed.
	DeferEvaluation bool
	// Payload is used only to derive an idempotency fingerprint. Practice
	// never persists the transcript, audio, or provider request body.
	Payload []byte
}

type TurnResult struct {
	SessionID       string
	TurnID          string
	Round           int
	EffectiveTurns  int
	SessionVersion  int
	TurnLimit       int
	Completed       bool
	CompletionToken string
	CreatedAt       time.Time
}
