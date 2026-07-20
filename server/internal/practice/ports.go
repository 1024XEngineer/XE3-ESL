package practice

import "context"

// PreparationReader 只暴露创建 Plan 和 Session 所需的准备数据
type PreparationReader interface {
	GetScenarioDefinition(context.Context, string) (ScenarioDefinitionSnapshot, error)
	GetScenarioConfig(context.Context, string) (ScenarioConfigSnapshot, error)
	GetPreparationSnapshot(context.Context, string) (PreparationSnapshot, error)
	GetRoleDefinition(context.Context, string) (RoleSnapshot, error)
	GetPracticeOption(context.Context, string) (PracticeOptionSnapshot, error)
}
