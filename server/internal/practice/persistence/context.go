package persistence

import (
	"context"
	"crypto/sha256"
	"time"
)

type PlanStatus string

const (
	PlanStatusConfiguring         PlanStatus = "configuring"
	PlanStatusConfigurationFailed PlanStatus = "configuration_failed"
	PlanStatusReady               PlanStatus = "ready"
	PlanStatusArchived            PlanStatus = "archived"
)

type Plan struct {
	ID                        string     `json:"practice_plan_id"`
	UserID                    string     `json:"user_id"`
	AgentThreadID             string     `json:"agent_thread_id"`
	MatterID                  string     `json:"matter_id"`
	ScenarioDefinitionID      string     `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int        `json:"scenario_definition_version"`
	ScenarioType              string     `json:"scenario_type"`
	ScenarioConfigID          string     `json:"scenario_config_id"`
	ScenarioConfigVersion     int        `json:"scenario_config_version"`
	PreparationProfileID      string     `json:"preparation_profile_id"`
	SelectedRoleIDs           []string   `json:"selected_role_ids"`
	Revision                  int        `json:"plan_revision"`
	Status                    PlanStatus `json:"practice_plan_status"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type ContextIdempotencyIntent struct {
	Method             string
	CanonicalPath      string
	Key                string
	PayloadFingerprint [sha256.Size]byte
}

type CreatePlanCommand struct {
	PlanID                    string
	AgentThreadID             string
	MatterID                  string
	ScenarioDefinitionID      string
	ScenarioDefinitionVersion int
	ScenarioType              string
	ScenarioConfigID          string
	ScenarioConfigVersion     int
	PreparationProfileID      string
	SelectedRoleIDs           []string
	Intent                    ContextIdempotencyIntent
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
	ID           string               `json:"practice_session_id"`
	PlanID       string               `json:"practice_plan_id"`
	ScenarioType string               `json:"scenario_type"`
	SnapshotID   string               `json:"snapshot_id"`
	Status       ContextSessionStatus `json:"practice_session_status"`
	Version      int                  `json:"session_version"`
	StartedAt    *time.Time           `json:"started_at,omitempty"`
	EndedAt      *time.Time           `json:"ended_at,omitempty"`
	EndReason    string               `json:"end_reason,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
}

type ScenarioDefinitionSnapshot struct {
	ID      string `json:"scenario_definition_id"`
	Type    string `json:"scenario_type"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Status  string `json:"status"`
}

type ScenarioConfigSnapshot struct {
	ID                   string   `json:"scenario_config_id"`
	ScenarioDefinitionID string   `json:"scenario_definition_id"`
	Type                 string   `json:"config_type"`
	Version              int      `json:"version"`
	JobTitle             string   `json:"job_title"`
	JobDescription       string   `json:"job_description"`
	FocusAreas           []string `json:"focus_areas"`
}

type PreparationSnapshot struct {
	ID                     string    `json:"preparation_snapshot_id"`
	SourceProfileID        string    `json:"source_profile_id"`
	SourceVersion          int       `json:"source_version"`
	ResumeSnapshot         string    `json:"resume_snapshot,omitempty"`
	JobDescriptionSnapshot string    `json:"job_description_snapshot,omitempty"`
	BackgroundSnapshot     string    `json:"background_snapshot"`
	CreatedAt              time.Time `json:"created_at"`
}

type RoleSnapshot struct {
	ID                   string   `json:"role_definition_id"`
	ScenarioDefinitionID string   `json:"scenario_definition_id"`
	Type                 string   `json:"role_type"`
	DisplayName          string   `json:"display_name"`
	Responsibilities     string   `json:"responsibilities"`
	Style                string   `json:"style"`
	FocusAreas           []string `json:"focus_areas"`
	VoiceConfigRef       string   `json:"voice_config_ref,omitempty"`
	Version              int      `json:"version"`
}

type PracticeOptionSnapshot struct {
	ID                   string `json:"practice_option_id"`
	ScenarioDefinitionID string `json:"scenario_definition_id"`
	RoleDefinitionID     string `json:"role_definition_id,omitempty"`
	Type                 string `json:"practice_option_type"`
	DisplayName          string `json:"display_name"`
	Version              int    `json:"version"`
}

type ContextParticipant struct {
	ID               string        `json:"practice_participant_id"`
	SessionID        string        `json:"practice_session_id"`
	Role             string        `json:"participant_role"`
	SubjectRef       SubjectRef    `json:"subject_ref"`
	RoleDefinitionID string        `json:"role_definition_id,omitempty"`
	RoleSnapshot     *RoleSnapshot `json:"role_snapshot,omitempty"`
	Order            int           `json:"participant_order"`
}

type PracticeObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type ContextSessionPolicy struct {
	SuggestedDurationSeconds int                 `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int                 `json:"min_effective_turns"`
	MaxEffectiveTurns        int                 `json:"max_effective_turns"`
	CoverageCheckpointTurn   int                 `json:"coverage_checkpoint_turn"`
	MaxFollowUpsPerQuestion  int                 `json:"max_follow_ups_per_question"`
	TargetObjectives         []PracticeObjective `json:"target_objectives"`
	EarlyCompletionRule      string              `json:"early_completion_rule"`
}

type ContextSessionSnapshot struct {
	ID                 string                     `json:"snapshot_id"`
	SessionID          string                     `json:"practice_session_id"`
	PlanRevision       int                        `json:"plan_revision"`
	ScenarioType       string                     `json:"scenario_type"`
	ScenarioDefinition ScenarioDefinitionSnapshot `json:"scenario_definition_snapshot"`
	ScenarioConfig     ScenarioConfigSnapshot     `json:"scenario_config_snapshot"`
	Preparation        PreparationSnapshot        `json:"preparation_snapshot"`
	Participants       []ContextParticipant       `json:"participants"`
	PracticeOption     PracticeOptionSnapshot     `json:"practice_option"`
	SessionPolicy      ContextSessionPolicy       `json:"session_policy"`
	PracticeFocuses    []PracticeObjective        `json:"practice_focuses"`
	CreatedAt          time.Time                  `json:"created_at"`
}

type ContextSessionBootstrap struct {
	Session  ContextSession         `json:"practice_session"`
	Snapshot ContextSessionSnapshot `json:"snapshot"`
}

type CreateContextSessionCommand struct {
	SessionID             string
	SnapshotID            string
	PlanID                string
	ExpectedPlanRevision  int
	PreparationSnapshotID string
	Snapshot              ContextSessionSnapshot
	Intent                ContextIdempotencyIntent
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

// ContextRepository extends the existing 000006 Practice authority. It does
// not introduce a second Session store: formal Session and Snapshot records
// are persisted in practice_sessions and practice_session_snapshots.
type ContextRepository interface {
	ReplayPlan(
		context.Context,
		Actor,
		ContextIdempotencyIntent,
	) (Plan, bool, error)
	CreatePlan(
		context.Context,
		Actor,
		CreatePlanCommand,
	) (Plan, bool, error)
	GetPlan(context.Context, Actor, string) (Plan, error)
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
	ResolveContextSessionByThread(
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
