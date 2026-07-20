package practice

import "context"

// PreparationReader 是 Practice 读取 Preparation 的唯一边界
// 这里只按稳定 ID 取快照，不承担候选项展示和源数据修改
type PreparationReader interface {
	GetScenarioDefinition(context.Context, string) (ScenarioDefinitionSnapshot, error)
	GetScenarioConfig(context.Context, string) (ScenarioConfigSnapshot, error)
	GetPreparationSnapshot(context.Context, string) (PreparationSnapshot, error)
	GetRoleDefinition(context.Context, string) (RoleSnapshot, error)
	GetPracticeOption(context.Context, string) (PracticeOptionSnapshot, error)
}
