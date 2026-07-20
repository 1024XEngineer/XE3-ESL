package practice

import "time"

type ScenarioType string

const ScenarioTypeInterview ScenarioType = "INTERVIEW"

type PracticePlanStatus string

const (
	PracticePlanConfiguring         PracticePlanStatus = "configuring"
	PracticePlanConfigurationFailed PracticePlanStatus = "configuration_failed"
	PracticePlanReady               PracticePlanStatus = "ready"
	PracticePlanArchived            PracticePlanStatus = "archived"
)

type PracticeSessionStatus string

const (
	PracticeSessionStarting   PracticeSessionStatus = "starting"
	PracticeSessionInProgress PracticeSessionStatus = "in_progress"
	PracticeSessionPaused     PracticeSessionStatus = "paused"
	PracticeSessionCompleted  PracticeSessionStatus = "completed"
	PracticeSessionEndedEarly PracticeSessionStatus = "ended_early"
)

type PracticePlan struct {
	ID                   string
	UserID               string
	ScenarioDefinitionID string
	ScenarioType         ScenarioType
	ScenarioConfigID     string
	PreparationProfileID string
	SelectedRoleIDs      []string
	Revision             int
	Status               PracticePlanStatus
}

// PracticeSession 只记录场次生命周期，练习上下文以 SnapshotID 指向的快照为准
type PracticeSession struct {
	ID           string
	PlanID       string
	ScenarioType ScenarioType
	SnapshotID   string
	Status       PracticeSessionStatus
	StartedAt    *time.Time
	EndedAt      *time.Time
	EndReason    string
}

// SubjectRef 用命名空间区分用户、虚拟角色等不同来源的参与者
type SubjectRef struct {
	Namespace string
	SubjectID string
}

// 以下快照复制自 Preparation，源数据后续变更不会影响已经创建的场次
type ScenarioDefinitionSnapshot struct {
	ScenarioDefinitionID string
	ScenarioType         ScenarioType
	Name                 string
	Version              int
	Status               string
}

type ScenarioConfigSnapshot struct {
	ScenarioConfigID     string
	ScenarioDefinitionID string
	ConfigType           string
	Version              int
}

type PreparationSnapshot struct {
	PreparationSnapshotID  string
	SourceProfileID        string
	SourceVersion          int
	ResumeSnapshot         string
	JobDescriptionSnapshot string
	BackgroundSnapshot     string
	CreatedAt              time.Time
}

type RoleSnapshot struct {
	RoleDefinitionID     string
	ScenarioDefinitionID string
	RoleType             string
	DisplayName          string
	Responsibilities     string
	Style                string
	FocusAreas           []string
	VoiceConfigRef       string
	Version              int
}

type PracticeOptionSnapshot struct {
	PracticeOptionID     string
	ScenarioDefinitionID string
	RoleDefinitionID     string
	PracticeOptionType   string
	DisplayName          string
	Version              int
}

type PracticeParticipant struct {
	ID               string
	SessionID        string
	SubjectRef       SubjectRef
	RoleDefinitionID string
	RoleSnapshot     RoleSnapshot
	ParticipantOrder int
}

// PracticeSessionSnapshot 是 Conversation 和 Review 读取场次上下文的稳定入口
// 场次创建后不再随计划或 Preparation 源数据变化
type PracticeSessionSnapshot struct {
	ID                 string
	SessionID          string
	PlanRevision       int
	ScenarioType       ScenarioType
	ScenarioDefinition ScenarioDefinitionSnapshot
	ScenarioConfig     ScenarioConfigSnapshot
	Preparation        PreparationSnapshot
	Participants       []PracticeParticipant
	PracticeOption     PracticeOptionSnapshot
	PracticeFocuses    []string
	SessionPolicy      PracticeSessionPolicy
	CreatedAt          time.Time
}
