package conversation

import "time"

// QuestionType 表示主问题或追问。
type QuestionType string

const (
	QuestionTypePrimary  QuestionType = "PRIMARY"
	QuestionTypeFollowUp QuestionType = "FOLLOW_UP"
)

// InteractionMode 表示回答采用的语音交互方式。
type InteractionMode string

const (
	InteractionModePushToTalk InteractionMode = "PUSH_TO_TALK"
	InteractionModeRealtime   InteractionMode = "REALTIME"
)

// TurnStatus 表示有效回答的处理状态。
type TurnStatus string

const (
	TurnStatusProcessing TurnStatus = "processing"
	TurnStatusCompleted  TurnStatus = "completed"
)

// AudioOwnerType 表示音频资产所属的 Conversation 实体类型。
type AudioOwnerType string

const (
	AudioOwnerTypeQuestion AudioOwnerType = "QUESTION"
	AudioOwnerTypeTurn     AudioOwnerType = "TURN"
)

// AudioStatus 表示音频资产的生命周期状态。
type AudioStatus string

const (
	AudioStatusPending AudioStatus = "pending"
	AudioStatusReady   AudioStatus = "ready"
	AudioStatusFailed  AudioStatus = "failed"
	AudioStatusDeleted AudioStatus = "deleted"
)

// AnswerValidity 保持与冻结的跨模块契约兼容，MS1 只产生 VALID 结果。
type AnswerValidity string

const (
	AnswerValidityValid   AnswerValidity = "VALID"
	AnswerValidityInvalid AnswerValidity = "INVALID"
)

// ObjectiveCoverage 表示回答对当前目标的覆盖程度。
type ObjectiveCoverage string

const (
	ObjectiveCoverageCovered          ObjectiveCoverage = "COVERED"
	ObjectiveCoveragePartiallyCovered ObjectiveCoverage = "PARTIALLY_COVERED"
	ObjectiveCoverageNotCovered       ObjectiveCoverage = "NOT_COVERED"
)

// ProcessingStage 表示 Conversation 内部处理阶段。
type ProcessingStage string

const (
	ProcessingStageTranscription         ProcessingStage = "transcription"
	ProcessingStageTurnOutcomeSubmission ProcessingStage = "turn_outcome_submission"
)

// ProcessingAttemptStatus 表示一次追加式处理尝试的状态。
type ProcessingAttemptStatus string

const (
	ProcessingAttemptStatusStarted   ProcessingAttemptStatus = "started"
	ProcessingAttemptStatusSucceeded ProcessingAttemptStatus = "succeeded"
	ProcessingAttemptStatusFailed    ProcessingAttemptStatus = "failed"
)

// Question 表示为 PracticeSession 生成的不可变问题，追问必须关联同场主问题。
type Question struct {
	QuestionID           string
	PracticeSessionID    string
	SpeakerParticipantID string
	ObjectiveID          *string
	QuestionType         QuestionType
	ParentQuestionID     *string
	Content              string
	Sequence             int
	AudioAssetID         *string
	CreatedAt            time.Time
}

// Turn 表示对 Question 的一次有效回答，空回答或转录失败不会创建 Turn。
type Turn struct {
	TurnID               string
	PracticeSessionID    string
	QuestionID           string
	SpeakerParticipantID string
	Sequence             int
	InteractionMode      InteractionMode
	AnswerText           string
	AudioAssetID         *string
	TurnStatus           TurnStatus
	SubmittedAt          time.Time
	CreatedAt            time.Time
	CompletedAt          *time.Time
}

// AudioAsset 保存音频业务元数据，实际内容由 Platform 文件存储能力管理。
//
// 转录前的回答音频通过 PendingAnswerQuestionID 关联问题；创建有效 Turn 后再绑定 Owner，并清除临时关联。
type AudioAsset struct {
	AudioAssetID            string
	OwnerType               *AudioOwnerType
	OwnerID                 *string
	PendingAnswerQuestionID *string
	DurationMS              int64
	StorageRef              *string
	AudioStatus             AudioStatus
	ContentType             *string
	Language                *string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// TurnOutcome 是有效 Turn 保存后提交给 Practice 的控制信号，TurnID 同时作为幂等键。
type TurnOutcome struct {
	TurnID                        string
	PracticeSessionID             string
	AnswerValidity                AnswerValidity
	ObjectiveCoverage             ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}

// Transcript 保存内部 ASR 结果，其他模块只使用 Turn.AnswerText。
type Transcript struct {
	TranscriptID string
	QuestionID   string
	TurnID       *string
	AudioAssetID string
	RawText      string
	Version      string
	Segments     []TranscriptSegment
	CreatedAt    time.Time
}

// TranscriptSegment 表示带音频时间范围的转录片段。
type TranscriptSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}

// ProcessingFailure 描述内部处理尝试的失败原因。
type ProcessingFailure struct {
	Code      string
	Message   string
	Retryable bool
	FailedAt  time.Time
}

// ProcessingAttempt 追加记录一次处理尝试，转录失败时允许 TurnID 为空。
type ProcessingAttempt struct {
	ProcessingAttemptID string
	QuestionID          string
	TurnID              *string
	AudioAssetID        *string
	Stage               ProcessingStage
	Status              ProcessingAttemptStatus
	AttemptNumber       int
	Failure             *ProcessingFailure
	StartedAt           time.Time
	FinishedAt          *time.Time
}
