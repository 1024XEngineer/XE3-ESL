package practice

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

type IdempotencyIntent struct {
	Method             string
	CanonicalPath      string
	Key                string
	PayloadFingerprint [sha256.Size]byte
}

type SessionStatus string

const (
	SessionStarting   SessionStatus = "starting"
	SessionInProgress SessionStatus = "in_progress"
	SessionPaused     SessionStatus = "paused"
	SessionCompleted  SessionStatus = "completed"
	SessionEndedEarly SessionStatus = "ended_early"
)

type Session struct {
	ID             string            `json:"practice_session_id"`
	PlanID         string            `json:"practice_plan_id"`
	PlanRevision   int               `json:"plan_revision"`
	SceneFamily    scene.SceneFamily `json:"scene_family"`
	SceneModel     scene.SceneModel  `json:"scene_model"`
	SnapshotID     string            `json:"snapshot_id"`
	Status         SessionStatus     `json:"practice_session_status"`
	Version        int               `json:"session_version"`
	EffectiveTurns int               `json:"-"`
	StartedAt      *time.Time        `json:"started_at,omitempty"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	EndReason      string            `json:"end_reason,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type Participant struct {
	ID               string                `json:"practice_participant_id"`
	SessionID        string                `json:"practice_session_id"`
	Role             string                `json:"participant_role"`
	SubjectRef       SubjectRef            `json:"subject_ref"`
	RoleDefinitionID string                `json:"role_definition_id,omitempty"`
	RoleSnapshot     *scene.RoleDefinition `json:"role_snapshot,omitempty"`
	Order            int                   `json:"participant_order"`
}

type SessionSnapshot struct {
	ID                 string                               `json:"snapshot_id"`
	SessionID          string                               `json:"practice_session_id"`
	PlanRevision       int                                  `json:"plan_revision"`
	SceneFamily        scene.SceneFamily                    `json:"scene_family"`
	SceneModel         scene.SceneModel                     `json:"scene_model"`
	SceneSelection     scene.SelectionSnapshot              `json:"scene_selection"`
	Preparation        preparation.Snapshot                 `json:"preparation_snapshot"`
	Participants       []Participant                        `json:"participants"`
	SessionPolicy      preparation.SessionPolicy            `json:"session_policy"`
	PracticeObjectives []preparation.PracticeObjective      `json:"practice_objectives"`
	IELTSAssignment    *preparation.IELTSAssignmentSnapshot `json:"ielts_assignment,omitempty"`
	CreatedAt          time.Time                            `json:"created_at"`
}

type SessionBootstrap struct {
	Session  Session         `json:"practice_session"`
	Snapshot SessionSnapshot `json:"snapshot"`
}

type CreateSessionCommand struct {
	SessionID    string
	SnapshotID   string
	PlanID       string
	PlanRevision int
	Snapshot     SessionSnapshot
	Intent       IdempotencyIntent
}

type SessionTransition string

const (
	SessionPause    SessionTransition = "pause"
	SessionResume   SessionTransition = "resume"
	SessionEndEarly SessionTransition = "end_early"
)

type TransitionSessionCommand struct {
	SessionID              string
	ExpectedSessionVersion int
	Transition             SessionTransition
	Intent                 IdempotencyIntent
}

// SessionRepository is Practice's complete Session persistence boundary.
// PracticePlan data is owned and read through Preparation's PlanReader.
type SessionRepository interface {
	ReplaySession(
		context.Context,
		Actor,
		IdempotencyIntent,
	) (SessionBootstrap, bool, error)
	CreateSession(
		context.Context,
		Actor,
		CreateSessionCommand,
	) (SessionBootstrap, bool, error)
	GetSession(context.Context, Actor, string) (Session, error)
	GetSessionSnapshot(
		context.Context,
		Actor,
		string,
	) (SessionSnapshot, error)
	ResolveSessionByPlan(
		context.Context,
		Actor,
		string,
	) (SessionBootstrap, error)
	TransitionSession(
		context.Context,
		Actor,
		TransitionSessionCommand,
	) (Session, bool, error)
	DeleteUserData(context.Context, DeletionContext) error
}

// VoiceSessionRepository exposes the same frozen Session authority to the
// production voice composition. This Port never reads a live Plan.
type VoiceSessionRepository interface {
	GetSession(context.Context, Actor, string) (Session, error)
	GetSessionSnapshot(
		context.Context,
		Actor,
		string,
	) (SessionSnapshot, error)
	ReplayVoiceStart(
		context.Context,
		Actor,
		IdempotencyIntent,
	) (SessionBootstrap, bool, error)
	ResolveSessionByPlan(
		context.Context,
		Actor,
		string,
	) (SessionBootstrap, error)
	ActivateSession(
		context.Context,
		Actor,
		string,
		string,
		IdempotencyIntent,
	) (SessionBootstrap, error)
	AdvanceTurn(
		context.Context,
		Actor,
		ConsumeTurnCommand,
	) (TurnResult, error)
}
