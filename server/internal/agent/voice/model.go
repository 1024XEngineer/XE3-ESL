package voice

import "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"

type MessageRole = core.MessageRole
type MessageModality = core.MessageModality
type MessageAudioStatus = core.MessageAudioStatus
type Message = core.Message
type MessageAudio = core.MessageAudio
type Run = core.Run
type RunConfiguration = core.RunConfiguration
type IDGenerator = core.IDGenerator

const (
	MessageRoleUser        = core.MessageRoleUser
	MessageRoleAssistant   = core.MessageRoleAssistant
	MessageModalityText    = core.MessageModalityText
	MessageModalityVoice   = core.MessageModalityVoice
	MessageAudioReadable   = core.MessageAudioReadable
	MessageAudioDeleting   = core.MessageAudioDeleting
	MessageAudioDeleted    = core.MessageAudioDeleted
	RunStatusPending       = core.RunStatusPending
	RunStatusCompleted     = core.RunStatusCompleted
	AgentVoiceObjectPrefix = core.AgentVoiceObjectPrefix
)

type VoiceCandidateStatus = core.VoiceCandidateStatus

const (
	VoiceCandidateStaged       = core.VoiceCandidateStaged
	VoiceCandidateTranscribing = core.VoiceCandidateTranscribing
	VoiceCandidateReady        = core.VoiceCandidateReady
	VoiceCandidateFailed       = core.VoiceCandidateFailed
	VoiceCandidateConfirming   = core.VoiceCandidateConfirming
	VoiceCandidateConfirmed    = core.VoiceCandidateConfirmed
	VoiceCandidateDeleting     = core.VoiceCandidateDeleting
	VoiceCandidateDeleted      = core.VoiceCandidateDeleted
)

type VoiceCandidate = core.VoiceCandidate
type TranscriptEvidence = core.TranscriptEvidence
type VoiceCandidateStage = core.VoiceCandidateStage
type VoiceUploadClaim = core.VoiceUploadClaim
type VoiceTranscriptionClaim = core.VoiceTranscriptionClaim
type VoiceConfirmation = core.VoiceConfirmation
type VoiceCleanupKind = core.VoiceCleanupKind

const (
	VoiceCleanupCandidate = core.VoiceCleanupCandidate
	VoiceCleanupAudio     = core.VoiceCleanupAudio
)

type VoiceCleanupClaim = core.VoiceCleanupClaim
type VoiceCleanupResult = core.VoiceCleanupResult
type StageVoiceCandidateCommand = core.StageVoiceCandidateCommand
type ConfirmVoiceCandidateCommand = core.ConfirmVoiceCandidateCommand
type VoiceMessageRepository = core.VoiceMessageRepository

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
