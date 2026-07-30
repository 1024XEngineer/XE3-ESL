package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

type MessageModality string

const (
	MessageModalityText       MessageModality = "text"
	MessageModalityVoice      MessageModality = "voice"
	MessageModalityMultimodal MessageModality = "multimodal"
)

type MessageAudioStatus string

const (
	MessageAudioReadable MessageAudioStatus = "readable"
	MessageAudioDeleting MessageAudioStatus = "deleting"
	MessageAudioDeleted  MessageAudioStatus = "deleted"
)

type Thread struct {
	ID             string
	OwnerID        string
	Title          string
	ActiveMatterID string
	NextMessageSeq int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ThreadMatterLink struct {
	OwnerID   string
	ThreadID  string
	MatterID  string
	Active    bool
	LinkedAt  time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID              string
	OwnerID         string
	ThreadID        string
	Sequence        int64
	Role            MessageRole
	ClientMessageID string
	ProducedByRunID string
	Modality        MessageModality
	Content         string
	Audio           *MessageAudio
	CreatedAt       time.Time
}

// MessageAudio is the durable one-to-one recording projection for a user
// AgentMessage. ObjectKey remains server-only.
type MessageAudio struct {
	ID             string
	OwnerID        string
	ThreadID       string
	MessageID      string
	CandidateID    string
	ObjectKey      string
	ContentType    string
	Size           int64
	ChecksumSHA256 string
	Duration       time.Duration
	SampleRate     int
	ETag           string
	Status         MessageAudioStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      time.Time
}

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

const (
	RunFailureInterrupted        = "interrupted"
	RunFailureConfigurationDrift = "configuration_drift"
	RunFailureInvalidContext     = "invalid_context"
	RunFailureInternal           = "internal_error"
)

type Run struct {
	ID                   string
	OwnerID              string
	ThreadID             string
	InputMessageID       string
	Attempt              int
	RetryOfRunID         string
	RetryClientID        string
	Status               RunStatus
	RequestedProvider    string
	RequestedModel       string
	MaxOutputTokens      int
	MaxInputCharacters   int
	WorkerLeaseToken     string
	WorkerLeaseExpiresAt time.Time
	AssistantMessageID   string
	ProviderCompletionID string
	ProviderModel        string
	FinishReason         string
	Usage                ai.TokenUsage
	FailureKind          string
	FailureRetryable     bool
	CreatedAt            time.Time
	StartedAt            time.Time
	CompletedAt          time.Time
	UpdatedAt            time.Time
}

type ToolCallStatus string

const (
	ToolCallStatusProposed  ToolCallStatus = "proposed"
	ToolCallStatusRunning   ToolCallStatus = "running"
	ToolCallStatusSucceeded ToolCallStatus = "succeeded"
	ToolCallStatusFailed    ToolCallStatus = "failed"
	ToolCallStatusRejected  ToolCallStatus = "rejected"
)

type ToolCallRecord struct {
	ID            string
	RunID         string
	OwnerID       string
	ThreadID      string
	Name          string
	SchemaVersion string
	Input         json.RawMessage
	Status        ToolCallStatus
	Result        json.RawMessage
	ErrorCategory string
	RequestID     string
	SourceRefs    []ToolSourceRef
	ProposedAt    time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	UpdatedAt     time.Time
}

type ToolSourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ContextMessageSource struct {
	MessageID string      `json:"message_id"`
	Sequence  int64       `json:"sequence"`
	Role      MessageRole `json:"role"`
}

type ContextMemorySource struct {
	MemoryID               string  `json:"memory_id"`
	MemoryVersion          int64   `json:"memory_version"`
	Type                   string  `json:"type"`
	Scope                  string  `json:"scope"`
	MatterID               string  `json:"matter_id,omitempty"`
	Similarity             float64 `json:"similarity"`
	Score                  float64 `json:"score"`
	EmbeddingProvider      string  `json:"embedding_provider"`
	EmbeddingModel         string  `json:"embedding_model"`
	EmbeddingDimensions    int     `json:"embedding_dimensions"`
	EmbeddingPolicyVersion string  `json:"embedding_policy_version"`
	RetrievalPolicyVersion string  `json:"retrieval_policy_version"`
}

type ContextStableProfileSource struct {
	MemoryID      string `json:"memory_id"`
	MemoryVersion int64  `json:"memory_version"`
	CanonicalKey  string `json:"canonical_key"`
	Type          string `json:"type"`
	Scope         string `json:"scope"`
}

type ContextSummarySource struct {
	CheckpointID           string `json:"checkpoint_id"`
	SourceFromSequence     int64  `json:"source_from_sequence"`
	CoveredThroughSequence int64  `json:"covered_through_sequence"`
	PolicyVersion          string `json:"policy_version"`
	PromptVersion          string `json:"prompt_version"`
	Provider               string `json:"provider"`
	Model                  string `json:"model"`
}

type ContextManifest struct {
	RunID                             string
	OwnerID                           string
	ThreadID                          string
	InputMessageID                    string
	ActiveMatterID                    string
	ActiveMatterVersion               int64
	InstructionVersion                string
	StableProfileContextPolicyVersion string
	SelectedStableProfile             []ContextStableProfileSource
	MemoryContextPolicyVersion        string
	SelectedMemories                  []ContextMemorySource
	SummaryContextPolicyVersion       string
	SummaryContextStatus              string
	SelectedSummary                   *ContextSummarySource
	SelectedMessages                  []ContextMessageSource
	OmittedMessageCount               int
	TrimReason                        string
	MaxInputCharacters                int
	UsedInputCharacters               int
	RequestedProvider                 string
	RequestedModel                    string
	MaxOutputTokens                   int
	ExposedTools                      []string
	ToolSchemaHashes                  map[string]string
	CreatedAt                         time.Time
}

type RunConfiguration struct {
	Provider           string
	Model              string
	MaxOutputTokens    int
	MaxInputCharacters int
}

type RunSubmission struct {
	Run         Run
	UserMessage Message
	Created     bool
}

type RunRetry struct {
	Run     Run
	Created bool
}

type ThreadPageCursor struct {
	UpdatedAt time.Time
	ThreadID  string
}

type MessagePageCursor struct {
	ThreadID       string
	BeforeSequence int64
}

type ThreadPage struct {
	Threads         []Thread
	FocusedThreadID string
	NextCursor      string
}

type MessagePage struct {
	Messages   []Message
	NextCursor string
}

type Repository interface {
	CreateThread(
		ctx context.Context,
		ownerID string,
		activeMatterID string,
	) (Thread, error)
	ListThreads(ctx context.Context, ownerID string) ([]Thread, error)
	PageThreads(
		ctx context.Context,
		ownerID string,
		limit int,
		before *ThreadPageCursor,
	) ([]Thread, error)
	FindThread(ctx context.Context, ownerID, threadID string) (Thread, error)
	FindFocusedThread(
		ctx context.Context,
		ownerID string,
	) (Thread, bool, error)
	SetFocusedThread(
		ctx context.Context,
		ownerID string,
		threadID string,
	) (Thread, error)
	ClearFocusedThread(ctx context.Context, ownerID string) error
	SetActiveMatter(
		ctx context.Context,
		ownerID string,
		threadID string,
		matterID string,
	) (ThreadMatterLink, error)
	AppendUserMessage(
		ctx context.Context,
		ownerID string,
		threadID string,
		clientMessageID string,
		content string,
	) (Message, error)
	ListMessages(
		ctx context.Context,
		ownerID string,
		threadID string,
	) ([]Message, error)
	PageMessages(
		ctx context.Context,
		ownerID string,
		threadID string,
		limit int,
		before *MessagePageCursor,
	) ([]Message, error)
}

type Application interface {
	CreateThread(
		ctx context.Context,
		actor requestcontext.Actor,
		activeMatterID string,
	) (Thread, error)
	ListThreads(
		ctx context.Context,
		actor requestcontext.Actor,
	) ([]Thread, error)
	PageThreads(
		ctx context.Context,
		actor requestcontext.Actor,
		pageSize int,
		cursor string,
	) (ThreadPage, error)
	GetThread(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
	) (Thread, error)
	GetFocusedThread(
		ctx context.Context,
		actor requestcontext.Actor,
	) (Thread, bool, error)
	SetFocusedThread(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
	) (Thread, error)
	ClearFocusedThread(
		ctx context.Context,
		actor requestcontext.Actor,
	) error
	SetActiveMatter(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		matterID string,
	) (ThreadMatterLink, error)
	AppendUserMessage(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		clientMessageID string,
		content string,
	) (Message, error)
	ListMessages(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
	) ([]Message, error)
	PageMessages(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		pageSize int,
		cursor string,
	) (MessagePage, error)
}

type RunRepository interface {
	CreateInitialRun(
		ctx context.Context,
		ownerID string,
		threadID string,
		clientMessageID string,
		content string,
		configuration RunConfiguration,
	) (RunSubmission, error)
	CreateRetryRun(
		ctx context.Context,
		ownerID string,
		runID string,
		retryClientID string,
		configuration RunConfiguration,
	) (RunRetry, error)
	ClaimRun(
		ctx context.Context,
		ownerID string,
		runID string,
	) (Run, bool, error)
	FindRun(ctx context.Context, ownerID, runID string) (Run, error)
	FindMessage(
		ctx context.Context,
		ownerID string,
		threadID string,
		messageID string,
	) (Message, error)
	SaveContextManifest(
		ctx context.Context,
		manifest ContextManifest,
	) (ContextManifest, error)
	FindContextManifest(
		ctx context.Context,
		ownerID string,
		runID string,
	) (ContextManifest, error)
	SaveContextToolSnapshot(
		ctx context.Context,
		manifest ContextManifest,
	) (ContextManifest, error)
	SaveToolCallProposed(
		ctx context.Context,
		record ToolCallRecord,
	) (ToolCallRecord, error)
	MarkToolCallRunning(
		ctx context.Context,
		ownerID string,
		runID string,
		toolCallID string,
		requestID string,
	) (ToolCallRecord, error)
	MarkToolCallSucceeded(
		ctx context.Context,
		ownerID string,
		runID string,
		toolCallID string,
		result json.RawMessage,
		sourceRefs []ToolSourceRef,
	) (ToolCallRecord, error)
	MarkToolCallFailed(
		ctx context.Context,
		ownerID string,
		runID string,
		toolCallID string,
		status ToolCallStatus,
		errorCategory string,
	) (ToolCallRecord, error)
	ListToolCalls(
		ctx context.Context,
		ownerID string,
		runID string,
	) ([]ToolCallRecord, error)
	CompleteRun(
		ctx context.Context,
		ownerID string,
		runID string,
		workerLeaseToken string,
		content string,
		result ai.TextResult,
	) (Run, error)
	FailRun(
		ctx context.Context,
		ownerID string,
		runID string,
		workerLeaseToken string,
		failureKind string,
		retryable bool,
	) (Run, error)
	RecoverInterruptedRuns(ctx context.Context) (int64, error)
}

type MultimodalRunRepository interface {
	CreateInitialMultimodalRun(
		ctx context.Context,
		ownerID string,
		threadID string,
		clientMessageID string,
		content string,
		imageAssetIDs []string,
		configuration RunConfiguration,
	) (RunSubmission, error)
}

type RunApplication interface {
	SubmitText(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		clientMessageID string,
		content string,
	) (RunSubmission, error)
	RetryText(
		ctx context.Context,
		actor requestcontext.Actor,
		runID string,
		retryClientID string,
	) (RunRetry, error)
	GetRun(
		ctx context.Context,
		actor requestcontext.Actor,
		runID string,
	) (Run, error)
	GetContextManifest(
		ctx context.Context,
		actor requestcontext.Actor,
		runID string,
	) (ContextManifest, error)
	GetToolCalls(
		ctx context.Context,
		actor requestcontext.Actor,
		runID string,
	) ([]ToolCallRecord, error)
}

type MultimodalRunApplication interface {
	SubmitMultimodal(
		ctx context.Context,
		actor requestcontext.Actor,
		threadID string,
		clientMessageID string,
		content string,
		imageAssetIDs []string,
	) (RunSubmission, error)
}

type IDGenerator interface {
	NewID() (string, error)
}
