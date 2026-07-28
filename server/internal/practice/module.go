// Package practice owns practice plans, sessions, and policy snapshots.
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreatePlanRequest struct {
	AgentThreadID             string   `json:"agent_thread_id"`
	MatterID                  string   `json:"matter_id"`
	PreparationSnapshotID     string   `json:"preparation_snapshot_id,omitempty"`
	ScenarioDefinitionID      string   `json:"scenario_definition_id,omitempty"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version,omitempty"`
	ScenarioConfigID          string   `json:"scenario_config_id,omitempty"`
	ScenarioConfigVersion     int      `json:"scenario_config_version,omitempty"`
	PreparationProfileID      string   `json:"preparation_profile_id,omitempty"`
	SelectedRoleIDs           []string `json:"selected_role_ids,omitempty"`
}

type UpdatePlanRequest struct {
	ExpectedPlanRevision  int      `json:"expected_plan_revision"`
	SelectedRoleIDs       []string `json:"selected_role_ids"`
	PracticeOptionID      string   `json:"practice_option_id"`
	PracticeOptionVersion int      `json:"practice_option_version"`
	MaxEffectiveTurns     int      `json:"max_effective_turns"`
}

type CreateSessionRequest struct {
	ExpectedPlanRevision  int      `json:"expected_plan_revision"`
	PreparationSnapshotID string   `json:"preparation_snapshot_id,omitempty"`
	PracticeOptionID      string   `json:"practice_option_id,omitempty"`
	RoleDefinitionIDs     []string `json:"role_definition_ids,omitempty"`
}

type TurnOutcome struct {
	SessionID string
	TurnID    string
	IsRetry   bool
}

type TurnDecision struct {
	EffectiveTurns     int
	Completed          bool
	NextQuestionNumber int
	SessionVersion     int
	EndReason          string
}

// Backend is the storage-facing boundary owned by Practice.
type Backend interface {
	CreatePlan(CreatePlanRequest) (map[string]any, error)
	PlanExists(string) bool
	CreateSession(string, CreateSessionRequest) (map[string]any, error)
	GetSession(string) (map[string]any, bool)
	GetSnapshot(string) (map[string]any, bool)
	StartSession(string) (int, bool, error)
	AuthorizeTurn(string, bool) error
	RecordTurnOutcome(TurnOutcome) (TurnDecision, error)
}

// Service is Practice's formal application-service entry point.
type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) CreatePlan(request CreatePlanRequest) (map[string]any, error) {
	return s.backend.CreatePlan(request)
}

func (s *Service) PlanExists(id string) bool {
	return s.backend.PlanExists(id)
}

func (s *Service) CreateSession(
	planID string,
	request CreateSessionRequest,
) (map[string]any, error) {
	return s.backend.CreateSession(planID, request)
}

func (s *Service) GetSession(id string) (map[string]any, bool) {
	return s.backend.GetSession(id)
}

func (s *Service) GetSnapshot(sessionID string) (map[string]any, bool) {
	return s.backend.GetSnapshot(sessionID)
}

func (s *Service) StartSession(sessionID string) (int, bool, error) {
	return s.backend.StartSession(sessionID)
}

func (s *Service) AuthorizeTurn(sessionID string, retry bool) error {
	return s.backend.AuthorizeTurn(sessionID, retry)
}

func (s *Service) RecordTurnOutcome(outcome TurnOutcome) (TurnDecision, error) {
	return s.backend.RecordTurnOutcome(outcome)
}
