package practice

type CreatePracticePlanCommand struct {
	AgentThreadID             string   `json:"agent_thread_id"`
	MatterID                  string   `json:"matter_id"`
	ScenarioDefinitionID      string   `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version"`
	ScenarioConfigID          string   `json:"scenario_config_id"`
	ScenarioConfigVersion     int      `json:"scenario_config_version"`
	PreparationProfileID      string   `json:"preparation_profile_id"`
	SelectedRoleIDs           []string `json:"selected_role_ids"`
}

type PracticePlanExistsQuery struct {
	PracticePlanID string
}

type CreatePracticeSessionCommand struct {
	PracticePlanID        string   `json:"-"`
	ExpectedPlanRevision  int      `json:"expected_plan_revision"`
	PreparationSnapshotID string   `json:"preparation_snapshot_id"`
	PracticeOptionID      string   `json:"practice_option_id"`
	RoleDefinitionIDs     []string `json:"role_definition_ids"`
}

type CreatePracticeSessionResult struct {
	Session  PracticeSession         `json:"practice_session"`
	Snapshot PracticeSessionSnapshot `json:"snapshot"`
}

type GetPracticeSessionQuery struct {
	PracticeSessionID string
}

type GetPracticeSessionSnapshotQuery struct {
	PracticeSessionID string
}

type StartPracticeSessionCommand struct {
	PracticeSessionID string
}

type StartPracticeSessionResult struct {
	SessionVersion int
	Started        bool
}

type AuthorizePracticeTurnCommand struct {
	PracticeSessionID string
	IsRetry           bool
}

type ApplyTurnOutcomeCommand struct {
	Outcome TurnOutcome
}

type ApplyTurnOutcomeResult struct {
	EffectiveTurns     int
	Completed          bool
	NextQuestionNumber int
	SessionVersion     int
	EndReason          PracticeSessionEndReason
	NextAction         NextAction
}
