package practice

import "time"

// 场景分支使用稳定判别值；MS1 只开放面试场景
type ScenarioType string

const (
	ScenarioTypeInterview ScenarioType = "INTERVIEW"
)

// 区分场次中的系统职责，不等同于具体角色定义
type ParticipantRole string

const (
	ParticipantRoleInterviewer ParticipantRole = "INTERVIEWER"
	ParticipantRoleCandidate   ParticipantRole = "CANDIDATE"
)

// 只保存跨模块定位主体所需的信息，不复制主体详情
type SubjectRef struct {
	Namespace string
	SubjectID string
}

// 计划状态只能通过 PracticePlan 提供的方法迁移
type PracticePlanStatus string

const (
	PracticePlanConfiguring         PracticePlanStatus = "configuring"
	PracticePlanConfigurationFailed PracticePlanStatus = "configuration_failed"
	PracticePlanReady               PracticePlanStatus = "ready"
	PracticePlanArchived            PracticePlanStatus = "archived"
)

// 场次进入终态后不可恢复，迁移规则由 PracticeSession 统一校验
type PracticeSessionStatus string

const (
	PracticeSessionStarting   PracticeSessionStatus = "starting"
	PracticeSessionInProgress PracticeSessionStatus = "in_progress"
	PracticeSessionPaused     PracticeSessionStatus = "paused"
	PracticeSessionCompleted  PracticeSessionStatus = "completed"
	PracticeSessionEndedEarly PracticeSessionStatus = "ended_early"
)

// 保存可跨场次复用的配置；已有活动场次时不能修改或归档
type PracticePlan struct {
	id                   string
	userID               string
	scenarioDefinitionID string
	scenarioType         ScenarioType
	scenarioConfigID     string
	preparationProfileID string
	selectedRoleIDs      []string
	revision             int
	status               PracticePlanStatus
}

// 校验创建参数，并以第一个修订版进入配置中状态
func NewPracticePlan(id, userID, scenarioDefinitionID string, scenarioType ScenarioType, scenarioConfigID, preparationProfileID string, selectedRoleIDs []string) (*PracticePlan, error) {
	if id == "" || userID == "" || scenarioDefinitionID == "" || scenarioType != ScenarioTypeInterview || scenarioConfigID == "" || preparationProfileID == "" || !hasOneToFourNonEmptyValues(selectedRoleIDs) {
		return nil, ErrPracticePlanInvalid
	}

	return &PracticePlan{
		id:                   id,
		userID:               userID,
		scenarioDefinitionID: scenarioDefinitionID,
		scenarioType:         scenarioType,
		scenarioConfigID:     scenarioConfigID,
		preparationProfileID: preparationProfileID,
		selectedRoleIDs:      cloneStrings(selectedRoleIDs),
		revision:             1,
		status:               PracticePlanConfiguring,
	}, nil
}

func (p *PracticePlan) ID() string { return p.id }

func (p *PracticePlan) UserID() string { return p.userID }

func (p *PracticePlan) ScenarioDefinitionID() string { return p.scenarioDefinitionID }

func (p *PracticePlan) ScenarioType() ScenarioType { return p.scenarioType }

func (p *PracticePlan) ScenarioConfigID() string { return p.scenarioConfigID }

func (p *PracticePlan) PreparationProfileID() string { return p.preparationProfileID }

// 返回副本，避免调用方绕过计划更新规则修改角色
func (p *PracticePlan) SelectedRoleIDs() []string { return cloneStrings(p.selectedRoleIDs) }

func (p *PracticePlan) Revision() int { return p.revision }

func (p *PracticePlan) Status() PracticePlanStatus { return p.status }

// 只接受尚未结束配置的计划
func (p *PracticePlan) MarkConfigurationFailed() error {
	if p.status != PracticePlanConfiguring {
		return ErrPracticePlanTransitionNotAllowed
	}
	p.status = PracticePlanConfigurationFailed
	return nil
}

// 允许首次配置成功，也允许失败后重试成功
func (p *PracticePlan) MarkReady() error {
	if p.status != PracticePlanConfiguring && p.status != PracticePlanConfigurationFailed {
		return ErrPracticePlanTransitionNotAllowed
	}
	p.status = PracticePlanReady
	return nil
}

// 拒绝修改存在活动场次的计划；成功后修订号递增
func (p *PracticePlan) Update(scenarioConfigID, preparationProfileID string, selectedRoleIDs []string, hasActiveSession bool) error {
	if hasActiveSession {
		return ErrPracticePlanHasActiveSession
	}
	if p.status != PracticePlanReady {
		return ErrPracticePlanTransitionNotAllowed
	}
	if scenarioConfigID == "" || preparationProfileID == "" || !hasOneToFourNonEmptyValues(selectedRoleIDs) {
		return ErrPracticePlanInvalid
	}
	p.scenarioConfigID = scenarioConfigID
	p.preparationProfileID = preparationProfileID
	p.selectedRoleIDs = cloneStrings(selectedRoleIDs)
	p.revision++
	return nil
}

// 拒绝归档存在活动场次或尚未就绪的计划
func (p *PracticePlan) Archive(hasActiveSession bool) error {
	if hasActiveSession {
		return ErrPracticePlanHasActiveSession
	}
	if p.status != PracticePlanReady {
		return ErrPracticePlanTransitionNotAllowed
	}
	p.status = PracticePlanArchived
	return nil
}

// 只恢复已归档的计划
func (p *PracticePlan) Restore() error {
	if p.status != PracticePlanArchived {
		return ErrPracticePlanTransitionNotAllowed
	}
	p.status = PracticePlanReady
	return nil
}

// 只保存运行状态；创建场次时的输入由独立快照冻结
type PracticeSession struct {
	id           string
	planID       string
	scenarioType ScenarioType
	snapshotID   string
	status       PracticeSessionStatus
	createdAt    time.Time
	startedAt    *time.Time
	endedAt      *time.Time
	endReason    string
}

// 校验关联标识和创建时间，初始状态为启动中
func NewPracticeSession(id, planID string, scenarioType ScenarioType, snapshotID string, createdAt time.Time) (*PracticeSession, error) {
	if id == "" || planID == "" || scenarioType != ScenarioTypeInterview || snapshotID == "" || createdAt.IsZero() {
		return nil, ErrPracticeSessionInvalid
	}
	return &PracticeSession{id: id, planID: planID, scenarioType: scenarioType, snapshotID: snapshotID, status: PracticeSessionStarting, createdAt: createdAt}, nil
}

func (s *PracticeSession) ID() string { return s.id }

func (s *PracticeSession) PlanID() string { return s.planID }

func (s *PracticeSession) ScenarioType() ScenarioType { return s.scenarioType }

func (s *PracticeSession) SnapshotID() string { return s.snapshotID }

func (s *PracticeSession) Status() PracticeSessionStatus { return s.status }

// 通过第二个返回值区分“尚未开始”和零值时间
func (s *PracticeSession) StartedAt() (time.Time, bool) {
	if s.startedAt == nil {
		return time.Time{}, false
	}
	return *s.startedAt, true
}

// 拒绝早于场次创建时间的开始时间
func (s *PracticeSession) Start(startedAt time.Time) error {
	if s.status != PracticeSessionStarting {
		return ErrPracticeSessionTransitionNotAllowed
	}
	if startedAt.Before(s.createdAt) {
		return ErrPracticeSessionInvalidTime
	}
	s.startedAt = timePointer(startedAt)
	s.status = PracticeSessionInProgress
	return nil
}

// 只允许暂停进行中的场次
func (s *PracticeSession) Pause() error {
	if s.status != PracticeSessionInProgress {
		return ErrPracticeSessionTransitionNotAllowed
	}
	s.status = PracticeSessionPaused
	return nil
}

// 只允许恢复暂停中的场次
func (s *PracticeSession) Resume() error {
	if s.status != PracticeSessionPaused {
		return ErrPracticeSessionTransitionNotAllowed
	}
	s.status = PracticeSessionInProgress
	return nil
}

// 只允许结束已经开始且仍在进行的场次
func (s *PracticeSession) Complete(endedAt time.Time) error {
	return s.end(PracticeSessionCompleted, endedAt, "")
}

// 接受进行中或暂停中的场次，并要求记录原因
func (s *PracticeSession) EndEarly(endedAt time.Time, reason string) error {
	if reason == "" {
		return ErrPracticeSessionInvalid
	}
	return s.end(PracticeSessionEndedEarly, endedAt, reason)
}

func (s *PracticeSession) end(status PracticeSessionStatus, endedAt time.Time, reason string) error {
	if s.status != PracticeSessionInProgress && (status != PracticeSessionEndedEarly || s.status != PracticeSessionPaused) {
		return ErrPracticeSessionTransitionNotAllowed
	}
	if s.startedAt == nil || endedAt.Before(*s.startedAt) {
		return ErrPracticeSessionInvalidTime
	}
	s.endedAt = timePointer(endedAt)
	s.endReason = reason
	s.status = status
	return nil
}

// 保留场次创建时使用的场景定义版本
type ScenarioDefinitionSnapshot struct {
	ScenarioDefinitionID string
	ScenarioType         ScenarioType
	Name                 string
	Version              int
	Status               string
}

// 保留场次创建时使用的场景配置版本
type ScenarioConfigSnapshot struct {
	ScenarioConfigID     string
	ScenarioDefinitionID string
	ConfigType           string
	Version              int
}

// 固化生成本场练习所依据的用户准备资料
type PreparationSnapshot struct {
	PreparationSnapshotID  string
	SourceProfileID        string
	SourceVersion          int
	ResumeSnapshot         string
	JobDescriptionSnapshot string
	BackgroundSnapshot     string
	CreatedAt              time.Time
}

// 固化面试官在本场练习中的职责、风格和声音配置
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

// 固化本场练习采用的选项版本
type PracticeOptionSnapshot struct {
	PracticeOptionID     string
	ScenarioDefinitionID string
	RoleDefinitionID     string
	PracticeOptionType   string
	DisplayName          string
	Version              int
}

type PracticeParticipantInput struct {
	ID               string
	SessionID        string
	ParticipantRole  ParticipantRole
	SubjectRef       SubjectRef
	RoleDefinitionID string
	RoleSnapshot     RoleSnapshot
	ParticipantOrder int
}

// 将主体引用与本场采用的角色版本绑定，后续角色变更不影响历史场次
type PracticeParticipant struct {
	id               string
	sessionID        string
	participantRole  ParticipantRole
	subjectRef       SubjectRef
	roleDefinitionID string
	roleSnapshot     RoleSnapshot
	participantOrder int
}

func (p PracticeParticipant) ID() string { return p.id }

func (p PracticeParticipant) SessionID() string { return p.sessionID }

func (p PracticeParticipant) ParticipantRole() ParticipantRole { return p.participantRole }

func (p PracticeParticipant) SubjectRef() SubjectRef { return p.subjectRef }

func (p PracticeParticipant) RoleDefinitionID() string { return p.roleDefinitionID }

// 返回副本，避免调用方修改快照中的切片字段
func (p PracticeParticipant) RoleSnapshot() RoleSnapshot { return cloneRoleSnapshot(p.roleSnapshot) }

func (p PracticeParticipant) ParticipantOrder() int { return p.participantOrder }

type PracticeSessionSnapshotInput struct {
	ID                 string
	SessionID          string
	PlanRevision       int
	ScenarioType       ScenarioType
	ScenarioDefinition ScenarioDefinitionSnapshot
	ScenarioConfig     ScenarioConfigSnapshot
	Preparation        PreparationSnapshot
	Participants       []PracticeParticipantInput
	PracticeOption     PracticeOptionSnapshot
	PracticeFocuses    []string
	SessionPolicy      PracticeSessionPolicy
	CreatedAt          time.Time
}

// 快照保存场次创建时的输入，后续读取不依赖源模块当前状态
type PracticeSessionSnapshot struct {
	id                 string
	sessionID          string
	planRevision       int
	scenarioType       ScenarioType
	scenarioDefinition ScenarioDefinitionSnapshot
	scenarioConfig     ScenarioConfigSnapshot
	preparation        PreparationSnapshot
	participants       []PracticeParticipant
	practiceOption     PracticeOptionSnapshot
	practiceFocuses    []string
	sessionPolicy      PracticeSessionPolicy
	createdAt          time.Time
}

// 校验各快照来自同一场景和场次，并深拷贝可变字段
func NewPracticeSessionSnapshot(input PracticeSessionSnapshotInput) (PracticeSessionSnapshot, error) {
	if !validSnapshotInput(input) {
		return PracticeSessionSnapshot{}, ErrPracticeSessionSnapshotInvalid
	}
	participants := make([]PracticeParticipant, len(input.Participants))
	for index, participantInput := range input.Participants {
		participants[index] = PracticeParticipant{
			id: participantInput.ID, sessionID: participantInput.SessionID,
			participantRole: participantInput.ParticipantRole, subjectRef: participantInput.SubjectRef,
			roleDefinitionID: participantInput.RoleDefinitionID,
			roleSnapshot:     cloneRoleSnapshot(participantInput.RoleSnapshot), participantOrder: participantInput.ParticipantOrder,
		}
	}
	return PracticeSessionSnapshot{
		id: input.ID, sessionID: input.SessionID, planRevision: input.PlanRevision, scenarioType: input.ScenarioType,
		scenarioDefinition: input.ScenarioDefinition, scenarioConfig: input.ScenarioConfig,
		preparation: input.Preparation, participants: participants, practiceOption: input.PracticeOption,
		practiceFocuses: cloneStrings(input.PracticeFocuses), sessionPolicy: input.SessionPolicy, createdAt: input.CreatedAt,
	}, nil
}

// 按参与者顺序返回非空的角色定义标识
func (s PracticeSessionSnapshot) ParticipantRoleIDs() []string {
	result := make([]string, 0, len(s.participants))
	for _, participant := range s.participants {
		if participant.RoleDefinitionID() != "" {
			result = append(result, participant.RoleDefinitionID())
		}
	}
	return result
}

// 返回副本，避免调用方修改快照状态
func (s PracticeSessionSnapshot) PracticeFocuses() []string { return cloneStrings(s.practiceFocuses) }

func (s PracticeSessionSnapshot) ID() string { return s.id }

func (s PracticeSessionSnapshot) ScenarioType() ScenarioType { return s.scenarioType }

func (s PracticeSessionSnapshot) ScenarioDefinition() ScenarioDefinitionSnapshot {
	return s.scenarioDefinition
}

func (s PracticeSessionSnapshot) ScenarioConfig() ScenarioConfigSnapshot { return s.scenarioConfig }

func (s PracticeSessionSnapshot) Preparation() PreparationSnapshot { return s.preparation }

// 深拷贝参与者集合及其中的角色快照
func (s PracticeSessionSnapshot) Participants() []PracticeParticipantInput {
	result := make([]PracticeParticipantInput, len(s.participants))
	for i, participant := range s.participants {
		result[i] = PracticeParticipantInput{
			ID: participant.ID(), SessionID: participant.SessionID(), ParticipantRole: participant.ParticipantRole(), SubjectRef: participant.SubjectRef(),
			RoleDefinitionID: participant.RoleDefinitionID(), RoleSnapshot: participant.RoleSnapshot(), ParticipantOrder: participant.ParticipantOrder(),
		}
	}
	return result
}

func (s PracticeSessionSnapshot) PreparationSnapshotID() string {
	return s.preparation.PreparationSnapshotID
}

func (s PracticeSessionSnapshot) PracticeOptionID() string { return s.practiceOption.PracticeOptionID }

func (s PracticeSessionSnapshot) PracticeOption() PracticeOptionSnapshot { return s.practiceOption }

func (s PracticeSessionSnapshot) PlanRevision() int { return s.planRevision }

func (s PracticeSessionSnapshot) SessionPolicy() PracticeSessionPolicy { return s.sessionPolicy }

func validSnapshotInput(input PracticeSessionSnapshotInput) bool {
	if input.ID == "" || input.SessionID == "" || input.PlanRevision < 1 || input.ScenarioType != ScenarioTypeInterview || input.CreatedAt.IsZero() || !validInterviewParticipants(input.SessionID, input.Participants) {
		return false
	}
	return input.ScenarioDefinition.ScenarioDefinitionID != "" && input.ScenarioDefinition.ScenarioType == input.ScenarioType && input.ScenarioDefinition.Version > 0 &&
		input.ScenarioConfig.ScenarioConfigID != "" && input.ScenarioConfig.ScenarioDefinitionID == input.ScenarioDefinition.ScenarioDefinitionID && input.ScenarioConfig.Version > 0 &&
		input.Preparation.PreparationSnapshotID != "" && input.Preparation.SourceProfileID != "" && input.Preparation.SourceVersion > 0 && !input.Preparation.CreatedAt.IsZero() &&
		input.PracticeOption.PracticeOptionID != "" && input.PracticeOption.ScenarioDefinitionID == input.ScenarioDefinition.ScenarioDefinitionID && input.PracticeOption.Version > 0 &&
		input.Participants[0].RoleSnapshot.ScenarioDefinitionID == input.ScenarioDefinition.ScenarioDefinitionID &&
		input.PracticeOption.RoleDefinitionID == input.Participants[0].RoleDefinitionID && input.SessionPolicy.valid()
}

func validInterviewParticipants(sessionID string, participants []PracticeParticipantInput) bool {
	if len(participants) != 1 {
		return false
	}
	roles := map[ParticipantRole]int{}
	participantIDs := map[string]struct{}{}
	subjects := map[SubjectRef]struct{}{}
	for _, participant := range participants {
		if participant.ID == "" || participant.SessionID != sessionID || participant.ParticipantRole == "" || participant.SubjectRef.Namespace == "" || participant.SubjectRef.SubjectID == "" || participant.ParticipantOrder < 1 {
			return false
		}
		if _, exists := participantIDs[participant.ID]; exists {
			return false
		}
		participantIDs[participant.ID] = struct{}{}
		if _, exists := subjects[participant.SubjectRef]; exists {
			return false
		}
		subjects[participant.SubjectRef] = struct{}{}
		roles[participant.ParticipantRole]++
		if participant.ParticipantRole == ParticipantRoleInterviewer {
			if participant.RoleDefinitionID == "" || participant.RoleDefinitionID != participant.RoleSnapshot.RoleDefinitionID || participant.RoleSnapshot.Version < 1 {
				return false
			}
		} else if participant.RoleDefinitionID != participant.RoleSnapshot.RoleDefinitionID || (participant.RoleDefinitionID != "" && participant.RoleSnapshot.Version < 1) {
			return false
		}
	}
	return roles[ParticipantRoleInterviewer] == 1 && len(roles) == 1
}

func cloneRoleSnapshot(snapshot RoleSnapshot) RoleSnapshot {
	snapshot.FocusAreas = cloneStrings(snapshot.FocusAreas)
	return snapshot
}

func hasOneNonEmptyValue(values []string) bool {
	return len(values) == 1 && hasNonEmptyValues(values)
}

func hasOneToFourNonEmptyValues(values []string) bool {
	return len(values) >= 1 && len(values) <= 4 && hasNonEmptyValues(values)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func timePointer(value time.Time) *time.Time { return &value }
