// Package practice owns practice plans, sessions, and policy snapshots.
package practice

import "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreatePlanRequest struct {
	AgentThreadID             string   `json:"agent_thread_id"`
	MatterID                  string   `json:"matter_id,omitempty"`
	PreparationSnapshotID     string   `json:"preparation_snapshot_id,omitempty"`
	ScenarioDefinitionID      string   `json:"scenario_definition_id,omitempty"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version,omitempty"`
	ScenarioConfigID          string   `json:"scenario_config_id,omitempty"`
	ScenarioConfigVersion     int      `json:"scenario_config_version,omitempty"`
	PreparationProfileID      string   `json:"preparation_profile_id,omitempty"`
	SelectedRoleIDs           []string `json:"selected_role_ids,omitempty"`
	PracticeOptionID          string   `json:"practice_option_id,omitempty"`
	PracticeOptionVersion     int      `json:"practice_option_version,omitempty"`
	MaxEffectiveTurns         int      `json:"max_effective_turns,omitempty"`
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
	UserConfirmed         bool     `json:"user_confirmed"`
	PreparationSnapshotID string   `json:"preparation_snapshot_id,omitempty"`
	PracticeOptionID      string   `json:"practice_option_id,omitempty"`
	RoleDefinitionIDs     []string `json:"role_definition_ids,omitempty"`
}

type StartConfirmation struct {
	AgentThreadID        string
	PracticePlanID       string
	ExpectedPlanRevision int
}

type ConfirmAndStartResult struct {
	Bootstrap      persistence.ContextSessionBootstrap
	Replayed       bool
	ActiveConflict bool
}

type Backend interface {
	CreatePracticePlan(CreatePracticePlanCommand) (PracticePlan, error)
	PracticePlanExists(PracticePlanExistsQuery) bool
	CreatePracticeSession(CreatePracticeSessionCommand) (CreatePracticeSessionResult, error)
	GetPracticeSession(GetPracticeSessionQuery) (PracticeSession, bool)
	GetPracticeSessionSnapshot(GetPracticeSessionSnapshotQuery) (PracticeSessionSnapshot, bool)
	StartPracticeSession(StartPracticeSessionCommand) (StartPracticeSessionResult, error)
	AuthorizePracticeTurn(AuthorizePracticeTurnCommand) error
	ApplyTurnOutcome(ApplyTurnOutcomeCommand) (ApplyTurnOutcomeResult, error)
}

type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) CreatePracticePlan(command CreatePracticePlanCommand) (PracticePlan, error) {
	return s.backend.CreatePracticePlan(command)
}

func (s *Service) PracticePlanExists(query PracticePlanExistsQuery) bool {
	return s.backend.PracticePlanExists(query)
}

func (s *Service) CreatePracticeSession(
	command CreatePracticeSessionCommand,
) (CreatePracticeSessionResult, error) {
	return s.backend.CreatePracticeSession(command)
}

func (s *Service) GetPracticeSession(
	query GetPracticeSessionQuery,
) (PracticeSession, bool) {
	return s.backend.GetPracticeSession(query)
}

func (s *Service) GetPracticeSessionSnapshot(
	query GetPracticeSessionSnapshotQuery,
) (PracticeSessionSnapshot, bool) {
	return s.backend.GetPracticeSessionSnapshot(query)
}

func (s *Service) StartPracticeSession(
	command StartPracticeSessionCommand,
) (StartPracticeSessionResult, error) {
	return s.backend.StartPracticeSession(command)
}

func (s *Service) AuthorizePracticeTurn(command AuthorizePracticeTurnCommand) error {
	return s.backend.AuthorizePracticeTurn(command)
}

func (s *Service) ApplyTurnOutcome(
	command ApplyTurnOutcomeCommand,
) (ApplyTurnOutcomeResult, error) {
	return s.backend.ApplyTurnOutcome(command)
}
