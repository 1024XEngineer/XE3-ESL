package practice

import "context"

type CreatePracticePlanCommand struct {
	UserID               string
	ScenarioDefinitionID string
	ScenarioType         ScenarioType
	ScenarioConfigID     string
	PreparationProfileID string
	SelectedRoleIDs      []string
	IdempotencyKey       string
}

type UpdatePracticePlanCommand struct {
	UserID               string
	PracticePlanID       string
	ScenarioConfigID     string
	PreparationProfileID string
	SelectedRoleIDs      []string
	ExpectedRevision     int
}

type RetryPracticePlanConfigurationCommand struct {
	UserID           string
	PracticePlanID   string
	ExpectedRevision int
}

type ArchivePracticePlanCommand struct {
	UserID           string
	PracticePlanID   string
	ExpectedRevision int
}

type RestorePracticePlanCommand struct {
	UserID           string
	PracticePlanID   string
	ExpectedRevision int
}

type DeleteEmptyPracticePlanCommand struct {
	UserID           string
	PracticePlanID   string
	ExpectedRevision int
}

type GetPracticePlanQuery struct {
	UserID         string
	PracticePlanID string
}

type ListPracticePlansQuery struct {
	UserID string
}

type CreatePracticeSessionCommand struct {
	UserID            string
	PracticePlanID    string
	PlanRevision      int
	ParticipantRoleID string
	PracticeOptionID  string
	IdempotencyKey    string
}

type StartPracticeSessionCommand struct {
	UserID            string
	PracticeSessionID string
}

type PausePracticeSessionCommand struct {
	UserID            string
	PracticeSessionID string
}

type ResumePracticeSessionCommand struct {
	UserID            string
	PracticeSessionID string
}

type EndPracticeSessionEarlyCommand struct {
	UserID            string
	PracticeSessionID string
	Reason            string
}

type GetActivePracticeSessionQuery struct {
	UserID         string
	PracticePlanID string
}

type GetPracticeSessionSnapshotQuery struct {
	PracticeSessionID string
}

type ApplyTurnOutcomeCommand struct {
	Outcome TurnOutcome
}

// PlanService 负责计划的配置与归档，不暴露场次推进能力
type PlanService interface {
	CreatePracticePlan(context.Context, CreatePracticePlanCommand) (PracticePlan, error)
	RetryPracticePlanConfiguration(context.Context, RetryPracticePlanConfigurationCommand) (PracticePlan, error)
	UpdatePracticePlan(context.Context, UpdatePracticePlanCommand) (PracticePlan, error)
	ArchivePracticePlan(context.Context, ArchivePracticePlanCommand) (PracticePlan, error)
	RestorePracticePlan(context.Context, RestorePracticePlanCommand) (PracticePlan, error)
	DeleteEmptyPracticePlan(context.Context, DeleteEmptyPracticePlanCommand) error
	GetPracticePlan(context.Context, GetPracticePlanQuery) (PracticePlan, error)
	ListPracticePlans(context.Context, ListPracticePlansQuery) ([]PracticePlan, error)
}

// SessionService 是 Conversation 和上层入口控制练习场次的边界
type SessionService interface {
	CreatePracticeSession(context.Context, CreatePracticeSessionCommand) (PracticeSession, error)
	StartPracticeSession(context.Context, StartPracticeSessionCommand) (PracticeSession, error)
	PausePracticeSession(context.Context, PausePracticeSessionCommand) (PracticeSession, error)
	ResumePracticeSession(context.Context, ResumePracticeSessionCommand) (PracticeSession, error)
	EndPracticeSessionEarly(context.Context, EndPracticeSessionEarlyCommand) (PracticeSession, error)
	GetActivePracticeSession(context.Context, GetActivePracticeSessionQuery) (PracticeSession, error)
	GetPracticeSessionSnapshot(context.Context, GetPracticeSessionSnapshotQuery) (PracticeSessionSnapshot, error)
	ApplyTurnOutcome(context.Context, ApplyTurnOutcomeCommand) (NextAction, error)
}

// Service 供需要完整 Practice 能力的组合根使用
// 普通调用方应优先依赖 PlanService 或 SessionService
type Service interface {
	PlanService
	SessionService
}
