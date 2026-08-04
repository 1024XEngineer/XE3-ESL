package persistence

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

type ContextIdempotencyIntent struct {
	Method             string
	CanonicalPath      string
	Key                string
	PayloadFingerprint [sha256.Size]byte
}

type ContextSessionStatus string

const (
	ContextSessionStarting   ContextSessionStatus = "starting"
	ContextSessionProgress   ContextSessionStatus = "in_progress"
	ContextSessionPaused     ContextSessionStatus = "paused"
	ContextSessionCompleted  ContextSessionStatus = "completed"
	ContextSessionEndedEarly ContextSessionStatus = "ended_early"
)

type ContextSession struct {
	ID             string               `json:"practice_session_id"`
	PlanID         string               `json:"practice_plan_id"`
	PlanRevision   int                  `json:"plan_revision"`
	SceneFamily    scene.SceneFamily    `json:"scene_family"`
	SceneModel     scene.SceneModel     `json:"scene_model"`
	SnapshotID     string               `json:"snapshot_id"`
	Status         ContextSessionStatus `json:"practice_session_status"`
	Version        int                  `json:"session_version"`
	EffectiveTurns int                  `json:"-"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	EndedAt        *time.Time           `json:"ended_at,omitempty"`
	EndReason      string               `json:"end_reason,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
}

type ContextParticipant struct {
	ID               string                `json:"practice_participant_id"`
	SessionID        string                `json:"practice_session_id"`
	Role             string                `json:"participant_role"`
	SubjectRef       SubjectRef            `json:"subject_ref"`
	RoleDefinitionID string                `json:"role_definition_id,omitempty"`
	RoleSnapshot     *scene.RoleDefinition `json:"role_snapshot,omitempty"`
	Order            int                   `json:"participant_order"`
}

type ContextSessionSnapshot struct {
	ID                 string                               `json:"snapshot_id"`
	SessionID          string                               `json:"practice_session_id"`
	PlanRevision       int                                  `json:"plan_revision"`
	SceneFamily        scene.SceneFamily                    `json:"scene_family"`
	SceneModel         scene.SceneModel                     `json:"scene_model"`
	SceneSelection     scene.SelectionSnapshot              `json:"scene_selection"`
	Preparation        preparation.Snapshot                 `json:"preparation_snapshot"`
	Participants       []ContextParticipant                 `json:"participants"`
	SessionPolicy      preparation.SessionPolicy            `json:"session_policy"`
	PracticeObjectives []preparation.PracticeObjective      `json:"practice_objectives"`
	IELTSAssignment    *preparation.IELTSAssignmentSnapshot `json:"ielts_assignment,omitempty"`
	CreatedAt          time.Time                            `json:"created_at"`
}

type ContextSessionBootstrap struct {
	Session  ContextSession         `json:"practice_session"`
	Snapshot ContextSessionSnapshot `json:"snapshot"`
}

type CreateContextSessionCommand struct {
	SessionID    string
	SnapshotID   string
	PlanID       string
	PlanRevision int
	Snapshot     ContextSessionSnapshot
	Intent       ContextIdempotencyIntent
}

type ContextSessionTransition string

const (
	ContextSessionPause    ContextSessionTransition = "pause"
	ContextSessionResume   ContextSessionTransition = "resume"
	ContextSessionEndEarly ContextSessionTransition = "end_early"
)

type TransitionContextSessionCommand struct {
	SessionID              string
	ExpectedSessionVersion int
	Transition             ContextSessionTransition
	Intent                 ContextIdempotencyIntent
}

// SessionRepository is Practice's complete Session persistence boundary.
// PracticePlan data is owned and read through Preparation's PlanReader.
type SessionRepository interface {
	ReplayContextSession(
		context.Context,
		Actor,
		ContextIdempotencyIntent,
	) (ContextSessionBootstrap, bool, error)
	CreateContextSession(
		context.Context,
		Actor,
		CreateContextSessionCommand,
	) (ContextSessionBootstrap, bool, error)
	GetContextSession(context.Context, Actor, string) (ContextSession, error)
	GetContextSessionSnapshot(
		context.Context,
		Actor,
		string,
	) (ContextSessionSnapshot, error)
	ResolveContextSessionByPlan(
		context.Context,
		Actor,
		string,
	) (ContextSessionBootstrap, error)
	TransitionContextSession(
		context.Context,
		Actor,
		TransitionContextSessionCommand,
	) (ContextSession, bool, error)
	DeleteUserData(context.Context, DeletionContext) error
}

// ContextVoiceRepository exposes the same frozen Session authority to the
// production voice composition. This Port never reads a live Plan.
type ContextVoiceRepository interface {
	GetContextSession(context.Context, Actor, string) (ContextSession, error)
	GetContextSessionSnapshot(
		context.Context,
		Actor,
		string,
	) (ContextSessionSnapshot, error)
	ReplayContextVoiceStart(
		context.Context,
		Actor,
		ContextIdempotencyIntent,
	) (ContextSessionBootstrap, bool, error)
	ResolveContextSessionByPlan(
		context.Context,
		Actor,
		string,
	) (ContextSessionBootstrap, error)
	ActivateContextSession(
		context.Context,
		Actor,
		string,
		string,
		ContextIdempotencyIntent,
	) (ContextSessionBootstrap, error)
	AdvanceContextVoiceTurn(
		context.Context,
		Actor,
		ConsumeTurnCommand,
	) (TurnResult, error)
}
