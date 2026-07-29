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

type ScenarioFamily string

const (
	ScenarioFamilyInterview ScenarioFamily = "INTERVIEW"
	ScenarioFamilyExam      ScenarioFamily = "EXAM"
	ScenarioFamilyWorkplace ScenarioFamily = "WORKPLACE"
	ScenarioFamilyDaily     ScenarioFamily = "DAILY"
)

type ScenarioModel string

const (
	ScenarioModelProjectExperienceDeepDive    ScenarioModel = "PROJECT_EXPERIENCE_DEEP_DIVE"
	ScenarioModelInterviewBasicDialogue       ScenarioModel = "INTERVIEW_BASIC_DIALOGUE"
	ScenarioModelIELTSSpeakingPart2           ScenarioModel = "IELTS_SPEAKING_PART_2"
	ScenarioModelExamBasicDialogue            ScenarioModel = "EXAM_BASIC_DIALOGUE"
	ScenarioModelProgressAndRiskUpdate        ScenarioModel = "PROGRESS_AND_RISK_UPDATE"
	ScenarioModelWorkplaceBasicDialogue       ScenarioModel = "WORKPLACE_BASIC_DIALOGUE"
	ScenarioModelHotelCheckinAndIssueHandling ScenarioModel = "HOTEL_CHECKIN_AND_ISSUE_HANDLING"
	ScenarioModelDailyBasicDialogue           ScenarioModel = "DAILY_BASIC_DIALOGUE"
)

type Plan struct {
	ID                        string                `json:"practice_plan_id"`
	UserID                    string                `json:"user_id"`
	AgentThreadID             string                `json:"agent_thread_id"`
	MatterID                  string                `json:"matter_id"`
	ScenarioDefinitionID      string                `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int                   `json:"scenario_definition_version"`
	ScenarioType              ScenarioFamily        `json:"scenario_type"`
	ScenarioModel             ScenarioModel         `json:"scenario_model"`
	ScenarioConfigID          string                `json:"scenario_config_id"`
	ScenarioConfigVersion     int                   `json:"scenario_config_version"`
	PreparationProfileID      string                `json:"preparation_profile_id"`
	SelectedRoleIDs           []string              `json:"selected_role_ids"`
	PreparationSnapshot       *PreparationSnapshot  `json:"preparation_snapshot,omitempty"`
	CatalogSnapshot           *PlanCatalogSnapshot  `json:"catalog_snapshot,omitempty"`
	SessionPolicy             *ContextSessionPolicy `json:"session_policy,omitempty"`
	PracticeFocuses           []PracticeObjective   `json:"practice_focuses,omitempty"`
	Revision                  int                   `json:"plan_revision"`
	Status                    PlanStatus            `json:"practice_plan_status"`
	CreatedAt                 time.Time             `json:"created_at"`
	UpdatedAt                 time.Time             `json:"updated_at"`
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
	ScenarioType              ScenarioFamily
	ScenarioModel             ScenarioModel
	ScenarioConfigID          string
	ScenarioConfigVersion     int
	PreparationProfileID      string
	SelectedRoleIDs           []string
	PreparationSnapshot       *PreparationSnapshot
	CatalogSnapshot           *PlanCatalogSnapshot
	SessionPolicy             *ContextSessionPolicy
	PracticeFocuses           []PracticeObjective
	Intent                    ContextIdempotencyIntent
}

type UpdatePlanCommand struct {
	PlanID               string
	ExpectedPlanRevision int
	SelectedRoleIDs      []string
	CatalogSnapshot      PlanCatalogSnapshot
	SessionPolicy        ContextSessionPolicy
	PracticeFocuses      []PracticeObjective
	Intent               ContextIdempotencyIntent
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
	ID            string               `json:"practice_session_id"`
	PlanID        string               `json:"practice_plan_id"`
	ScenarioType  ScenarioFamily       `json:"scenario_type"`
	ScenarioModel ScenarioModel        `json:"scenario_model"`
	SnapshotID    string               `json:"snapshot_id"`
	Status        ContextSessionStatus `json:"practice_session_status"`
	Version       int                  `json:"session_version"`
	// EffectiveTurns is persisted on the shared Practice Session row. It is
	// intentionally not added to the formal Context HTTP representation yet;
	// Agent consumes it through the internal voice boundary.
	EffectiveTurns int        `json:"-"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	EndReason      string     `json:"end_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ScenarioDefinitionSnapshot struct {
	ID               string         `json:"scenario_definition_id"`
	Type             ScenarioFamily `json:"scenario_type"`
	Model            ScenarioModel  `json:"scenario_model"`
	Name             string         `json:"name"`
	Version          int            `json:"version"`
	Status           string         `json:"status"`
	TurnPolicyRef    string         `json:"turn_policy_ref"`
	SessionPolicyRef string         `json:"session_policy_ref"`
}

type ScenarioConfigSnapshot struct {
	ID                   string              `json:"scenario_config_id"`
	ScenarioDefinitionID string              `json:"scenario_definition_id"`
	Type                 ScenarioFamily      `json:"config_type"`
	Model                ScenarioModel       `json:"scenario_model"`
	Version              int                 `json:"version"`
	JobTitle             string              `json:"job_title,omitempty"`
	JobDescription       string              `json:"job_description,omitempty"`
	PromptModel          ScenarioPromptModel `json:"prompt_model"`
	FocusAreas           []string            `json:"focus_areas,omitempty"`
}

type ScenarioPromptModel struct {
	PublicSceneBrief         string   `json:"public_scene_brief"`
	PracticeGoal             string   `json:"practice_goal"`
	UserRole                 string   `json:"user_role"`
	AIRole                   string   `json:"ai_role"`
	PersonaSummary           string   `json:"persona_summary"`
	FocusAreas               []string `json:"focus_areas"`
	TurnBlueprints           []string `json:"turn_blueprints"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
}

type PreparationSnapshot struct {
	ID                                 string                      `json:"preparation_snapshot_id"`
	SourceProfileID                    string                      `json:"source_profile_id"`
	SourceVersion                      int                         `json:"source_version"`
	SourceJobTargetID                  string                      `json:"source_job_target_id,omitempty"`
	SourceJobTargetConfirmationVersion int                         `json:"source_job_target_confirmation_version,omitempty"`
	JobTargetInputSnapshot             *JobTargetInputSnapshot     `json:"job_target_input_snapshot,omitempty"`
	JobTargetCandidateSnapshot         *JobTargetCandidateSnapshot `json:"job_target_candidate_snapshot,omitempty"`
	ResumeSnapshot                     string                      `json:"resume_snapshot,omitempty"`
	JobDescriptionSnapshot             string                      `json:"job_description_snapshot,omitempty"`
	BackgroundSnapshot                 string                      `json:"background_snapshot"`
	CreatedAt                          time.Time                   `json:"created_at"`
}

type JobTargetInputSnapshot struct {
	Source              string `json:"source"`
	JobTitle            string `json:"job_title,omitempty"`
	JobDescription      string `json:"job_description,omitempty"`
	Company             string `json:"company,omitempty"`
	Seniority           string `json:"seniority,omitempty"`
	CandidateBackground string `json:"candidate_background,omitempty"`
	ResumeRef           string `json:"resume_ref,omitempty"`
	PracticeFocus       string `json:"practice_focus,omitempty"`
}

type JobTargetCatalogRecommendationSnapshot struct {
	ScenarioDefinitionID      string   `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version"`
	SelectedRoleIDs           []string `json:"selected_role_ids"`
	PracticeOptionID          string   `json:"practice_option_id"`
	PracticeOptionVersion     int      `json:"practice_option_version"`
}

type JobTargetCandidateSnapshot struct {
	Source                string                                 `json:"source"`
	GeneralAdviceOnly     bool                                   `json:"general_advice_only"`
	JobTitle              string                                 `json:"job_title"`
	Seniority             string                                 `json:"seniority"`
	Responsibilities      []string                               `json:"responsibilities"`
	CoreSkills            []string                               `json:"core_skills"`
	CommunicationFocus    []string                               `json:"communication_focus"`
	PracticeGoals         []string                               `json:"practice_goals"`
	ScopeNotice           string                                 `json:"scope_notice"`
	CatalogRecommendation JobTargetCatalogRecommendationSnapshot `json:"catalog_recommendation"`
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

type PlanCatalogSnapshot struct {
	ScenarioDefinition ScenarioDefinitionSnapshot `json:"scenario_definition"`
	ScenarioConfig     ScenarioConfigSnapshot     `json:"scenario_config"`
	SelectedRoles      []RoleSnapshot             `json:"selected_roles"`
	PracticeOption     PracticeOptionSnapshot     `json:"practice_option"`
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
	ScenarioType       ScenarioFamily             `json:"scenario_type"`
	ScenarioModel      ScenarioModel              `json:"scenario_model"`
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
	UpdatePlan(
		context.Context,
		Actor,
		UpdatePlanCommand,
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

// ContextVoiceRepository is the narrow formal Context authority consumed by
// the production Agent voice composition. It never creates a legacy Session:
// Start resolves the exact Actor + Thread + Matter binding, while later
// recovery uses the immutable Plan anchor for the Actor + Thread.
type ContextVoiceRepository interface {
	GetPlan(context.Context, Actor, string) (Plan, error)
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
	ResolveContextSessionByThread(
		context.Context,
		Actor,
		string,
	) (ContextSessionBootstrap, error)
	ResolveContextSession(
		context.Context,
		Actor,
		string,
		string,
	) (ContextSessionBootstrap, error)
	ActivateContextSession(
		context.Context,
		Actor,
		string,
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
