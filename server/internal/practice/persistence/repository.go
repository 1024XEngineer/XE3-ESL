// Package persistence defines the Practice-owned storage DTOs and narrow Port
// used by persistence adapters. The parent practice package remains the
// authority for the formal domain contract and maps to this boundary.
package persistence

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidArgument       = errors.New("practice: invalid argument")
	ErrNotFound              = errors.New("practice: not found")
	ErrConflict              = errors.New("practice: conflict")
	ErrActiveSessionConflict = activeSessionConflictError{}
	ErrIdempotencyConflict   = errors.New("practice: idempotency conflict")
	ErrConfirmationRequired  = errors.New("practice: confirmation required")
	ErrSessionCompleted      = errors.New("practice: session completed")
	ErrDeletionGeneration    = errors.New("practice: stale deletion generation")
)

type activeSessionConflictError struct{}

func (activeSessionConflictError) Error() string {
	return "practice: active session conflict"
}

func (activeSessionConflictError) Is(target error) bool {
	return target == ErrConflict
}

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

type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusCompleted SessionStatus = "completed"
)

type SubjectRef struct {
	Namespace string `json:"namespace"`
	SubjectID string `json:"subject_id"`
}

type ParticipantSnapshot struct {
	ParticipantID   string         `json:"participant_id"`
	ParticipantRole string         `json:"participant_role"`
	SubjectRef      SubjectRef     `json:"subject_ref"`
	RoleDefinition  map[string]any `json:"role_definition,omitempty"`
	Order           int            `json:"order"`
}

// SessionSnapshot is frozen when its Session is created. TurnLimit is a
// per-session policy value; the repository does not hard-code three turns.
type SessionSnapshot struct {
	Mode         string                `json:"mode"`
	TargetIDs    []string              `json:"target_ids"`
	Participants []ParticipantSnapshot `json:"participants"`
	TurnLimit    int                   `json:"turn_limit"`
}

type CreateSessionCommand struct {
	SessionID string
	PlanID    string
	Snapshot  SessionSnapshot
}

type Session struct {
	ID             string
	OwnerUserID    string
	PlanID         string
	Status         SessionStatus
	Version        int
	EffectiveTurns int
	Snapshot       SessionSnapshot
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      time.Time
	CompletedAt    *time.Time
}

type ConsumeTurnCommand struct {
	SessionID             string
	TurnID                string
	CountsTowardTurnLimit bool
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

// Repository is the narrow persistence boundary required by the PostgreSQL
// adapter. Lifecycle and next-action policy remain outside this contract.
type Repository interface {
	CreateSession(context.Context, Actor, CreateSessionCommand) (Session, error)
	GetSession(context.Context, Actor, string) (Session, error)
	ListSessions(context.Context, Actor) ([]Session, error)
	ConsumeTurn(context.Context, Actor, ConsumeTurnCommand) (TurnResult, error)
	DeleteSession(context.Context, Actor, string) error
	DeleteUserData(context.Context, DeletionContext) error
}
