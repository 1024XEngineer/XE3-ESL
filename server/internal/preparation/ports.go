package preparation

import "context"

// 场景仓库，持久化场景定义，当前只提供查询。
type ScenarioRepository interface {
	FindByID(ctx context.Context, id string) (*ScenarioDefinition, error)
	List(ctx context.Context) ([]*ScenarioDefinition, error)
}

// 角色仓库，持久化角色定义；内置角色不可修改，仅用户角色可保存。
type RoleRepository interface {
	FindByScenario(ctx context.Context, scenarioID string) ([]*RoleDefinition, error)
	FindByID(ctx context.Context, id string) (*RoleDefinition, error)
	Save(ctx context.Context, role *RoleDefinition) error
}

// 简历仓库，持久化用户上传的简历。
type ResumeRepository interface {
	Save(ctx context.Context, resume *Resume) error
	FindByID(ctx context.Context, id string) (*Resume, error)
	FindByUser(ctx context.Context, userID string) ([]*Resume, error)
	Delete(ctx context.Context, id string) error
}

// 背景快照仓库，快照不可变，故无更新方法。
type BackgroundRepository interface {
	Save(ctx context.Context, snapshot *BackgroundSnapshot) error
	FindByID(ctx context.Context, id string) (*BackgroundSnapshot, error)
	FindByUser(ctx context.Context, userID string) ([]*BackgroundSnapshot, error)
	Delete(ctx context.Context, id string) error
}

// 跨模块只读接口，返回快照以隔离后续编辑，供练习模块调用。
type ScenarioReader interface {
	GetScenarioSnapshot(ctx context.Context, scenarioID, version string) (ScenarioSnapshot, error)
	GetRoleSnapshot(ctx context.Context, roleID string) (RoleSnapshot, error)
}
