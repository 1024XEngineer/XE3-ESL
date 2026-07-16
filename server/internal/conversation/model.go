package conversation

import "time"

// QuestionType identifies whether a question starts an objective or follows up
// on a primary question.
type QuestionType string

const (
	QuestionTypePrimary  QuestionType = "PRIMARY"
	QuestionTypeFollowUp QuestionType = "FOLLOW_UP"
)

// InteractionMode identifies how an answer was captured.
type InteractionMode string

const (
	InteractionModePushToTalk InteractionMode = "PUSH_TO_TALK"
	InteractionModeRealtime   InteractionMode = "REALTIME"
)

// TurnStatus tracks whether an effective answer is still being submitted to
// Practice or has been accepted.
type TurnStatus string

const (
	TurnStatusProcessing TurnStatus = "processing"
	TurnStatusCompleted  TurnStatus = "completed"
)

// AudioOwnerType identifies the Conversation entity that owns an audio asset.
type AudioOwnerType string

const (
	AudioOwnerTypeQuestion AudioOwnerType = "QUESTION"
	AudioOwnerTypeTurn     AudioOwnerType = "TURN"
)

// AudioStatus tracks the lifecycle of audio metadata managed by Conversation.
type AudioStatus string

const (
	AudioStatusPending AudioStatus = "pending"
	AudioStatusReady   AudioStatus = "ready"
	AudioStatusFailed  AudioStatus = "failed"
	AudioStatusDeleted AudioStatus = "deleted"
)

// AnswerValidity is kept compatible with the frozen cross-module contract.
// Conversation only produces VALID outcomes in MS1.
type AnswerValidity string

const (
	AnswerValidityValid   AnswerValidity = "VALID"
	AnswerValidityInvalid AnswerValidity = "INVALID"
)

// ObjectiveCoverage describes how well an answer covers the current objective.
type ObjectiveCoverage string

const (
	ObjectiveCoverageCovered          ObjectiveCoverage = "COVERED"
	ObjectiveCoveragePartiallyCovered ObjectiveCoverage = "PARTIALLY_COVERED"
	ObjectiveCoverageNotCovered       ObjectiveCoverage = "NOT_COVERED"
)

// ProcessingStage identifies an internal Conversation processing step.
type ProcessingStage string

const (
	ProcessingStageTranscription         ProcessingStage = "transcription"
	ProcessingStageTurnOutcomeSubmission ProcessingStage = "turn_outcome_submission"
)

// ProcessingAttemptStatus records the result of one append-only processing
// attempt.
type ProcessingAttemptStatus string

const (
	ProcessingAttemptStatusStarted   ProcessingAttemptStatus = "started"
	ProcessingAttemptStatusSucceeded ProcessingAttemptStatus = "succeeded"
	ProcessingAttemptStatusFailed    ProcessingAttemptStatus = "failed"
)

// Question is an immutable interview question generated for a PracticeSession.
// A FOLLOW_UP question references a PRIMARY question in the same session.
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

// Turn is one effective answer to a Question. Recording fragments, connection
// events, empty answers, and failed transcription attempts do not create Turns.
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

// AudioAsset contains business metadata for audio whose bytes are stored by a
// Platform file-storage capability. StorageRef is a stable internal reference,
// not a public URL or storage credential.
//
// Question TTS and completed answer audio have an OwnerType and OwnerID. An
// answer uploaded before transcription has no owner yet and instead references
// its Question through PendingAnswerQuestionID. After a valid Turn is created,
// the asset is bound to that Turn and PendingAnswerQuestionID is cleared.
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

// TurnOutcome is the idempotent control signal submitted to Practice after a
// valid Turn is saved. TurnID is its idempotency key.
type TurnOutcome struct {
	TurnID                        string
	PracticeSessionID             string
	AnswerValidity                AnswerValidity
	ObjectiveCoverage             ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}

// Transcript stores the internal ASR result. Other modules consume
// Turn.AnswerText instead of depending on this structure.
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

// TranscriptSegment is a time-bounded part of a Transcript.
type TranscriptSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}

// ProcessingFailure describes why an internal processing attempt failed.
type ProcessingFailure struct {
	Code      string
	Message   string
	Retryable bool
	FailedAt  time.Time
}

// ProcessingAttempt is an append-only record of a Conversation processing
// attempt. A transcription failure can be recorded before a Turn exists by
// using QuestionID and AudioAssetID with a nil TurnID.
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
