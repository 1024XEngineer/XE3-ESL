package practice

import "context"

type CreatePracticePlanCommand struct {
	UserID, ScenarioDefinitionID, ScenarioConfigID, PreparationProfileID string
	ScenarioType                                                         ScenarioType
	SelectedRoleIDs                                                      []string
	IdempotencyKey                                                       string
}

type UpdatePracticePlanCommand struct {
	UserID, PracticePlanID, ScenarioConfigID, PreparationProfileID string
	SelectedRoleIDs                                                []string
	ExpectedRevision                                               int
}

type CreatePracticeSessionCommand struct {
	UserID, PracticePlanID, ParticipantRoleID, PracticeOptionID, IdempotencyKey string
	PlanRevision                                                                int
}

type ApplyTurnOutcomeCommand struct {
	UserID  string
	Outcome TurnOutcome
}

// Service 声明 Practice 对内提供的应用能力，具体编排由后续实现提供
type Service interface {
	CreatePracticePlan(context.Context, CreatePracticePlanCommand) (*PracticePlan, error)
	RetryPlanConfiguration(context.Context, string, string, int) (*PracticePlan, error)
	UpdatePracticePlan(context.Context, UpdatePracticePlanCommand) (*PracticePlan, error)
	ArchivePracticePlan(context.Context, string, string, int) (*PracticePlan, error)
	RestorePracticePlan(context.Context, string, string, int) (*PracticePlan, error)
	DeleteEmptyPracticePlan(context.Context, string, string, int) error

	CreatePracticeSession(context.Context, CreatePracticeSessionCommand) (*PracticeSession, error)
	StartPracticeSession(context.Context, string, string) (*PracticeSession, error)
	PausePracticeSession(context.Context, string, string) (*PracticeSession, error)
	ResumePracticeSession(context.Context, string, string) (*PracticeSession, error)
	EndPracticeSessionEarly(context.Context, string, string, string) (*PracticeSession, error)

	GetPracticeSessionSnapshot(context.Context, string, string) (PracticeSessionSnapshot, error)
	ApplyTurnOutcome(context.Context, ApplyTurnOutcomeCommand) (NextAction, error)
}
