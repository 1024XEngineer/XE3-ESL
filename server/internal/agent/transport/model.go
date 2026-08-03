package transport

import (
	agentapp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/image"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/voice"
)

type Thread = core.Thread
type ThreadMatterLink = core.ThreadMatterLink
type MessageRole = core.MessageRole
type Message = core.Message
type MessageAudio = core.MessageAudio
type MessagePageCursor = core.MessagePageCursor
type MessageAudioStatus = core.MessageAudioStatus
type MessageModality = core.MessageModality
type Run = core.Run
type ContextManifest = core.ContextManifest
type RunSubmission = core.RunSubmission
type RunRetry = core.RunRetry
type RunStreamObserver = core.RunStreamObserver
type Application = core.Application
type ThreadDeletionApplication = core.ThreadDeletionApplication
type RunApplication = core.RunApplication
type MultimodalRunApplication = core.MultimodalRunApplication
type Service = agentapp.Service
type ImageApplication = agentimage.Application
type ImageAsset = agentimage.Asset
type ImageContent = agentimage.Content
type UploadImageRequest = agentimage.UploadRequest

const (
	MessageRoleUser           = core.MessageRoleUser
	MessageRoleAssistant      = core.MessageRoleAssistant
	MessageModalityText       = core.MessageModalityText
	MessageModalityVoice      = core.MessageModalityVoice
	MessageModalityMultimodal = core.MessageModalityMultimodal
	MessageAudioReadable      = core.MessageAudioReadable
	MessageAudioDeleting      = core.MessageAudioDeleting
	MessageAudioDeleted       = core.MessageAudioDeleted
	RunStatusPending          = core.RunStatusPending
	RunStatusRunning          = core.RunStatusRunning
	RunStatusCompleted        = core.RunStatusCompleted
	RunStatusFailed           = core.RunStatusFailed
)

type VoiceSessionApplication = agentvoice.VoiceSessionApplication
type SameQuestionRetryApplication = agentvoice.SameQuestionRetryApplication
type VoiceConversationPort = agentvoice.VoiceConversationPort
type VoicePracticePort = agentvoice.VoicePracticePort
type VoiceReviewPort = agentvoice.VoiceReviewPort
type VoiceRoundOrchestrator = agentvoice.VoiceRoundOrchestrator
type VoiceTurnProgress = agentvoice.VoiceTurnProgress
type VoiceReviewCheckpoint = agentvoice.VoiceReviewCheckpoint
type VoiceReviewSource = agentvoice.VoiceReviewSource
type VoiceCompletionEvaluationPort = agentvoice.VoiceCompletionEvaluationPort
type VoiceCompletionEvaluationSource = agentvoice.VoiceCompletionEvaluationSource
type VoiceReviewReader = agentvoice.VoiceReviewReader
type VoiceHTTPMessageApplication = agentvoice.VoiceMessageApplication
type VoiceMessageApplication = agentvoice.VoiceMessageApplication
type VoiceMessageConfig = agentvoice.VoiceMessageConfig
type VoiceCandidate = agentvoice.VoiceCandidate
type VoiceConfirmation = agentvoice.VoiceConfirmation
type VoiceCandidateStatus = agentvoice.VoiceCandidateStatus
type ConfirmVoiceCandidateCommand = agentvoice.ConfirmVoiceCandidateCommand
type UploadVoiceCandidateRequest = agentvoice.UploadVoiceCandidateRequest
type VoicePracticeSession = agentvoice.VoicePracticeSession
type VoiceScenarioPrompt = agentvoice.VoiceScenarioPrompt
type VoiceSessionState = agentvoice.VoiceSessionState
type VoiceSessionReview = agentvoice.VoiceSessionReview
type VoiceReviewResult = agentvoice.VoiceReviewResult
type VoiceReviewConclusion = agentvoice.VoiceReviewConclusion
type VoiceReviewHistoryCursor = agentvoice.VoiceReviewHistoryCursor
type VoiceReviewHistoryQuery = agentvoice.VoiceReviewHistoryQuery
type VoiceReviewHistoryPage = agentvoice.VoiceReviewHistoryPage

var NewVoiceRoundOrchestrator = agentvoice.NewVoiceRoundOrchestrator
var NewVoiceSessionApplication = agentvoice.NewVoiceSessionApplication

const (
	VoiceCandidateReady     = agentvoice.VoiceCandidateReady
	VoiceCandidateFailed    = agentvoice.VoiceCandidateFailed
	VoiceCandidateConfirmed = agentvoice.VoiceCandidateConfirmed
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
)
