package preparation

const (
	IELTSSpeakingPart2ScenarioID    = "scn_ielts_speaking_part_2"
	WorkplaceProgressRiskScenarioID = "scn_workplace_progress_risk_update"
	DailyHotelCheckinScenarioID     = "scn_daily_hotel_checkin_issue"

	IELTSSpeakingPart2ConfigID    = "scfg_ielts_speaking_part_2"
	WorkplaceProgressRiskConfigID = "scfg_workplace_progress_risk_update"
	DailyHotelCheckinConfigID     = "scfg_daily_hotel_checkin_issue"

	IELTSExaminerRoleID  = "role_ielts_examiner"
	DirectManagerRoleID  = "role_direct_manager"
	HotelFrontDeskRoleID = "role_hotel_front_desk"

	IELTSFullSimulationOptionID     = "option_ielts_full_simulation"
	IELTSExaminerFocusOptionID      = "option_ielts_examiner_focus"
	WorkplaceFullSimulationOptionID = "option_workplace_full_simulation"
	DirectManagerFocusOptionID      = "option_direct_manager_focus"
	HotelFullSimulationOptionID     = "option_hotel_full_simulation"
	HotelFrontDeskFocusOptionID     = "option_hotel_front_desk_focus"
)

func ieltsSpeakingPart2CatalogDefinition() catalogScenario {
	return singleRoleScenario(
		ScenarioDefinition{
			ID:           IELTSSpeakingPart2ScenarioID,
			Type:         ScenarioFamilyExam,
			Model:        ScenarioModelIELTSSpeakingPart2,
			Name:         "IELTS Speaking Part 2",
			Version:      1,
			Status:       ScenarioStatusActive,
			DisplayOrder: 20,
		},
		ScenarioConfig{
			ID:                   IELTSSpeakingPart2ConfigID,
			ScenarioDefinitionID: IELTSSpeakingPart2ScenarioID,
			Type:                 ScenarioFamilyExam,
			Model:                ScenarioModelIELTSSpeakingPart2,
			Version:              1,
			PromptModel: ScenarioPromptModel{
				PublicSceneBrief: "根据一张 Cue Card 进行连续表达，并回答考官的必要追问。",
				PracticeGoal:     "围绕主题清楚展开观点、细节和理由。",
				UserRole:         "考生",
				AIRole:           "IELTS 口语考官",
				PersonaSummary:   "A neutral IELTS speaking examiner who presents one task at a time and does not teach during the simulation.",
				FocusAreas: []string{
					"cue_card",
					"topic_development",
					"detail_and_reason",
					"fluency_and_extension",
				},
				TurnBlueprints: []string{
					"给出清楚的 Cue Card 并邀请作答",
					"根据回答追问主体内容",
					"追问一个细节或理由",
					"检查表达的流利度和展开程度",
				},
				SuggestedDurationSeconds: 600,
			},
		},
		RoleDefinition{
			ID:                   IELTSExaminerRoleID,
			ScenarioDefinitionID: IELTSSpeakingPart2ScenarioID,
			Type:                 "IELTS_EXAMINER",
			DisplayName:          "IELTS 口语考官",
			Responsibilities:     "Present the cue card and ask concise, neutral follow-up questions.",
			Style:                "Neutral, concise, and exam appropriate.",
			FocusAreas:           []string{"topic_development", "fluency_and_extension"},
			Version:              1,
			DisplayOrder:         10,
		},
		IELTSFullSimulationOptionID,
		IELTSExaminerFocusOptionID,
	)
}

func workplaceProgressRiskCatalogDefinition() catalogScenario {
	return singleRoleScenario(
		ScenarioDefinition{
			ID:           WorkplaceProgressRiskScenarioID,
			Type:         ScenarioFamilyWorkplace,
			Model:        ScenarioModelProgressAndRiskUpdate,
			Name:         "进度与风险汇报",
			Version:      1,
			Status:       ScenarioStatusActive,
			DisplayOrder: 10,
		},
		ScenarioConfig{
			ID:                   WorkplaceProgressRiskConfigID,
			ScenarioDefinitionID: WorkplaceProgressRiskScenarioID,
			Type:                 ScenarioFamilyWorkplace,
			Model:                ScenarioModelProgressAndRiskUpdate,
			Version:              1,
			PromptModel: ScenarioPromptModel{
				PublicSceneBrief: "向直属领导汇报项目进展、证据、风险和需要的支持。",
				PracticeGoal:     "用结果导向的方式说明状态、风险、方案和决策请求。",
				UserRole:         "项目负责人",
				AIRole:           "直属领导",
				PersonaSummary:   "A direct manager who asks for evidence, mitigation, and a concrete decision or support request.",
				FocusAreas: []string{
					"status",
					"evidence",
					"risk_mitigation",
					"decision_or_support",
				},
				TurnBlueprints: []string{
					"请用户概括当前状态",
					"核实进展证据和影响",
					"追问主要风险与缓解方案",
					"确认需要的决策或支持",
				},
				SuggestedDurationSeconds: 600,
			},
		},
		RoleDefinition{
			ID:                   DirectManagerRoleID,
			ScenarioDefinitionID: WorkplaceProgressRiskScenarioID,
			Type:                 "DIRECT_MANAGER",
			DisplayName:          "直属领导",
			Responsibilities:     "Clarify delivery status, impact, mitigation, and the requested decision.",
			Style:                "Direct, outcome oriented, and constructive.",
			FocusAreas:           []string{"evidence", "risk_mitigation", "decision_or_support"},
			Version:              1,
			DisplayOrder:         10,
		},
		WorkplaceFullSimulationOptionID,
		DirectManagerFocusOptionID,
	)
}

func dailyHotelCheckinCatalogDefinition() catalogScenario {
	return singleRoleScenario(
		ScenarioDefinition{
			ID:           DailyHotelCheckinScenarioID,
			Type:         ScenarioFamilyDaily,
			Model:        ScenarioModelHotelCheckinAndIssueHandling,
			Name:         "酒店入住与问题处理",
			Version:      1,
			Status:       ScenarioStatusActive,
			DisplayOrder: 50,
		},
		ScenarioConfig{
			ID:                   DailyHotelCheckinConfigID,
			ScenarioDefinitionID: DailyHotelCheckinScenarioID,
			Type:                 ScenarioFamilyDaily,
			Model:                ScenarioModelHotelCheckinAndIssueHandling,
			Version:              1,
			PromptModel: ScenarioPromptModel{
				PublicSceneBrief: "在酒店前台核验预订、办理入住并处理一个房间问题。",
				PracticeGoal:     "清楚说明预订和问题，理解可行方案并确认最终安排。",
				UserRole:         "住客",
				AIRole:           "酒店前台",
				PersonaSummary:   "A professional hotel receptionist who verifies details, clarifies one issue, offers realistic options, and confirms the outcome.",
				FocusAreas: []string{
					"reservation_verification",
					"issue_description",
					"solution_clarification",
					"outcome_confirmation",
				},
				TurnBlueprints: []string{
					"核验住客姓名和预订信息",
					"请住客说明入住或房间问题",
					"澄清限制并提供可行方案",
					"确认住客选择和最终安排",
				},
				SuggestedDurationSeconds: 480,
			},
		},
		RoleDefinition{
			ID:                   HotelFrontDeskRoleID,
			ScenarioDefinitionID: DailyHotelCheckinScenarioID,
			Type:                 "HOTEL_FRONT_DESK",
			DisplayName:          "酒店前台",
			Responsibilities:     "Verify the booking, clarify the issue, offer realistic options, and confirm the final arrangement.",
			Style:                "Professional, calm, and service oriented.",
			FocusAreas:           []string{"reservation_verification", "solution_clarification"},
			Version:              1,
			DisplayOrder:         10,
		},
		HotelFullSimulationOptionID,
		HotelFrontDeskFocusOptionID,
	)
}

func singleRoleScenario(
	definition ScenarioDefinition,
	config ScenarioConfig,
	role RoleDefinition,
	fullSimulationOptionID string,
	focusOptionID string,
) catalogScenario {
	return catalogScenario{
		definition: definition,
		config:     config,
		roles:      []RoleDefinition{role},
		practiceOptions: []PracticeOptionDefinition{
			{
				ID:                   fullSimulationOptionID,
				ScenarioDefinitionID: definition.ID,
				Type:                 PracticeOptionFullSimulation,
				DisplayName:          "完整模拟",
				Version:              1,
				DisplayOrder:         10,
			},
			{
				ID:                   focusOptionID,
				ScenarioDefinitionID: definition.ID,
				RoleDefinitionID:     role.ID,
				Type:                 PracticeOptionFocus,
				DisplayName:          "重点练习",
				Version:              1,
				DisplayOrder:         20,
			},
		},
	}
}
