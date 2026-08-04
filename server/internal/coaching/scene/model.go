package scene

// SceneFamily identifies the broad behavior family of a Scene.
type SceneFamily string

const (
	SceneFamilyInterview SceneFamily = "INTERVIEW"
	SceneFamilyExam      SceneFamily = "EXAM"
	SceneFamilyWorkplace SceneFamily = "WORKPLACE"
	SceneFamilyDaily     SceneFamily = "DAILY"
)

// SceneModel identifies the concrete behavior model within one family.
type SceneModel string

const (
	SceneModelProjectExperienceDeepDive    SceneModel = "PROJECT_EXPERIENCE_DEEP_DIVE"
	SceneModelInterviewBasicDialogue       SceneModel = "INTERVIEW_BASIC_DIALOGUE"
	SceneModelIELTSSpeakingPart1           SceneModel = "IELTS_SPEAKING_PART_1"
	SceneModelIELTSSpeakingPart2           SceneModel = "IELTS_SPEAKING_PART_2"
	SceneModelIELTSSpeakingPart3           SceneModel = "IELTS_SPEAKING_PART_3"
	SceneModelIELTSSpeakingFullMock        SceneModel = "IELTS_SPEAKING_FULL_MOCK"
	SceneModelExamBasicDialogue            SceneModel = "EXAM_BASIC_DIALOGUE"
	SceneModelProgressAndRiskUpdate        SceneModel = "PROGRESS_AND_RISK_UPDATE"
	SceneModelWorkplaceBasicDialogue       SceneModel = "WORKPLACE_BASIC_DIALOGUE"
	SceneModelHotelCheckinAndIssueHandling SceneModel = "HOTEL_CHECKIN_AND_ISSUE_HANDLING"
	SceneModelDailyBasicDialogue           SceneModel = "DAILY_BASIC_DIALOGUE"
)

type SceneStatus string

const (
	SceneStatusActive   SceneStatus = "active"
	SceneStatusInactive SceneStatus = "inactive"
)

type PracticeOptionType string

const (
	PracticeOptionFullSimulation PracticeOptionType = "FULL_SIMULATION"
	PracticeOptionFocus          PracticeOptionType = "FOCUS"
)

// ScenePrompt is prompt and preview content owned by one Scene version.
type ScenePrompt struct {
	PublicSceneBrief         string   `json:"public_scene_brief"`
	PracticeGoal             string   `json:"practice_goal"`
	UserRole                 string   `json:"user_role"`
	AIRole                   string   `json:"ai_role"`
	PersonaSummary           string   `json:"persona_summary"`
	FocusAreas               []string `json:"focus_areas"`
	TurnBlueprints           []string `json:"turn_blueprints"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
}

// PracticeObjectiveDefinition is one executable objective offered by a Role.
type PracticeObjectiveDefinition struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

// RoleDefinition is one optional counterpart perspective owned by a Scene.
type RoleDefinition struct {
	ID                 string                        `json:"role_definition_id"`
	SceneID            string                        `json:"scene_id"`
	Type               string                        `json:"role_type"`
	DisplayName        string                        `json:"display_name"`
	Responsibilities   string                        `json:"responsibilities"`
	Style              string                        `json:"style"`
	PracticeObjectives []PracticeObjectiveDefinition `json:"practice_objectives"`
	VoiceConfigRef     string                        `json:"voice_config_ref,omitempty"`
	DisplayOrder       int                           `json:"-"`
}

// PracticeOption describes one stable role-selection mode owned by a Scene.
type PracticeOption struct {
	ID               string             `json:"practice_option_id"`
	SceneID          string             `json:"scene_id"`
	RoleDefinitionID string             `json:"role_definition_id,omitempty"`
	Type             PracticeOptionType `json:"practice_option_type"`
	DisplayName      string             `json:"display_name"`
	DisplayOrder     int                `json:"-"`
}

// SceneDefinition is the single immutable, versioned authority for one Scene.
// Prompt, roles, options, and policy references change only with SceneVersion.
type SceneDefinition struct {
	ID               string           `json:"scene_id"`
	Family           SceneFamily      `json:"scene_family"`
	Model            SceneModel       `json:"scene_model"`
	Name             string           `json:"name"`
	Version          int              `json:"scene_version"`
	Status           SceneStatus      `json:"status"`
	TurnPolicyRef    string           `json:"turn_policy_ref"`
	SessionPolicyRef string           `json:"session_policy_ref"`
	Prompt           ScenePrompt      `json:"prompt"`
	Roles            []RoleDefinition `json:"roles"`
	PracticeOptions  []PracticeOption `json:"practice_options"`
	DisplayOrder     int              `json:"-"`
}

// SelectionSnapshot freezes one exact Scene version and the user's selection.
type SelectionSnapshot struct {
	Scene            SceneDefinition `json:"scene"`
	SelectedRoleIDs  []string        `json:"selected_role_ids"`
	PracticeOptionID string          `json:"practice_option_id"`
}

func (selection SelectionSnapshot) SelectedRoles() ([]RoleDefinition, error) {
	roles := make([]RoleDefinition, 0, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		role, found := findRole(selection.Scene.Roles, roleID)
		if !found {
			return nil, ErrRoleDefinitionNotFound
		}
		roles = append(roles, cloneRole(role))
	}
	return roles, nil
}

func (selection SelectionSnapshot) PracticeOption() (PracticeOption, error) {
	option, found := findPracticeOption(
		selection.Scene.PracticeOptions,
		selection.PracticeOptionID,
	)
	if !found {
		return PracticeOption{}, ErrPracticeOptionNotFound
	}
	return option, nil
}
