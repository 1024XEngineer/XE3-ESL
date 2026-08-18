package scene

// PracticeExperience identifies the product entry that owns a Scene.
type PracticeExperience string

const (
	PracticeExperienceInterview     PracticeExperience = "INTERVIEW"
	PracticeExperienceIELTSSpeaking PracticeExperience = "IELTS_SPEAKING"
	PracticeExperienceWorkplace     PracticeExperience = "WORKPLACE"
	PracticeExperienceLifeAndTravel PracticeExperience = "LIFE_AND_TRAVEL"
)

// SceneCategory identifies the server-authored catalog group within one
// PracticeExperience. It is presentation metadata, not a runtime policy key.
type SceneCategory string

const (
	SceneCategoryInterviewRecruiter     SceneCategory = "INTERVIEW_RECRUITER"
	SceneCategoryInterviewBehavioral    SceneCategory = "INTERVIEW_BEHAVIORAL"
	SceneCategoryInterviewProfessional  SceneCategory = "INTERVIEW_PROFESSIONAL"
	SceneCategoryInterviewHiringManager SceneCategory = "INTERVIEW_HIRING_MANAGER"
	SceneCategoryInterviewCustom        SceneCategory = "INTERVIEW_CUSTOM"
	SceneCategoryIELTSSpeaking          SceneCategory = "IELTS_SPEAKING"
	SceneCategoryWorkplaceGeneral       SceneCategory = "WORKPLACE_GENERAL"
	SceneCategoryLifeTravel             SceneCategory = "LIFE_TRAVEL"
	SceneCategoryLifeDaily              SceneCategory = "LIFE_DAILY"
)

type SceneStatus string

const (
	SceneStatusActive   SceneStatus = "active"
	SceneStatusInactive SceneStatus = "inactive"
)

type PracticeMode string

const (
	PracticeModeFullSimulation PracticeMode = "FULL_SIMULATION"
	PracticeModeFocus          PracticeMode = "FOCUS"
	PracticeModeFullMock       PracticeMode = "FULL_MOCK"
	PracticeModePart1          PracticeMode = "PART_1"
	PracticeModePart2          PracticeMode = "PART_2"
	PracticeModePart3          PracticeMode = "PART_3"
)

// ScenePrompt is prompt and preview content owned by one Scene version.
type ScenePrompt struct {
	PublicSceneBrief string   `json:"public_scene_brief"`
	PracticeGoal     string   `json:"practice_goal"`
	UserRole         string   `json:"user_role"`
	AIRole           string   `json:"ai_role"`
	PersonaSummary   string   `json:"persona_summary"`
	FocusAreas       []string `json:"focus_areas"`
	TurnBlueprints   []string `json:"turn_blueprints"`
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
	ID                       string       `json:"practice_option_id"`
	SceneID                  string       `json:"scene_id"`
	RoleDefinitionID         string       `json:"role_definition_id,omitempty"`
	Mode                     PracticeMode `json:"practice_mode"`
	DisplayName              string       `json:"display_name"`
	SuggestedDurationSeconds int          `json:"suggested_duration_seconds"`
	TurnPolicyRef            string       `json:"turn_policy_ref"`
	SessionPolicyRef         string       `json:"session_policy_ref"`
	EvaluationPolicyRef      string       `json:"evaluation_policy_ref"`
	DisplayOrder             int          `json:"-"`
}

// SceneDefinition is the single immutable, versioned authority for one Scene.
// Prompt, roles, options, and policy references change only with SceneVersion.
type SceneDefinition struct {
	ID              string             `json:"scene_id"`
	Experience      PracticeExperience `json:"practice_experience"`
	Category        SceneCategory      `json:"scene_category"`
	Name            string             `json:"name"`
	Version         int                `json:"scene_version"`
	Status          SceneStatus        `json:"status"`
	Prompt          ScenePrompt        `json:"prompt"`
	Roles           []RoleDefinition   `json:"roles"`
	PracticeOptions []PracticeOption   `json:"practice_options"`
	DisplayOrder    int                `json:"-"`
}

type SceneSourceType string

const (
	SceneSourceCatalog SceneSourceType = "CATALOG"
	SceneSourceCustom  SceneSourceType = "CUSTOM"
)

// SceneSource identifies where an executable scene came from. Catalog
// identity is kept here instead of being imitated by a custom scene.
type SceneSource struct {
	Type         SceneSourceType `json:"type"`
	SceneID      string          `json:"scene_id,omitempty"`
	SceneVersion int             `json:"scene_version,omitempty"`
}

// RoleSnapshot is the execution form of one selected-scene role. SceneKey is
// a plan-scoped execution key, not necessarily a Catalog scene identifier.
type RoleSnapshot struct {
	ID                 string                        `json:"role_definition_id"`
	SceneKey           string                        `json:"scene_key"`
	Type               string                        `json:"role_type"`
	DisplayName        string                        `json:"display_name"`
	Responsibilities   string                        `json:"responsibilities"`
	Style              string                        `json:"style"`
	PracticeObjectives []PracticeObjectiveDefinition `json:"practice_objectives"`
	VoiceConfigRef     string                        `json:"voice_config_ref,omitempty"`
}

// PracticeOptionSnapshot contains only execution data frozen into a Plan.
type PracticeOptionSnapshot struct {
	ID                       string       `json:"practice_option_id"`
	SceneKey                 string       `json:"scene_key"`
	RoleDefinitionID         string       `json:"role_definition_id,omitempty"`
	Mode                     PracticeMode `json:"practice_mode"`
	DisplayName              string       `json:"display_name"`
	SuggestedDurationSeconds int          `json:"suggested_duration_seconds"`
	TurnPolicyRef            string       `json:"turn_policy_ref"`
	SessionPolicyRef         string       `json:"session_policy_ref"`
	EvaluationPolicyRef      string       `json:"evaluation_policy_ref"`
}

// ExecutableSceneSnapshot is the immutable runtime content shared by Catalog
// and one-off Custom scenes. Publication status and Catalog identity do not
// belong to this value.
type ExecutableSceneSnapshot struct {
	Key             string                   `json:"scene_key"`
	Revision        int                      `json:"scene_revision"`
	Experience      PracticeExperience       `json:"practice_experience"`
	Category        SceneCategory            `json:"scene_category"`
	Name            string                   `json:"name"`
	Prompt          ScenePrompt              `json:"prompt"`
	Roles           []RoleSnapshot           `json:"roles"`
	PracticeOptions []PracticeOptionSnapshot `json:"practice_options"`
}

// SelectionSnapshot freezes one exact executable scene and the user's
// selection. Source is an explicit discriminator for Catalog and Custom data.
type SelectionSnapshot struct {
	Source           SceneSource             `json:"source"`
	Scene            ExecutableSceneSnapshot `json:"scene"`
	SelectedRoleIDs  []string                `json:"selected_role_ids"`
	PracticeOptionID string                  `json:"practice_option_id"`
}

func (selection SelectionSnapshot) SelectedRoles() ([]RoleSnapshot, error) {
	roles := make([]RoleSnapshot, 0, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		role, found := findRoleSnapshot(selection.Scene.Roles, roleID)
		if !found {
			return nil, ErrRoleDefinitionNotFound
		}
		roles = append(roles, cloneRoleSnapshot(role))
	}
	return roles, nil
}

func (selection SelectionSnapshot) PracticeOption() (PracticeOptionSnapshot, error) {
	option, found := findPracticeOptionSnapshot(
		selection.Scene.PracticeOptions,
		selection.PracticeOptionID,
	)
	if !found {
		return PracticeOptionSnapshot{}, ErrPracticeOptionNotFound
	}
	return option, nil
}
