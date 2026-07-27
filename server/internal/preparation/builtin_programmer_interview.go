package preparation

const (
	ProgrammerInterviewScenarioID = "scn_programmer_interview"
	BackendEngineerConfigID       = "scfg_backend_engineer"

	HRInterviewerRoleID        = "role_hr_interviewer"
	TechnicalInterviewerRoleID = "role_technical_interviewer"
	ProjectManagerRoleID       = "role_project_manager"
	ExecutiveInterviewerRoleID = "role_executive_interviewer"

	FullSimulationOptionID = "option_full_simulation"
	HRFocusOptionID        = "option_hr_focus"
	TechnicalFocusOptionID = "option_technical_focus"
	ProjectFocusOptionID   = "option_project_manager_focus"
	ExecutiveFocusOptionID = "option_executive_focus"
)

// NewBuiltinCatalog validates and returns the v1 in-process catalog.
func NewBuiltinCatalog() (*Catalog, error) {
	return newCatalog([]catalogScenario{programmerInterviewCatalogDefinition()})
}

func programmerInterviewCatalogDefinition() catalogScenario {
	return catalogScenario{
		definition: ScenarioDefinition{
			ID:           ProgrammerInterviewScenarioID,
			Type:         ScenarioTypeInterview,
			Name:         "English interview for technical roles",
			Version:      1,
			Status:       ScenarioStatusActive,
			DisplayOrder: 10,
		},
		config: ScenarioConfig{
			ID:                   BackendEngineerConfigID,
			ScenarioDefinitionID: ProgrammerInterviewScenarioID,
			Type:                 ScenarioTypeInterview,
			Version:              1,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable APIs and explain engineering trade-offs.",
			FocusAreas: []string{
				"introduction",
				"system_design",
				"project_depth",
				"collaboration",
			},
		},
		roles: []RoleDefinition{
			{
				ID:                   TechnicalInterviewerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "TECHNICAL_INTERVIEWER",
				DisplayName:          "Technical depth perspective",
				Responsibilities:     "Probe technical depth, engineering trade-offs, and decision making.",
				Style:                "Precise and evidence seeking.",
				FocusAreas:           []string{"system_design", "project_depth"},
				Version:              1,
				DisplayOrder:         10,
			},
			{
				ID:                   HRInterviewerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "HR_INTERVIEWER",
				DisplayName:          "Recruiter and motivation perspective",
				Responsibilities:     "Explore career motivation and communication clarity.",
				Style:                "Warm and structured.",
				FocusAreas:           []string{"motivation", "communication"},
				Version:              1,
				DisplayOrder:         20,
			},
			{
				ID:                   ProjectManagerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "PROJECT_MANAGER",
				DisplayName:          "Delivery and collaboration perspective",
				Responsibilities:     "Explore delivery ownership and cross-functional collaboration.",
				Style:                "Outcome oriented and collaborative.",
				FocusAreas:           []string{"delivery", "collaboration"},
				Version:              1,
				DisplayOrder:         30,
			},
			{
				ID:                   ExecutiveInterviewerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "EXECUTIVE_INTERVIEWER",
				DisplayName:          "Leadership and impact perspective",
				Responsibilities:     "Explore leadership judgment and measurable impact for senior or management roles.",
				Style:                "Concise, high level, and optional for advanced roles.",
				FocusAreas:           []string{"impact", "judgment"},
				Version:              1,
				DisplayOrder:         40,
			},
		},
		practiceOptions: []PracticeOptionDefinition{
			{
				ID:                   FullSimulationOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 PracticeOptionFullSimulation,
				DisplayName:          "Full simulation",
				Version:              1,
				DisplayOrder:         10,
			},
			{
				ID:                   TechnicalFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     TechnicalInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "Technical depth focus",
				Version:              1,
				DisplayOrder:         20,
			},
			{
				ID:                   HRFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     HRInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "Recruiter and motivation focus",
				Version:              1,
				DisplayOrder:         30,
			},
			{
				ID:                   ProjectFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     ProjectManagerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "Delivery and collaboration focus",
				Version:              1,
				DisplayOrder:         40,
			},
			{
				ID:                   ExecutiveFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     ExecutiveInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "Leadership and impact focus",
				Version:              1,
				DisplayOrder:         50,
			},
		},
	}
}
