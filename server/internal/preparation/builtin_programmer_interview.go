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
	scenarios := []catalogScenario{
		programmerInterviewCatalogDefinition(),
		ieltsSpeakingPart2CatalogDefinition(),
		workplaceProgressRiskCatalogDefinition(),
		dailyHotelCheckinCatalogDefinition(),
	}
	scenarios = append(scenarios, basicInterviewCatalogDefinitions()...)
	scenarios = append(scenarios, basicExamCatalogDefinitions()...)
	scenarios = append(scenarios, basicWorkplaceCatalogDefinitions()...)
	scenarios = append(scenarios, basicDailyCatalogDefinitions()...)
	return newCatalog(scenarios)
}

func programmerInterviewCatalogDefinition() catalogScenario {
	return catalogScenario{
		definition: ScenarioDefinition{
			ID:               ProgrammerInterviewScenarioID,
			Type:             ScenarioFamilyInterview,
			Model:            ScenarioModelProjectExperienceDeepDive,
			Name:             "项目经历深挖",
			Version:          1,
			Status:           ScenarioStatusActive,
			TurnPolicyRef:    "interview.project_deep_dive.turn.v1",
			SessionPolicyRef: "interview.project_deep_dive.session.v1",
			DisplayOrder:     30,
		},
		config: ScenarioConfig{
			ID:                   BackendEngineerConfigID,
			ScenarioDefinitionID: ProgrammerInterviewScenarioID,
			Type:                 ScenarioFamilyInterview,
			Model:                ScenarioModelProjectExperienceDeepDive,
			Version:              1,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable APIs and explain engineering trade-offs.",
			PromptModel: ScenarioPromptModel{
				PublicSceneBrief: "围绕一个真实项目说明个人职责、关键难点、技术取舍和结果。",
				PracticeGoal:     "清楚表达个人贡献、决策依据、结果与反思。",
				UserRole:         "候选人",
				AIRole:           "技术面试官",
				PersonaSummary:   "A precise technical interviewer who probes evidence and trade-offs without inventing candidate experience.",
				FocusAreas: []string{
					"background_responsibility",
					"key_challenge",
					"technical_tradeoff",
					"result_reflection",
				},
				TurnBlueprints: []string{
					"澄清项目背景和候选人的具体职责",
					"追问最关键的技术难点",
					"讨论方案选择和技术取舍",
					"核实结果、影响和复盘",
				},
				SuggestedDurationSeconds: 900,
			},
		},
		roles: []RoleDefinition{
			{
				ID:                   TechnicalInterviewerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "TECHNICAL_INTERVIEWER",
				DisplayName:          "技术面试官",
				Responsibilities:     "Probe technical depth, engineering trade-offs, and decision making.",
				Style:                "Precise and evidence seeking.",
				FocusAreas: []string{
					"key_challenge",
					"technical_tradeoff",
				},
				Version:      1,
				DisplayOrder: 10,
			},
			{
				ID:                   HRInterviewerRoleID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				Type:                 "HR_INTERVIEWER",
				DisplayName:          "招聘专员",
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
				DisplayName:          "项目经理",
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
				DisplayName:          "用人经理",
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
				DisplayName:          "完整模拟",
				Version:              1,
				DisplayOrder:         10,
			},
			{
				ID:                   TechnicalFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     TechnicalInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "技术深挖",
				Version:              1,
				DisplayOrder:         20,
			},
			{
				ID:                   HRFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     HRInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "动机与沟通",
				Version:              1,
				DisplayOrder:         30,
			},
			{
				ID:                   ProjectFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     ProjectManagerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "交付与协作",
				Version:              1,
				DisplayOrder:         40,
			},
			{
				ID:                   ExecutiveFocusOptionID,
				ScenarioDefinitionID: ProgrammerInterviewScenarioID,
				RoleDefinitionID:     ExecutiveInterviewerRoleID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "领导力与影响",
				Version:              1,
				DisplayOrder:         50,
			},
		},
	}
}
