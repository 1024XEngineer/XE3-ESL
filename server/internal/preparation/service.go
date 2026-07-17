package preparation

import "context"

// 业务服务，方法体待后续接入仓库后补充。
type Service struct {
	scenarios   ScenarioRepository
	roles       RoleRepository
	resumes     ResumeRepository
	backgrounds BackgroundRepository
}

func NewService(s ScenarioRepository, r RoleRepository, m ResumeRepository, b BackgroundRepository) *Service {
	return &Service{scenarios: s, roles: r, resumes: m, backgrounds: b}
}

// --- 场景 ---

// 返回所有场景。
func (s *Service) ListScenarios(ctx context.Context) ([]*ScenarioDefinition, error) {
	return nil, nil
}

// 按编号返回单个场景。
func (s *Service) GetScenario(ctx context.Context, scenarioID string) (*ScenarioDefinition, error) {
	return nil, nil
}

// --- 角色 ---

// 返回某场景下的全部角色。
func (s *Service) ListRoles(ctx context.Context, scenarioID string) ([]*RoleDefinition, error) {
	return nil, nil
}

// 按编号返回单个角色。
func (s *Service) GetRole(ctx context.Context, roleID string) (*RoleDefinition, error) {
	return nil, nil
}

// 更新用户自定义角色；内置角色不可修改。
func (s *Service) UpdateRole(ctx context.Context, role *RoleDefinition) (*RoleDefinition, error) {
	return nil, nil
}

// --- 简历 ---

// 保存上传的简历并进入解析流程。
func (s *Service) CreateResume(ctx context.Context, resume *Resume) (*Resume, error) {
	return nil, nil
}

// 按编号返回简历。
func (s *Service) GetResume(ctx context.Context, id string) (*Resume, error) {
	return nil, nil
}

// 返回某用户的全部简历。
func (s *Service) ListResumes(ctx context.Context, userID string) ([]*Resume, error) {
	return nil, nil
}

// 删除简历；历史快照不受影响。
func (s *Service) DeleteResume(ctx context.Context, id string) error {
	return nil
}

// --- 背景快照 ---

// 创建不可变的背景快照。
func (s *Service) CreateBackground(ctx context.Context, snapshot *BackgroundSnapshot) (*BackgroundSnapshot, error) {
	return nil, nil
}

// 按编号返回背景快照。
func (s *Service) GetBackground(ctx context.Context, id string) (*BackgroundSnapshot, error) {
	return nil, nil
}

// 返回某用户的全部背景快照。
func (s *Service) ListBackgrounds(ctx context.Context, userID string) ([]*BackgroundSnapshot, error) {
	return nil, nil
}

// 修改未被引用的背景快照；已被引用时应新建而非覆盖。
func (s *Service) UpdateBackground(ctx context.Context, snapshot *BackgroundSnapshot) (*BackgroundSnapshot, error) {
	return nil, nil
}

// 删除背景快照；历史练习不受影响。
func (s *Service) DeleteBackground(ctx context.Context, id string) error {
	return nil
}

// --- 跨模块只读 ---

// 返回场景只读快照，供练习模块使用。
func (s *Service) GetScenarioSnapshot(ctx context.Context, scenarioID, version string) (ScenarioSnapshot, error) {
	return ScenarioSnapshot{}, nil
}

// 返回角色只读快照，供练习模块使用。
func (s *Service) GetRoleSnapshot(ctx context.Context, roleID string) (RoleSnapshot, error) {
	return RoleSnapshot{}, nil
}
