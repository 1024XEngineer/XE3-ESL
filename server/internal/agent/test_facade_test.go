package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	agentapp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentpersistence "github.com/1024XEngineer/XE3-ESL/server/internal/agent/persistence"
	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/summary"
	agenttransport "github.com/1024XEngineer/XE3-ESL/server/internal/agent/transport"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/gin-gonic/gin"
)

type MessageRole = core.MessageRole
type MessageModality = core.MessageModality
type MessageAudioStatus = core.MessageAudioStatus
type Thread = core.Thread
type ThreadMatterLink = core.ThreadMatterLink
type Message = core.Message
type MessageAudio = core.MessageAudio
type RunStatus = core.RunStatus
type Run = core.Run
type ToolCallStatus = core.ToolCallStatus
type ToolCallRecord = core.ToolCallRecord
type ContextMessageSource = core.ContextMessageSource
type ContextMemorySource = core.ContextMemorySource
type ContextStableProfileSource = core.ContextStableProfileSource
type ContextSummarySource = core.ContextSummarySource
type ContextManifest = core.ContextManifest
type ThreadSummaryContent = core.ThreadSummaryContent
type ThreadSummaryCheckpoint = core.ThreadSummaryCheckpoint
type CreateThreadSummaryCheckpointCommand = core.CreateThreadSummaryCheckpointCommand
type ThreadSummaryCheckpointRepository = core.ThreadSummaryCheckpointRepository
type SummaryConfiguration = agentsummary.Configuration
type GenerateSummaryCheckpointCommand = agentsummary.GenerateCheckpointCommand
type SummaryService = agentsummary.Service
type RunConfiguration = core.RunConfiguration
type RunSubmission = core.RunSubmission
type RunRetry = core.RunRetry
type ThreadPageCursor = core.ThreadPageCursor
type MessagePageCursor = core.MessagePageCursor
type ThreadPage = core.ThreadPage
type MessagePage = core.MessagePage
type Repository = core.Repository
type Application = core.Application
type RunRepository = core.RunRepository
type RunApplication = core.RunApplication
type IDGenerator = core.IDGenerator
type Service = agentapp.Service
type ContextRepository = agentruntime.ContextRepository
type ContextAssembler = agentruntime.ContextAssembler
type MemorySearchRequest = agentruntime.MemorySearchRequest
type MemorySearchHit = agentruntime.MemorySearchHit
type MemorySearcher = agentruntime.MemorySearcher
type StableProfileReadRequest = agentruntime.StableProfileReadRequest
type StableProfileMemory = agentruntime.StableProfileMemory
type StableProfileReader = agentruntime.StableProfileReader
type RunService = agentruntime.RunService
type PostgreSQL = agentpersistence.PostgreSQL
type PostgresRepository = agentpersistence.PostgresRepository

type VoiceCandidateStatus = core.VoiceCandidateStatus
type VoiceCandidate = core.VoiceCandidate
type TranscriptEvidence = core.TranscriptEvidence
type VoiceCandidateStage = core.VoiceCandidateStage
type VoiceUploadClaim = core.VoiceUploadClaim
type VoiceTranscriptionClaim = core.VoiceTranscriptionClaim
type VoiceConfirmation = core.VoiceConfirmation
type VoiceCleanupKind = core.VoiceCleanupKind
type VoiceCleanupClaim = core.VoiceCleanupClaim
type VoiceCleanupResult = core.VoiceCleanupResult
type StageVoiceCandidateCommand = core.StageVoiceCandidateCommand
type ConfirmVoiceCandidateCommand = core.ConfirmVoiceCandidateCommand
type VoiceMessageRepository = core.VoiceMessageRepository

type VoiceSessionApplication = agentvoice.VoiceSessionApplication
type VoiceMessageConfig = agentvoice.VoiceMessageConfig
type UploadVoiceCandidateRequest = agentvoice.UploadVoiceCandidateRequest
type VoiceMessageApplication = agentvoice.VoiceMessageApplication
type VoiceMessageService = agentvoice.VoiceMessageService
type VoiceAudioSourceLoader = agentvoice.VoiceAudioSourceLoader
type VoicePendingRunProcessor = agentvoice.VoicePendingRunProcessor

const (
	MessageRoleUser              = core.MessageRoleUser
	MessageRoleAssistant         = core.MessageRoleAssistant
	MessageModalityText          = core.MessageModalityText
	MessageModalityVoice         = core.MessageModalityVoice
	MessageAudioReadable         = core.MessageAudioReadable
	MessageAudioDeleting         = core.MessageAudioDeleting
	MessageAudioDeleted          = core.MessageAudioDeleted
	RunStatusPending             = core.RunStatusPending
	RunStatusRunning             = core.RunStatusRunning
	RunStatusCompleted           = core.RunStatusCompleted
	RunStatusFailed              = core.RunStatusFailed
	ToolCallStatusProposed       = core.ToolCallStatusProposed
	ToolCallStatusRunning        = core.ToolCallStatusRunning
	ToolCallStatusSucceeded      = core.ToolCallStatusSucceeded
	ToolCallStatusFailed         = core.ToolCallStatusFailed
	ToolCallStatusRejected       = core.ToolCallStatusRejected
	RunFailureInterrupted        = core.RunFailureInterrupted
	RunFailureConfigurationDrift = core.RunFailureConfigurationDrift
	RunFailureInvalidContext     = core.RunFailureInvalidContext
	RunFailureInternal           = core.RunFailureInternal
	VoiceCandidateStaged         = core.VoiceCandidateStaged
	VoiceCandidateReady          = core.VoiceCandidateReady
	VoiceCandidateFailed         = core.VoiceCandidateFailed
	VoiceCandidateDeleting       = core.VoiceCandidateDeleting
	VoiceCandidateDeleted        = core.VoiceCandidateDeleted
	VoiceCleanupCandidate        = core.VoiceCleanupCandidate
	VoiceCleanupAudio            = core.VoiceCleanupAudio
	contextTrimNone              = "none"
	contextTrimBudget            = "context_budget"
	contextTrimSummary           = "summary_checkpoint"
	contextTrimSummaryAndBudget  = "summary_checkpoint_and_budget"
	instructionV1                = "speakup_text_v1"
	memoryContextLimit           = 6
	memoryContextPolicyV1        = "memory-context-v1"
	summaryContextPolicyV1       = "summary-context-v1"
	summaryContextNotAvailable   = "not_available"
	summaryContextSelected       = "selected"
	summaryContextOmittedBudget  = "omitted_budget"
	maxMessageContentRunes       = core.MaxMessageContentRunes
	maxMessageContentBytes       = core.MaxMessageContentBytes
	maxAgentPageSize             = core.MaxAgentPageSize
	maxPersistedTokenCount       = 1<<31 - 1
)

var (
	ErrInvalidRequest           = core.ErrInvalidRequest
	ErrNotFound                 = core.ErrNotFound
	ErrConflict                 = core.ErrConflict
	ErrIdempotencyConflict      = core.ErrIdempotencyConflict
	ErrInvalidContext           = core.ErrInvalidContext
	ErrRepository               = core.ErrRepository
	ErrVoiceCandidateProcessing = core.ErrVoiceCandidateProcessing
	ErrVoiceCandidateStale      = core.ErrVoiceCandidateStale
	ErrVoiceCleanupPending      = core.ErrVoiceCleanupPending
	NewService                  = agentapp.NewService
	NewContextAssembler         = agentruntime.NewContextAssembler
	NewRunService               = agentruntime.NewRunService
	WithToolRegistry            = agentruntime.WithToolRegistry
	NewPostgresRepository       = agentpersistence.NewPostgresRepository
	NewSummaryService           = agentsummary.NewService
	NewVoiceMessageService      = agentvoice.NewVoiceMessageService
	NewHTTPHandler              = agenttransport.NewHTTPHandler
	NewHTTPHandlerWithRuns      = agenttransport.NewHTTPHandlerWithRuns
)

func encodeThreadPageCursor(cursor ThreadPageCursor) (string, error) {
	return core.EncodeThreadPageCursor(cursor)
}

func decodeThreadPageCursor(raw string) (ThreadPageCursor, error) {
	return core.DecodeThreadPageCursor(raw)
}

func encodeMessagePageCursor(cursor MessagePageCursor) (string, error) {
	return core.EncodeMessagePageCursor(cursor)
}

func decodeMessagePageCursor(
	raw string,
	threadID string,
) (MessagePageCursor, error) {
	return core.DecodeMessagePageCursor(raw, threadID)
}

type testModule struct {
	handler interface{ RegisterRoutes(*gin.Engine) }
}

func NewModule(handler interface{ RegisterRoutes(*gin.Engine) }) (*testModule, error) {
	return &testModule{handler: handler}, nil
}

func (m *testModule) RegisterRoutes(router *gin.Engine) {
	m.handler.RegisterRoutes(router)
}

func successfulVoiceTranscription() ai.TranscriptionResult {
	return ai.TranscriptionResult{
		ID:         "fake-asr-request-1",
		Provider:   "fake",
		Model:      "fake-asr-model",
		Transcript: "A faithful provider transcript.",
		Language:   "en",
	}
}

func voiceTestWAV(sample byte) []byte {
	const (
		sampleRate = 16_000
		samples    = 1_600
		dataSize   = samples * 2
	)
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(result[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], dataSize)
	for index := 44; index < len(result); index++ {
		result[index] = sample
	}
	return result
}

type storedVoiceSourceLoader struct {
	store     *objectfake.Store
	directory string
}

func (loader *storedVoiceSourceLoader) LoadVoiceAudio(
	_ context.Context,
	candidate VoiceCandidate,
) (platformmedia.ManagedAudioSource, error) {
	body, found := loader.store.Bytes(candidate.ObjectKey)
	if !found {
		return nil, objectstore.ErrOperationFailed
	}
	return platformmedia.CaptureTemporaryAudio(
		loader.directory,
		candidate.ContentType,
		bytes.NewReader(body),
	)
}

type blockingVoiceStore struct {
	*objectfake.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
	puts    atomic.Int32
}

func newBlockingVoiceStore(store *objectfake.Store) *blockingVoiceStore {
	return &blockingVoiceStore{
		Store:   store,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (store *blockingVoiceStore) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	store.puts.Add(1)
	store.once.Do(func() { close(store.started) })
	select {
	case <-ctx.Done():
		return objectstore.PutResult{}, ctx.Err()
	case <-store.release:
		return store.Store.Put(ctx, request)
	}
}

func messageResponse(message Message) gin.H {
	result := gin.H{
		"message_id": message.ID,
		"thread_id":  message.ThreadID,
		"sequence":   message.Sequence,
		"role":       message.Role,
		"content":    message.Content,
		"created_at": message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if message.ClientMessageID != "" {
		result["client_message_id"] = message.ClientMessageID
	}
	if message.ProducedByRunID != "" {
		result["produced_by_run_id"] = message.ProducedByRunID
	}
	if message.Audio != nil {
		result["modality"] = MessageModalityVoice
		result["audio"] = agentMessageAudioResponse(*message.Audio)
	}
	return result
}

func agentMessageAudioResponse(audio MessageAudio) gin.H {
	result := gin.H{
		"audio_id":     audio.ID,
		"status":       audio.Status,
		"content_type": audio.ContentType,
		"size_bytes":   audio.Size,
		"duration_ms":  durationMilliseconds(audio.Duration),
	}
	if audio.Status == MessageAudioReadable {
		result["playback_path"] =
			"/v1/agent-message-audios/" + audio.ID + "/playback"
	}
	if !audio.DeletedAt.IsZero() {
		result["deleted_at"] = audio.DeletedAt.UTC().
			Format(time.RFC3339Nano)
	}
	return result
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
}
