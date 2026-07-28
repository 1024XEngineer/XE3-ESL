package practice

import "time"

type ScenarioType string

const ScenarioTypeInterview ScenarioType = "INTERVIEW"

type PracticePlanStatus string

const (
	PracticePlanConfiguring         PracticePlanStatus = "configuring"
	PracticePlanConfigurationFailed PracticePlanStatus = "configuration_failed"
	PracticePlanReady               PracticePlanStatus = "ready"
	PracticePlanArchived            PracticePlanStatus = "archived"
)

type PracticeSessionStatus string

const (
	PracticeSessionStarting   PracticeSessionStatus = "starting"
	PracticeSessionInProgress PracticeSessionStatus = "in_progress"
	PracticeSessionPaused     PracticeSessionStatus = "paused"
	PracticeSessionCompleted  PracticeSessionStatus = "completed"
	PracticeSessionEndedEarly PracticeSessionStatus = "ended_early"
)

type PracticeSessionEndReason string

const PracticeSessionEndCoverageSatisfiedAtCheckpoint PracticeSessionEndReason = "COVERAGE_SATISFIED_AT_CHECKPOINT"

type PracticePlan struct {
	ID                        string             `json:"practice_plan_id"`
	UserID                    string             `json:"user_id"`
	AgentThreadID             string             `json:"agent_thread_id"`
	MatterID                  string             `json:"matter_id"`
	ScenarioDefinitionID      string             `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int                `json:"scenario_definition_version"`
	ScenarioType              ScenarioType       `json:"scenario_type"`
	ScenarioConfigID          string             `json:"scenario_config_id"`
	ScenarioConfigVersion     int                `json:"scenario_config_version"`
	PreparationProfileID      string             `json:"preparation_profile_id"`
	SelectedRoleIDs           []string           `json:"selected_role_ids"`
	Revision                  int                `json:"plan_revision"`
	Status                    PracticePlanStatus `json:"practice_plan_status"`
	CreatedAt                 time.Time          `json:"created_at"`
	UpdatedAt                 time.Time          `json:"updated_at"`
}

type PracticeSession struct {
	ID           string                   `json:"practice_session_id"`
	PlanID       string                   `json:"practice_plan_id"`
	ScenarioType ScenarioType             `json:"scenario_type"`
	SnapshotID   string                   `json:"snapshot_id"`
	Status       PracticeSessionStatus    `json:"practice_session_status"`
	Version      int                      `json:"session_version"`
	CreatedAt    time.Time                `json:"created_at"`
	StartedAt    *time.Time               `json:"started_at,omitempty"`
	EndedAt      *time.Time               `json:"ended_at,omitempty"`
	EndReason    PracticeSessionEndReason `json:"end_reason,omitempty"`
}

type SubjectRef struct {
	Namespace string `json:"namespace"`
	SubjectID string `json:"subject_id"`
}

type ParticipantRole string

type ScenarioDefinitionSnapshot struct {
	ID      string       `json:"scenario_definition_id"`
	Type    ScenarioType `json:"scenario_type"`
	Name    string       `json:"name"`
	Version int          `json:"version"`
	Status  string       `json:"status"`
}

type ScenarioConfigSnapshot struct {
	ID                   string       `json:"scenario_config_id"`
	ScenarioDefinitionID string       `json:"scenario_definition_id"`
	Type                 ScenarioType `json:"config_type"`
	Version              int          `json:"version"`
	JobTitle             string       `json:"job_title"`
	JobDescription       string       `json:"job_description"`
	FocusAreas           []string     `json:"focus_areas"`
}

type PreparationSnapshot struct {
	ID                     string    `json:"preparation_snapshot_id"`
	SourceProfileID        string    `json:"source_profile_id"`
	SourceVersion          int       `json:"source_version"`
	ResumeSnapshot         string    `json:"resume_snapshot"`
	JobDescriptionSnapshot string    `json:"job_description_snapshot"`
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

type PracticeParticipant struct {
	ID               string          `json:"practice_participant_id"`
	SessionID        string          `json:"practice_session_id"`
	ParticipantRole  ParticipantRole `json:"participant_role"`
	SubjectRef       SubjectRef      `json:"subject_ref"`
	RoleDefinitionID string          `json:"role_definition_id,omitempty"`
	RoleSnapshot     *RoleSnapshot   `json:"role_snapshot,omitempty"`
	ParticipantOrder int             `json:"participant_order"`
}

type PracticeObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type PracticeSessionSnapshot struct {
	ID                 string                     `json:"snapshot_id"`
	SessionID          string                     `json:"practice_session_id"`
	PlanRevision       int                        `json:"plan_revision"`
	ScenarioType       ScenarioType               `json:"scenario_type"`
	ScenarioDefinition ScenarioDefinitionSnapshot `json:"scenario_definition_snapshot"`
	ScenarioConfig     ScenarioConfigSnapshot     `json:"scenario_config_snapshot"`
	Preparation        PreparationSnapshot        `json:"preparation_snapshot"`
	Participants       []PracticeParticipant      `json:"participants"`
	PracticeOption     PracticeOptionSnapshot     `json:"practice_option"`
	SessionPolicy      PracticeSessionPolicy      `json:"session_policy"`
	PracticeFocuses    []PracticeObjective        `json:"practice_focuses"`
	CreatedAt          time.Time                  `json:"created_at"`
}
