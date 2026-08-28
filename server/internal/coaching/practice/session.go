package practice

import (
	"context"
	"crypto/sha256"
	"time"
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
	ID                  string             `json:"practice_session_id"`
	PlanID              string             `json:"practice_plan_id"`
	PlanVersion         int                `json:"plan_version"`
	Experience          PracticeExperience `json:"practice_experience"`
	Category            SceneCategory      `json:"scene_category"`
	PracticeMode        PracticeMode       `json:"practice_mode"`
	EvaluationPolicyRef string             `json:"evaluation_policy_ref"`
	Status              SessionStatus      `json:"practice_session_status"`
	Version             int                `json:"session_version"`
	EffectiveTurns      int                `json:"-"`
	StartedAt           *time.Time         `json:"started_at,omitempty"`
	EndedAt             *time.Time         `json:"ended_at,omitempty"`
	EndReason           string             `json:"end_reason,omitempty"`
	CreatedAt           time.Time          `json:"created_at"`
}

type Participant struct {
	ID               string          `json:"practice_participant_id"`
	SessionID        string          `json:"practice_session_id"`
	Role             string          `json:"participant_role"`
	SubjectRef       SubjectRef      `json:"subject_ref"`
	RoleDefinitionID string          `json:"role_definition_id,omitempty"`
	RoleSnapshot     *RoleDefinition `json:"role_snapshot,omitempty"`
	Order            int             `json:"participant_order"`
}

type SessionSnapshot struct {
	SessionID          string               `json:"practice_session_id"`
	PlanVersion        int                  `json:"plan_version"`
	Experience         PracticeExperience   `json:"practice_experience"`
	Category           SceneCategory        `json:"scene_category"`
	PracticeMode       PracticeMode         `json:"practice_mode"`
	SceneSelection     SceneSelection       `json:"scene_selection"`
	Preparation        PreparationSnapshot  `json:"preparation_snapshot"`
	Participants       []Participant        `json:"participants"`
	SessionPolicy      SessionPolicy        `json:"session_policy"`
	PracticeObjectives []PracticeObjective  `json:"practice_objectives"`
	IELTSAssignment    *IELTSAssignment     `json:"ielts_assignment,omitempty"`
	Presentation       PresentationSnapshot `json:"-"`
}

type SessionBootstrap struct {
	Session  Session         `json:"practice_session"`
	Snapshot SessionSnapshot `json:"snapshot"`
}

type CreateSessionCommand struct {
	SessionID          string
	PlanID             string
	PlanVersion        int
	Snapshot           SessionSnapshot
	ClientRequestID    string
	RequestFingerprint [sha256.Size]byte
}

type SessionTransition string

const (
	SessionPause    SessionTransition = "pause"
	SessionResume   SessionTransition = "resume"
	SessionComplete SessionTransition = "complete"
	SessionEndEarly SessionTransition = "end_early"
)

type TransitionSessionCommand struct {
	SessionID              string
	ExpectedSessionVersion int
	Transition             SessionTransition
	ClientRequestID        string
	RequestFingerprint     [sha256.Size]byte
}

// SessionRepository is Practice's complete Session persistence boundary.
// PracticePlan data is owned and read through Preparation's PlanReader.
type SessionRepository interface {
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
	TransitionSession(
		context.Context,
		Actor,
		TransitionSessionCommand,
	) (Session, bool, error)
}
