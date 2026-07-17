package preparation

import "time"

// 来源类型，区分内置与用户自定义。
type Source string

const (
	SourceBuiltIn Source = "built_in" // 系统内置，如程序员面试场景
	SourceUser    Source = "user"     // 用户自行创建
)

// 练习场景定义，聚合角色规则、会话规则和评分维度。
type ScenarioDefinition struct {
	ID                     string
	Version                string
	Source                 Source
	Name                   string
	BackgroundRequirements BackgroundRequirements
	RolePolicy             RolePolicy
	SessionPolicy          SessionPolicy
	EvaluationRubric       EvaluationRubric
}

// 背景信息要求，声明某场景需要用户提供哪些字段。
type BackgroundRequirements struct {
	JobTitleRequired       bool
	JobDescriptionRequired bool
	ExperienceRequired     bool
	FocusAreasRequired     bool
}

// 角色数量约束。
type RolePolicy struct {
	MinRoles int
	MaxRoles int
}

// 回合数与追问约束。
type SessionPolicy struct {
	MinTurns      int
	MaxTurns      int
	AllowFollowUp bool
}

// 评分维度及版本。
type EvaluationRubric struct {
	Version    string
	Dimensions []string
}

// 角色定义，包含人设、目标、说话风格和行为约束。
type RoleDefinition struct {
	ID            string
	ScenarioID    string
	Version       string
	Source        Source
	Name          string
	Persona       string
	Goal          string
	SpeakingStyle string
	Constraints   []string
	VoiceID       string
}

// 简历，包含上传文件和解析后的文本。
type Resume struct {
	ID             string
	UserID         string
	FileStorageKey string
	FileName       string
	ParsedText     string
	Status         ResumeStatus
	CreatedAt      time.Time
}

// 简历解析状态。
type ResumeStatus string

const (
	ResumeStatusPending   ResumeStatus = "pending"   // 已上传，待解析
	ResumeStatusParsed    ResumeStatus = "parsed"    // 已解析，待确认
	ResumeStatusConfirmed ResumeStatus = "confirmed" // 已确认
)

// 候选人资料。
type Candidate struct {
	ID          string
	UserID      string
	DisplayName string
	Resumes     []string
}

// 背景快照，用户确认后不可变，被练习引用后不再修改。
type BackgroundSnapshot struct {
	ID                  string
	UserID              string
	JobTitle            string
	JobDescription      string
	ExperienceHighlight string
	FocusAreas          []string
	AdditionalNotes     string
	ResumeID            string
	ResumeStorageKey    string
	CandidateID         string
	CreatedAt           time.Time
}

// 场景只读快照，供练习模块引用，隔离后续编辑。
type ScenarioSnapshot struct {
	ScenarioID    string
	Version       string
	Name          string
	SessionPolicy SessionPolicy
	RolePolicy    RolePolicy
	Roles         []RoleSnapshot
}

// 角色只读快照。
type RoleSnapshot struct {
	RoleID        string
	ScenarioID    string
	Version       string
	Name          string
	Persona       string
	Goal          string
	SpeakingStyle string
	Constraints   []string
	VoiceID       string
}

// 简历只读快照。
type ResumeSnapshot struct {
	ResumeID       string
	UserID         string
	FileStorageKey string
	FileName       string
	ParsedText     string
	Status         ResumeStatus
}
