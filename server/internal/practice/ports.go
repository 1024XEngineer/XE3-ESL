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

// 创建和锁定方法承担并发约束，Service 不在进程内重复加锁
type Repository interface {
	// 在调用外部依赖前读取首次创建结果，保证重放不受后续状态变化影响
	FindPlanCreation(context.Context, string) (*PracticePlan, CreatePracticePlanCommand, bool, error)
	// 原子创建计划；键已存在时返回首次创建的计划
	CreatePlan(context.Context, string, CreatePracticePlanCommand, *PracticePlan) (*PracticePlan, CreatePracticePlanCommand, bool, error)
	GetPlan(context.Context, string) (*PracticePlan, error)
	// 在当前事务内锁定计划，直至事务结束
	LockPlanForUpdate(context.Context, string) (*PracticePlan, error)
	SavePlan(context.Context, *PracticePlan) error
	HasActiveSession(context.Context, string) (bool, error)
	HasSessions(context.Context, string) (bool, error)
	// 原子删除从未产生场次的计划
	DeletePlan(context.Context, string) error
	// 在准备场次前读取首次创建结果
	FindSessionCreation(context.Context, string) (*PracticeSession, PracticeSessionSnapshot, bool, error)
	// 原子创建场次和快照；键已存在时返回首次创建的场次和快照
	CreateActiveSession(context.Context, string, string, *PracticeSession, PracticeSessionSnapshot) (*PracticeSession, PracticeSessionSnapshot, bool, error)
	GetSession(context.Context, string) (*PracticeSession, error)
	// 在当前事务内锁定场次，直至事务结束
	LockSessionForUpdate(context.Context, string) (*PracticeSession, error)
	// 保存场次并在进入终态时释放计划的活动占位
	SaveSession(context.Context, *PracticeSession) error
	GetSnapshot(context.Context, string) (PracticeSessionSnapshot, error)
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
