package preparation

const (
	IELTSSpeakingPart2ScenarioID    = "scn_ielts_speaking_part_2"
	IELTSSpeakingFullMockScenarioID = "scn_ielts_speaking_full"
	WorkplaceProgressRiskScenarioID = "scn_workplace_progress_risk_update"
	DailyHotelCheckinScenarioID     = "scn_daily_hotel_checkin_issue"

	IELTSSpeakingPart2ConfigID    = "scfg_ielts_speaking_part_2"
	IELTSSpeakingFullMockConfigID = "scfg_ielts_speaking_full"
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

func ieltsSpeakingFullMockCatalogDefinition() catalogScenario {
	const roleID = "role_ielts_speaking_full_counterpart"
	return singleRoleScenario(
		ScenarioDefinition{
			ID:               IELTSSpeakingFullMockScenarioID,
			Type:             ScenarioFamilyExam,
			Model:            ScenarioModelIELTSSpeakingFullMock,
			Name:             "IELTS 口语完整模拟",
			Version:          2,
			Status:           ScenarioStatusActive,
			TurnPolicyRef:    "generic.practice.turn.v1",
			SessionPolicyRef: "generic.practice.session.v1",
			DisplayOrder:     40,
		},
		ScenarioConfig{
			ID:                   IELTSSpeakingFullMockConfigID,
			ScenarioDefinitionID: IELTSSpeakingFullMockScenarioID,
			Type:                 ScenarioFamilyExam,
			Model:                ScenarioModelIELTSSpeakingFullMock,
			Version:              2,
			PromptModel: ScenarioPromptModel{
				PublicSceneBrief: "按 Part 1、Part 2、Part 3 连续完成一轮 IELTS 口语完整模考。",
				PracticeGoal:     "适应真实三段式流程，并在不同题型中保持连贯自然的表达。",
				UserRole:         "考生",
				AIRole:           "IELTS 口语考官",
				PersonaSummary:   "A neutral IELTS speaking examiner who follows the frozen three-part mock-test sequence, asks exactly one item at a time, and never teaches or scores during the simulation.",
				FocusAreas: []string{
					"part_1_familiar_topics",
					"part_2_long_turn",
					"part_3_discussion",
					"section_transition",
				},
				TurnBlueprints: []string{
					"Part 1 question: Where is your hometown?",
					"Part 1 question: Is there anything you do not like about your hometown?",
					"Part 1 question: Would you say it is a good place for young people?",
					"Part 1 question: Do you use artificial intelligence in your daily life?",
					"Part 1 question: Has technology changed the way you learn things?",
					"Part 1 question: Is there any technology you find difficult to use?",
					"Part 1 question: What do you usually do in your free time?",
					"Part 1 question: Do you prefer spending your free time alone or with other people?",
					"Part 2 cue card: Describe a skill you would like to learn.\nYou should say:\n• What the skill is\n• Why you want to learn it\n• How you would learn it\n• And explain how learning this skill would benefit you",
					"Part 3 question: What kinds of skills are most valuable in today's society?",
					"Part 3 question: Some people say it is never too late to learn a new skill. Do you agree?",
					"Part 3 question: Do you think schools should focus more on practical skills?",
					"Part 3 question: How has technology changed the way people learn skills?",
					"Part 3 question: Do you think some skills will become obsolete in the future?",
				},
				SuggestedDurationSeconds: 900,
			},
		},
		RoleDefinition{
			ID:                   roleID,
			ScenarioDefinitionID: IELTSSpeakingFullMockScenarioID,
			Type:                 "IELTS_EXAMINER",
			DisplayName:          "IELTS 口语考官",
			Responsibilities:     "Run the frozen Part 1, Part 2, and Part 3 sequence without coaching or scoring.",
			Style:                "Neutral, concise, and exam appropriate.",
			FocusAreas: []string{
				"part_1_familiar_topics",
				"part_2_long_turn",
				"part_3_discussion",
			},
			Version:      2,
			DisplayOrder: 10,
		},
		"option_ielts_speaking_full_full",
		"option_ielts_speaking_full_focus",
	)
}

func ieltsSpeakingPart2CatalogDefinition() catalogScenario {
	return singleRoleScenario(
		ScenarioDefinition{
			ID:               IELTSSpeakingPart2ScenarioID,
			Type:             ScenarioFamilyExam,
			Model:            ScenarioModelIELTSSpeakingPart2,
			Name:             "IELTS Speaking Part 2",
			Version:          1,
			Status:           ScenarioStatusActive,
			TurnPolicyRef:    "ielts.speaking_part2.turn.v1",
			SessionPolicyRef: "ielts.speaking_part2.session.v1",
			DisplayOrder:     20,
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
			ID:               WorkplaceProgressRiskScenarioID,
			Type:             ScenarioFamilyWorkplace,
			Model:            ScenarioModelProgressAndRiskUpdate,
			Name:             "进度与风险汇报",
			Version:          1,
			Status:           ScenarioStatusActive,
			TurnPolicyRef:    "workplace.progress_risk_update.turn.v1",
			SessionPolicyRef: "workplace.progress_risk_update.session.v1",
			DisplayOrder:     10,
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
			ID:               DailyHotelCheckinScenarioID,
			Type:             ScenarioFamilyDaily,
			Model:            ScenarioModelHotelCheckinAndIssueHandling,
			Name:             "酒店入住与问题处理",
			Version:          1,
			Status:           ScenarioStatusActive,
			TurnPolicyRef:    "daily.hotel_checkin_issue.turn.v1",
			SessionPolicyRef: "daily.hotel_checkin_issue.session.v1",
			DisplayOrder:     50,
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

type basicScenarioSpec struct {
	slug         string
	name         string
	scene        string
	goal         string
	userRole     string
	aiRole       string
	roleType     string
	persona      string
	focusAreas   []string
	blueprints   []string
	duration     int
	displayOrder int
}

func basicInterviewCatalogDefinitions() []catalogScenario {
	return basicCatalogDefinitions(
		ScenarioFamilyInterview,
		ScenarioModelInterviewBasicDialogue,
		[]basicScenarioSpec{
			{
				slug:     "interview_self_introduction",
				name:     "英文自我介绍",
				scene:    "向招聘方做一段 60～90 秒的英文自我介绍，并围绕亮点继续交流。",
				goal:     "说清背景、优势和岗位匹配，并自然回应一到两个追问。",
				userRole: "候选人",
				aiRole:   "招聘方",
				roleType: "RECRUITER",
				persona:  "A warm recruiter who invites a concise introduction and follows up on one concrete strength without scoring the candidate.",
				focusAreas: []string{
					"background",
					"strength",
					"role_fit",
				},
				blueprints: []string{
					"邀请候选人做简短自我介绍",
					"围绕一个具体亮点自然追问",
					"澄清亮点与岗位的关联",
					"请候选人简短总结匹配度",
				},
				duration:     480,
				displayOrder: 10,
			},
			{
				slug:     "interview_recruiter_screening",
				name:     "招聘初筛",
				scene:    "与招聘专员讨论求职动机、岗位理解、基本条件和候选人反问。",
				goal:     "清楚表达动机与岗位理解，并完成一轮双向确认。",
				userRole: "候选人",
				aiRole:   "招聘专员",
				roleType: "RECRUITER",
				persona:  "A professional recruiter who keeps the screening focused on motivation, role understanding, practical conditions, and candidate questions.",
				focusAreas: []string{
					"motivation",
					"role_understanding",
					"conditions",
					"candidate_questions",
				},
				blueprints: []string{
					"询问候选人的求职动机",
					"澄清对岗位的理解",
					"确认一项基本求职条件",
					"邀请候选人提出一个问题",
				},
				duration:     600,
				displayOrder: 20,
			},
			{
				slug:     "interview_behavioral",
				name:     "行为面试",
				scene:    "围绕一个真实的协作、冲突、失败或成长经历进行行为面试。",
				goal:     "用具体情境、行动、结果和反思说明个人能力。",
				userRole: "候选人",
				aiRole:   "行为面试官",
				roleType: "BEHAVIORAL_INTERVIEWER",
				persona:  "A focused behavioral interviewer who explores one real example at a time and never invents candidate experience.",
				focusAreas: []string{
					"situation",
					"action",
					"result",
					"reflection",
				},
				blueprints: []string{
					"给出一个明确的行为主题",
					"澄清当时的情境与个人责任",
					"追问采取的具体行动和结果",
					"询问复盘与成长",
				},
				duration:     720,
				displayOrder: 40,
			},
			{
				slug:     "interview_system_design_spoken",
				name:     "系统设计口述",
				scene:    "用英语口述一个系统设计方案，从需求澄清逐步讨论架构、瓶颈和取舍。",
				goal:     "有条理地澄清需求、组织方案并解释关键技术取舍。",
				userRole: "候选人",
				aiRole:   "系统设计面试官",
				roleType: "SYSTEM_DESIGN_INTERVIEWER",
				persona:  "A technical interviewer who asks one system-design question at a time and probes the candidate's own architecture and trade-offs.",
				focusAreas: []string{
					"requirements",
					"architecture",
					"bottleneck",
					"tradeoff",
				},
				blueprints: []string{
					"给出一个边界清楚的系统设计任务",
					"请候选人澄清需求和规模",
					"围绕候选人的方案追问瓶颈",
					"讨论一个关键技术取舍",
				},
				duration:     900,
				displayOrder: 50,
			},
			{
				slug:     "interview_hiring_manager",
				name:     "Hiring Manager 面试",
				scene:    "与用人经理讨论岗位动机、业务影响、判断和跨团队协作方式。",
				goal:     "说明岗位匹配、高影响经历和做出判断的依据。",
				userRole: "候选人",
				aiRole:   "用人经理",
				roleType: "HIRING_MANAGER",
				persona:  "A concise hiring manager who probes role fit, judgment, collaboration, and measurable business impact.",
				focusAreas: []string{
					"role_fit",
					"business_impact",
					"judgment",
					"collaboration",
				},
				blueprints: []string{
					"询问候选人选择该岗位的原因",
					"追问一段高影响经历",
					"澄清关键判断和跨团队协作",
					"确认可衡量的结果",
				},
				duration:     720,
				displayOrder: 60,
			},
			{
				slug:     "interview_custom",
				name:     "自定义面试",
				scene:    "使用默认的通用岗位背景练习一个暂未被正式子场景覆盖的面试目标。",
				goal:     "保持面试语境，围绕用户补充的一句话目标进行自然问答。",
				userRole: "候选人",
				aiRole:   "通用面试官",
				roleType: "CUSTOM_INTERVIEWER",
				persona:  "A professional interviewer who follows the user's stated practice goal without claiming to reproduce a specific company's real questions.",
				focusAreas: []string{
					"custom_goal",
					"evidence",
					"clarification",
				},
				blueprints: []string{
					"根据用户目标提出一个明确问题",
					"回应用户刚才的实际回答",
					"追问一个最相关的证据或细节",
					"围绕目标自然收尾",
				},
				duration:     600,
				displayOrder: 70,
			},
		},
	)
}

func basicExamCatalogDefinitions() []catalogScenario {
	return basicCatalogDefinitions(
		ScenarioFamilyExam,
		ScenarioModelExamBasicDialogue,
		[]basicScenarioSpec{
			{
				slug:     "ielts_speaking_part_1",
				name:     "IELTS Speaking Part 1",
				scene:    "围绕个人经历和熟悉话题进行简短、自然的口语问答。",
				goal:     "直接回答问题，并用理由或细节自然展开。",
				userRole: "考生",
				aiRole:   "IELTS 口语考官",
				roleType: "IELTS_EXAMINER",
				persona:  "A neutral IELTS speaking examiner who asks short personal-topic questions and does not teach or score during the simulation.",
				focusAreas: []string{
					"direct_answer",
					"reason",
					"detail",
					"natural_extension",
				},
				blueprints: []string{
					"提出一个简短的熟悉话题问题",
					"根据回答追问一个理由",
					"追问一个具体细节或例子",
					"自然切换到相邻话题",
				},
				duration:     480,
				displayOrder: 10,
			},
			{
				slug:     "ielts_speaking_part_3",
				name:     "IELTS Speaking Part 3",
				scene:    "围绕一个社会性话题讨论观点、原因、例子、比较和影响。",
				goal:     "清楚表达抽象观点，并用理由和例子支持。",
				userRole: "考生",
				aiRole:   "IELTS 口语考官",
				roleType: "IELTS_EXAMINER",
				persona:  "A neutral IELTS speaking examiner who explores one abstract idea at a time and requests reasons, examples, or comparisons.",
				focusAreas: []string{
					"opinion",
					"reason",
					"example",
					"comparison",
				},
				blueprints: []string{
					"提出一个清楚的社会性观点问题",
					"追问观点背后的原因",
					"邀请考生给出例子或比较",
					"讨论可能的影响",
				},
				duration:     600,
				displayOrder: 30,
			},
			{
				slug:     "speaking_exam_custom",
				name:     "自定义口语考试",
				scene:    "使用默认考试设定，练习老师指定或其他考试形式的口语问题。",
				goal:     "围绕用户补充的考试名称、题型或目标完成自定义练习。",
				userRole: "考生",
				aiRole:   "口语考官",
				roleType: "CUSTOM_EXAMINER",
				persona:  "A neutral speaking examiner who follows the user's stated format while clearly treating it as custom practice.",
				focusAreas: []string{
					"custom_format",
					"clear_answer",
					"supporting_detail",
				},
				blueprints: []string{
					"根据用户目标提出一个考试式问题",
					"根据回答追问一个相关细节",
					"保持中立考官行为",
					"完成自定义练习收尾",
				},
				duration:     600,
				displayOrder: 50,
			},
		},
	)
}

func basicWorkplaceCatalogDefinitions() []catalogScenario {
	return basicCatalogDefinitions(
		ScenarioFamilyWorkplace,
		ScenarioModelWorkplaceBasicDialogue,
		[]basicScenarioSpec{
			{
				slug:     "workplace_meeting_disagreement",
				name:     "会议发言与表达异议",
				scene:    "在会议中针对当前方案清楚表达观点、不同意见和替代建议。",
				goal:     "说明立场与原因，并推动形成可执行的下一步。",
				userRole: "参会者",
				aiRole:   "会议主持人",
				roleType: "MEETING_FACILITATOR",
				persona:  "A constructive meeting facilitator who responds to the user's position and asks for one clear reason or alternative.",
				focusAreas: []string{
					"position",
					"reason",
					"alternative",
					"next_step",
				},
				blueprints: []string{
					"介绍当前讨论背景并邀请发言",
					"追问用户立场背后的原因",
					"请用户提出一个替代方案",
					"确认下一步行动",
				},
				duration:     600,
				displayOrder: 20,
			},
			{
				slug:     "workplace_cross_team_alignment",
				name:     "跨团队对齐与请求资源",
				scene:    "与合作团队负责人对齐目标、优先级、责任、依赖和资源需求。",
				goal:     "在时间或资源约束下推动双方形成清楚安排。",
				userRole: "项目负责人",
				aiRole:   "合作团队负责人",
				roleType: "PARTNER_TEAM_LEAD",
				persona:  "A partner-team lead with a realistic capacity constraint who seeks clear priorities, ownership, and an executable agreement.",
				focusAreas: []string{
					"goal",
					"priority",
					"dependency",
					"resource_request",
				},
				blueprints: []string{
					"带着一个时间或资源约束开场",
					"澄清共同目标和优先级",
					"确认责任与依赖",
					"形成可执行的资源安排",
				},
				duration:     720,
				displayOrder: 30,
			},
			{
				slug:     "workplace_feedback_conflict",
				name:     "提供反馈与处理冲突",
				scene:    "向同事说明一个具体事实、影响和期望，并共同确认下一步。",
				goal:     "清楚表达反馈，同时保持专业关系。",
				userRole: "同事或负责人",
				aiRole:   "需要反馈的同事",
				roleType: "COLLEAGUE",
				persona:  "A colleague who may accept, clarify, or defensively question feedback depending on the user's wording, while staying professional.",
				focusAreas: []string{
					"fact",
					"impact",
					"expectation",
					"next_step",
				},
				blueprints: []string{
					"邀请用户说明需要讨论的事实",
					"根据语气作出真实回应",
					"澄清影响和期望",
					"共同确认具体下一步",
				},
				duration:     600,
				displayOrder: 40,
			},
			{
				slug:     "workplace_client_delay",
				name:     "客户延期说明与需求澄清",
				scene:    "向客户解释交付限制、澄清需求并协商可行方案。",
				goal:     "说明影响和时间安排，避免含糊承诺。",
				userRole: "项目负责人",
				aiRole:   "客户",
				roleType: "CLIENT",
				persona:  "A concerned client who asks about impact, timing, and alternatives and does not accept vague commitments.",
				focusAreas: []string{
					"constraint",
					"impact",
					"timeline",
					"alternative",
				},
				blueprints: []string{
					"询问延期或需求变化的具体情况",
					"追问对业务和时间的影响",
					"要求一个可行替代方案",
					"确认新的承诺与边界",
				},
				duration:     720,
				displayOrder: 50,
			},
			{
				slug:     "workplace_solution_presentation",
				name:     "方案介绍与问答",
				scene:    "向领导、客户或评审者简洁介绍方案并回答价值、风险和落地问题。",
				goal:     "结构化介绍方案，并根据听众问题清楚回应。",
				userRole: "汇报人",
				aiRole:   "方案评审者",
				roleType: "SOLUTION_REVIEWER",
				persona:  "A pragmatic reviewer who listens to the user's proposal and asks the single most relevant question about value, risk, or execution.",
				focusAreas: []string{
					"value",
					"risk",
					"execution",
					"question_response",
				},
				blueprints: []string{
					"邀请用户简洁介绍方案",
					"根据介绍追问最相关的价值问题",
					"澄清一个风险或落地条件",
					"确认结论和下一步",
				},
				duration:     720,
				displayOrder: 60,
			},
			{
				slug:     "workplace_negotiation",
				name:     "条件协商",
				scene:    "与对手方澄清双方利益与底线，并尝试用交换条件形成方案。",
				goal:     "识别优先级，提出清楚的条件式方案。",
				userRole: "协商方",
				aiRole:   "对手方",
				roleType: "NEGOTIATION_COUNTERPART",
				persona:  "A realistic negotiation counterpart with one firm constraint who considers conditional trades but does not concede automatically.",
				focusAreas: []string{
					"interest",
					"priority",
					"constraint",
					"conditional_offer",
				},
				blueprints: []string{
					"给出初始立场和一个明确约束",
					"请用户澄清最重要的优先级",
					"回应用户提出的交换条件",
					"确认可接受的方案或分歧",
				},
				duration:     720,
				displayOrder: 70,
			},
			{
				slug:     "workplace_custom",
				name:     "自定义职场沟通",
				scene:    "使用默认工作关系和业务目标，练习其他真实工作交流。",
				goal:     "根据用户补充的对象、目标和边界保持专业对话。",
				userRole: "职场沟通者",
				aiRole:   "工作对象",
				roleType: "WORKPLACE_COUNTERPART",
				persona:  "A professional workplace counterpart who follows the user's stated relationship, business goal, and non-negotiable boundaries.",
				focusAreas: []string{
					"relationship",
					"business_goal",
					"boundary",
					"next_step",
				},
				blueprints: []string{
					"根据用户目标建立工作语境",
					"回应用户刚才的实际表达",
					"澄清一个业务边界",
					"推动形成具体下一步",
				},
				duration:     600,
				displayOrder: 80,
			},
		},
	)
}

func basicDailyCatalogDefinitions() []catalogScenario {
	return basicCatalogDefinitions(
		ScenarioFamilyDaily,
		ScenarioModelDailyBasicDialogue,
		[]basicScenarioSpec{
			{
				slug:     "daily_small_talk",
				name:     "自我介绍与寒暄",
				scene:    "与新朋友、邻居或同学自然打招呼、介绍自己并延续简短话题。",
				goal:     "自然开启话题并完成一段轻松的双向交流。",
				userRole: "自己",
				aiRole:   "新朋友",
				roleType: "NEW_ACQUAINTANCE",
				persona:  "A friendly new acquaintance who opens with a natural topic and responds conversationally without turning the exchange into an interview.",
				focusAreas: []string{
					"greeting",
					"self_introduction",
					"shared_topic",
				},
				blueprints: []string{
					"先打招呼并提供一个自然话题",
					"回应用户的自我介绍",
					"围绕共同点继续一句",
					"自然结束简短寒暄",
				},
				duration:     360,
				displayOrder: 10,
			},
			{
				slug:     "daily_restaurant_ordering",
				name:     "餐厅点餐",
				scene:    "在餐厅询问菜品、说明偏好、完成点餐并确认订单。",
				goal:     "理解服务员问题，清楚表达选择和饮食偏好。",
				userRole: "顾客",
				aiRole:   "服务员",
				roleType: "RESTAURANT_SERVER",
				persona:  "A helpful restaurant server who follows a realistic ordering flow and may explain availability, flavors, or set-menu options.",
				focusAreas: []string{
					"menu_question",
					"preference",
					"order",
					"confirmation",
				},
				blueprints: []string{
					"欢迎顾客并询问是否需要介绍",
					"回应菜品问题或说明一个选择",
					"确认偏好和具体点单",
					"复述并确认订单",
				},
				duration:     480,
				displayOrder: 20,
			},
			{
				slug:     "daily_shopping_return",
				name:     "购物、换货与退款",
				scene:    "在商店询价、比较商品，或说明换货退款问题并协商处理方式。",
				goal:     "清楚说明需求与事实，理解门店规则和可行选项。",
				userRole: "顾客",
				aiRole:   "店员",
				roleType: "STORE_ASSISTANT",
				persona:  "A realistic store assistant who asks relevant questions and offers options within a clear store policy.",
				focusAreas: []string{
					"product_question",
					"issue",
					"store_policy",
					"resolution",
				},
				blueprints: []string{
					"询问顾客需要购买还是处理售后",
					"澄清商品或问题细节",
					"说明一个合理门店规则",
					"确认可行处理方案",
				},
				duration:     480,
				displayOrder: 30,
			},
			{
				slug:     "daily_airport_transport",
				name:     "机场与交通",
				scene:    "向工作人员或司机问路、确认时间，并处理一个行程变化。",
				goal:     "清楚说明目的地和时间需求，确认交通或票务信息。",
				userRole: "旅客",
				aiRole:   "交通工作人员",
				roleType: "TRANSPORT_STAFF",
				persona:  "A concise transport staff member who provides necessary information and clarifies destination, timing, or ticket details.",
				focusAreas: []string{
					"destination",
					"time",
					"route",
					"travel_change",
				},
				blueprints: []string{
					"询问旅客的目的地或问题",
					"澄清时间或票务信息",
					"提供一个必要路线或选择",
					"确认旅客理解下一步",
				},
				duration:     480,
				displayOrder: 40,
			},
			{
				slug:     "daily_rental_maintenance",
				name:     "租房与维修",
				scene:    "向房东、物业或维修人员描述故障、约定时间并确认责任。",
				goal:     "准确描述问题，协商可行时间和后续安排。",
				userRole: "租客",
				aiRole:   "物业工作人员",
				roleType: "PROPERTY_STAFF",
				persona:  "A practical property staff member who asks for fault details, has realistic scheduling limits, and confirms responsibility and follow-up.",
				focusAreas: []string{
					"fault_description",
					"availability",
					"responsibility",
					"follow_up",
				},
				blueprints: []string{
					"请租客描述具体故障",
					"澄清影响和可进入时间",
					"提出一个现实的维修时段",
					"确认责任和后续安排",
				},
				duration:     480,
				displayOrder: 60,
			},
			{
				slug:     "daily_medical_appointment",
				name:     "医疗预约",
				scene:    "与医疗前台预约时间、描述基础情况并确认准备事项。",
				goal:     "完成基础预约沟通，不把练习当作医疗诊断。",
				userRole: "患者",
				aiRole:   "医疗前台",
				roleType: "MEDICAL_RECEPTIONIST",
				persona:  "A medical receptionist who handles scheduling and basic preparation only, never diagnoses, and directs emergencies to real urgent care.",
				focusAreas: []string{
					"appointment_reason",
					"availability",
					"time_confirmation",
					"preparation",
				},
				blueprints: []string{
					"询问预约原因和是否紧急",
					"澄清可用时间",
					"提供并确认一个预约时段",
					"说明基础准备事项",
				},
				duration:     420,
				displayOrder: 70,
			},
			{
				slug:     "daily_phone_call",
				name:     "电话沟通",
				scene:    "通过电话向客服、机构或联系人说明身份、目的并确认信息。",
				goal:     "在看不到对方时清楚表达，并自然请求重复或复述确认。",
				userRole: "来电者",
				aiRole:   "电话接听者",
				roleType: "PHONE_CONTACT",
				persona:  "A clear phone contact who confirms identity and purpose, and naturally requests repetition when information is unclear.",
				focusAreas: []string{
					"identity",
					"purpose",
					"clarification",
					"confirmation",
				},
				blueprints: []string{
					"接听电话并询问来电目的",
					"澄清一项关键信息",
					"必要时请求重复或复述",
					"确认处理结果",
				},
				duration:     420,
				displayOrder: 80,
			},
			{
				slug:     "daily_social_invitation",
				name:     "社交邀请与礼貌拒绝",
				scene:    "与朋友或同事发出、回应或礼貌拒绝一项邀请。",
				goal:     "表达意愿和原因，并在需要时协商替代时间。",
				userRole: "自己",
				aiRole:   "朋友或同事",
				roleType: "FRIEND_OR_COLLEAGUE",
				persona:  "A friendly contact who responds naturally to invitations and is open to an alternative time or activity.",
				focusAreas: []string{
					"invitation",
					"response",
					"polite_reason",
					"alternative",
				},
				blueprints: []string{
					"提出或回应一个自然邀请",
					"询问时间或意愿",
					"回应接受或礼貌拒绝",
					"协商替代安排",
				},
				duration:     360,
				displayOrder: 90,
			},
			{
				slug:     "daily_complaint_help",
				name:     "投诉与求助",
				scene:    "向服务人员清楚说明问题、提出诉求并确认解决办法。",
				goal:     "有条理地说明事实与期望，理解可行范围。",
				userRole: "顾客或旅客",
				aiRole:   "服务人员",
				roleType: "SERVICE_STAFF",
				persona:  "A calm service staff member who clarifies facts and realistic limits before offering a solution.",
				focusAreas: []string{
					"problem",
					"fact",
					"request",
					"resolution",
				},
				blueprints: []string{
					"请用户说明遇到的问题",
					"澄清一个关键事实",
					"确认用户的具体诉求",
					"提供并确认可行方案",
				},
				duration:     480,
				displayOrder: 100,
			},
			{
				slug:     "daily_custom",
				name:     "自定义日常交流",
				scene:    "使用默认生活场景，练习其他日常地点、身份和交流目标。",
				goal:     "根据用户补充的信息保持自然日常角色和对话边界。",
				userRole: "日常沟通者",
				aiRole:   "日常交流对象",
				roleType: "DAILY_COUNTERPART",
				persona:  "A natural everyday conversation partner who follows the stated place, relationship, and goal without shifting into a job interview or exam.",
				focusAreas: []string{
					"place",
					"relationship",
					"goal",
					"confirmation",
				},
				blueprints: []string{
					"根据用户目标建立日常语境",
					"回应用户刚才的实际表达",
					"澄清一个必要细节",
					"自然完成交流",
				},
				duration:     420,
				displayOrder: 110,
			},
		},
	)
}

func basicCatalogDefinitions(
	family ScenarioFamily,
	model ScenarioModel,
	specs []basicScenarioSpec,
) []catalogScenario {
	result := make([]catalogScenario, 0, len(specs))
	for _, spec := range specs {
		scenarioID := "scn_" + spec.slug
		roleID := "role_" + spec.slug + "_counterpart"
		result = append(result, singleRoleScenario(
			ScenarioDefinition{
				ID:               scenarioID,
				Type:             family,
				Model:            model,
				Name:             spec.name,
				Version:          1,
				Status:           ScenarioStatusActive,
				TurnPolicyRef:    "generic.practice.turn.v1",
				SessionPolicyRef: "generic.practice.session.v1",
				DisplayOrder:     spec.displayOrder,
			},
			ScenarioConfig{
				ID:                   "scfg_" + spec.slug,
				ScenarioDefinitionID: scenarioID,
				Type:                 family,
				Model:                model,
				Version:              1,
				PromptModel: ScenarioPromptModel{
					PublicSceneBrief:         spec.scene,
					PracticeGoal:             spec.goal,
					UserRole:                 spec.userRole,
					AIRole:                   spec.aiRole,
					PersonaSummary:           spec.persona,
					FocusAreas:               spec.focusAreas,
					TurnBlueprints:           spec.blueprints,
					SuggestedDurationSeconds: spec.duration,
				},
			},
			RoleDefinition{
				ID:                   roleID,
				ScenarioDefinitionID: scenarioID,
				Type:                 spec.roleType,
				DisplayName:          spec.aiRole,
				Responsibilities:     spec.goal,
				Style:                "Natural, concise, and appropriate to the current role.",
				FocusAreas:           spec.focusAreas,
				Version:              1,
				DisplayOrder:         10,
			},
			"option_"+spec.slug+"_full",
			"option_"+spec.slug+"_focus",
		))
	}
	return result
}
