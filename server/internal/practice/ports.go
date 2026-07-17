package practice

import (
	"context"
	"time"
)

// 创建场次时从 Preparation 一次性读取全部准备数据
type SessionPreparation struct {
	ScenarioDefinition ScenarioDefinitionSnapshot
	ScenarioConfig     ScenarioConfigSnapshot
	Preparation        PreparationSnapshot
	Role               RoleSnapshot
	InterviewerSubject SubjectRef
	PracticeOption     PracticeOptionSnapshot
	PracticeFocuses    []string
	Mode               PracticeMode
	TargetObjectiveIDs []string
}

// Practice 仅通过此端口读取其他模块的数据
type PreparationReader interface {
	ValidatePlan(context.Context, CreatePracticePlanCommand) error
	PrepareSession(context.Context, *PracticePlan, string, string) (SessionPreparation, error)
}

// PlanRepository 管理计划持久化，锁定方法只允许在事务内调用
type PlanRepository interface {
	// 原子创建计划；键已存在时返回首次创建的计划
	CreatePlan(context.Context, string, *PracticePlan) (*PracticePlan, bool, error)
	GetPlan(context.Context, string) (*PracticePlan, error)
	LockPlanForUpdate(context.Context, string) (*PracticePlan, error)
	SavePlan(context.Context, *PracticePlan) error
	HasActiveSession(context.Context, string) (bool, error)
	HasSessions(context.Context, string) (bool, error)
	DeletePlan(context.Context, string) error
}

// SessionRepository 原子保存场次及其不可变快照
type SessionRepository interface {
	// 原子创建场次和快照；键已存在时返回首次创建的场次和快照
	CreateActiveSession(context.Context, string, string, *PracticeSession, PracticeSessionSnapshot) (*PracticeSession, PracticeSessionSnapshot, bool, error)
	GetSession(context.Context, string) (*PracticeSession, error)
	LockSessionForUpdate(context.Context, string) (*PracticeSession, error)
	SaveSession(context.Context, *PracticeSession) error
	GetSnapshot(context.Context, string) (PracticeSessionSnapshot, error)
}

// TurnProgressRepository 保存 turn_id 对应的首次推进结果
type TurnProgressRepository interface {
	FindTurnAction(context.Context, string, string) (NextAction, bool, error)
	EffectiveTurnCount(context.Context, string) (int, error)
	SaveTurnProgress(context.Context, string, string, int, NextAction) error
}

// 保证回调中的读取、状态迁移和保存原子提交
type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type IDGenerator interface{ NewID() string }

type Clock interface{ Now() time.Time }
