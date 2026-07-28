package preparation

// ScenarioFamily identifies the behavior family serialized as scenario_type.
type ScenarioFamily string

const (
	ScenarioFamilyInterview ScenarioFamily = "INTERVIEW"
	ScenarioFamilyExam      ScenarioFamily = "EXAM"
	ScenarioFamilyWorkplace ScenarioFamily = "WORKPLACE"
	ScenarioFamilyDaily     ScenarioFamily = "DAILY"
)

// ScenarioModel identifies the concrete behavior model within one family.
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

// ScenarioStatus determines whether a scenario is available to new practice
// plans. Inactive definitions remain valid catalog data but are not public.
type ScenarioStatus string

const (
	ScenarioStatusActive   ScenarioStatus = "active"
	ScenarioStatusInactive ScenarioStatus = "inactive"
)

// PracticeOptionType identifies how a scenario and its roles are selected.
type PracticeOptionType string

const (
	PracticeOptionFullSimulation PracticeOptionType = "FULL_SIMULATION"
	PracticeOptionFocus          PracticeOptionType = "FOCUS"
)

// ScenarioDefinition is the public, versioned scenario identity.
type ScenarioDefinition struct {
	ID               string         `json:"scenario_definition_id"`
	Type             ScenarioFamily `json:"scenario_type"`
	Model            ScenarioModel  `json:"scenario_model"`
	Name             string         `json:"name"`
	Version          int            `json:"version"`
	Status           ScenarioStatus `json:"status"`
	TurnPolicyRef    string         `json:"turn_policy_ref"`
	SessionPolicyRef string         `json:"session_policy_ref"`
	DisplayOrder     int            `json:"-"`
}

// ScenarioPromptModel is the shared prompt and preview content frozen into a
// Practice Session snapshot.
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

// ScenarioConfig is the current versioned configuration for one scenario.
// Job fields remain only for the JD-first interview compatibility path.
type ScenarioConfig struct {
	ID                   string              `json:"scenario_config_id"`
	ScenarioDefinitionID string              `json:"scenario_definition_id"`
	Type                 ScenarioFamily      `json:"config_type"`
	Model                ScenarioModel       `json:"scenario_model"`
	Version              int                 `json:"version"`
	JobTitle             string              `json:"job_title,omitempty"`
	JobDescription       string              `json:"job_description,omitempty"`
	PromptModel          ScenarioPromptModel `json:"prompt_model"`
}

// RoleDefinition is a versioned, Preparation-owned optional interviewer
// perspective. DisplayOrder controls menu presentation only; it never implies a
// required interview stage or progression sequence.
type RoleDefinition struct {
	ID                   string   `json:"role_definition_id"`
	ScenarioDefinitionID string   `json:"scenario_definition_id"`
	Type                 string   `json:"role_type"`
	DisplayName          string   `json:"display_name"`
	Responsibilities     string   `json:"responsibilities"`
	Style                string   `json:"style"`
	FocusAreas           []string `json:"focus_areas"`
	VoiceConfigRef       string   `json:"voice_config_ref,omitempty"`
	Version              int      `json:"version"`
	DisplayOrder         int      `json:"-"`
}

// PracticeOptionDefinition describes one stable way to practice from the
// currently selected perspective. FULL_SIMULATION is not bound to one role;
// FOCUS is bound to exactly one role.
type PracticeOptionDefinition struct {
	ID                   string             `json:"practice_option_id"`
	ScenarioDefinitionID string             `json:"scenario_definition_id"`
	RoleDefinitionID     string             `json:"role_definition_id,omitempty"`
	Type                 PracticeOptionType `json:"practice_option_type"`
	DisplayName          string             `json:"display_name"`
	Version              int                `json:"version"`
	DisplayOrder         int                `json:"-"`
}

// ScenarioDetail is the canonical REST and application read model.
type ScenarioDetail struct {
	ScenarioDefinition ScenarioDefinition         `json:"scenario_definition"`
	ScenarioConfig     ScenarioConfig             `json:"scenario_config"`
	PracticeOptions    []PracticeOptionDefinition `json:"practice_options"`
}

type ScenarioDefinitionListItem struct {
	ScenarioDefinition
	Summary string `json:"summary"`
}

// ScenarioDefinitionList is the canonical scenario collection response.
type ScenarioDefinitionList struct {
	Scenarios []ScenarioDefinitionListItem `json:"scenarios"`
}

// RoleDefinitionList is the canonical role collection response.
type RoleDefinitionList struct {
	Roles []RoleDefinition `json:"roles"`
}

// CatalogSnapshot is the minimum immutable catalog selection consumed by a
// future Practice adapter. Returned values are copies of catalog data.
type CatalogSnapshot struct {
	ScenarioDefinition ScenarioDefinition       `json:"scenario_definition"`
	ScenarioConfig     ScenarioConfig           `json:"scenario_config"`
	SelectedRoles      []RoleDefinition         `json:"selected_roles"`
	PracticeOption     PracticeOptionDefinition `json:"practice_option"`
}
