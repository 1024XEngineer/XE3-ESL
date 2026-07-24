// Package practice owns practice plans, sessions, and policy snapshots.
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreatePlanRequest struct {
	ScenarioDefinitionID      string   `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version"`
	ScenarioConfigID          string   `json:"scenario_config_id"`
	ScenarioConfigVersion     int      `json:"scenario_config_version"`
	PreparationProfileID      string   `json:"preparation_profile_id"`
	SelectedRoleIDs           []string `json:"selected_role_ids"`
}

type CreateSessionRequest struct {
	ExpectedPlanRevision  int      `json:"expected_plan_revision"`
	PreparationSnapshotID string   `json:"preparation_snapshot_id"`
	PracticeOptionID      string   `json:"practice_option_id"`
	RoleDefinitionIDs     []string `json:"role_definition_ids"`
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
