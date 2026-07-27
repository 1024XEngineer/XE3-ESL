package preparation

// ScenarioType identifies the behavior family of a scenario definition.
type ScenarioType string

const (
	ScenarioTypeInterview ScenarioType = "INTERVIEW"
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
	ID           string         `json:"scenario_definition_id"`
	Type         ScenarioType   `json:"scenario_type"`
	Name         string         `json:"name"`
	Version      int            `json:"version"`
	Status       ScenarioStatus `json:"status"`
	DisplayOrder int            `json:"-"`
}

// ScenarioConfig is the current INTERVIEW configuration for one scenario.
// Additional config families require an accepted contract increment.
type ScenarioConfig struct {
	ID                   string       `json:"scenario_config_id"`
	ScenarioDefinitionID string       `json:"scenario_definition_id"`
	Type                 ScenarioType `json:"config_type"`
	Version              int          `json:"version"`
	JobTitle             string       `json:"job_title"`
	JobDescription       string       `json:"job_description"`
	FocusAreas           []string     `json:"focus_areas"`
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

// ScenarioDefinitionList is the canonical scenario collection response.
type ScenarioDefinitionList struct {
	Scenarios []ScenarioDefinition `json:"scenarios"`
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
