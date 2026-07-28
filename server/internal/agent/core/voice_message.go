package core

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const AgentVoiceObjectPrefix = "audio/v1/agent/"

var (
	ErrVoiceCandidateProcessing = errors.New("agent: voice candidate is processing")
	ErrVoiceCandidateStale      = errors.New("agent: voice candidate version is stale")
	ErrVoiceCleanupPending      = errors.New("agent: voice object cleanup is pending")
)

type VoiceCandidateStatus string

const (
	VoiceCandidateStaged       VoiceCandidateStatus = "staged"
	VoiceCandidateTranscribing VoiceCandidateStatus = "transcribing"
	VoiceCandidateReady        VoiceCandidateStatus = "candidate_ready"
	VoiceCandidateFailed       VoiceCandidateStatus = "failed"
	VoiceCandidateConfirming   VoiceCandidateStatus = "confirming"
	VoiceCandidateConfirmed    VoiceCandidateStatus = "confirmed"
	VoiceCandidateDeleting     VoiceCandidateStatus = "deleting"
	VoiceCandidateDeleted      VoiceCandidateStatus = "deleted"
)

// VoiceCandidate is Agent-owned durable workflow state. ObjectKey is an
// internal capability and must never be copied into an HTTP response.
type VoiceCandidate struct {
	ID                 string
	OwnerID            string
	ThreadID           string
	UploadRequestID    string
	ObjectKey          string
	ContentType        string
	Size               int64
	ChecksumSHA256     string
	Duration           time.Duration
	SampleRate         int
	ETag               string
	UploadLeaseUntil   time.Time
	UploadFencingToken uint64
	Status             VoiceCandidateStatus
	ASRAttempt         int
	CandidateVersion   int64
	ASRLeaseUntil      time.Time
	ASRFencingToken    uint64
	ASRRequestID       string
	ASRProvider        string
	ASRModel           string
	ASRCandidateText   string
	ASRLanguage        string
	ASREmotion         string
	ASRFinishReason    string
	FailureKind        string
	FailureRetryable   bool
	ExpiresAt          time.Time
	ConfirmedMessageID string
	ConfirmedRunID     string
	MessageAudioID     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ConfirmedAt        time.Time
	DeletedAt          time.Time
}

// TranscriptEvidence preserves the provider candidate independently from the
// text the user confirmed into AgentMessage.content.
type TranscriptEvidence struct {
	ID               string
	OwnerID          string
	ThreadID         string
	CandidateID      string
	CandidateVersion int64
	MessageID        string
	ASRRequestID     string
	ASRProvider      string
	ASRModel         string
	ASRCandidateText string
	ConfirmedText    string
	Language         string
	Emotion          string
	FinishReason     string
	CreatedAt        time.Time
}

type VoiceCandidateStage struct {
	Candidate VoiceCandidate
	Created   bool
}

type VoiceUploadClaim struct {
	Candidate      VoiceCandidate
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type VoiceTranscriptionClaim struct {
	Candidate      VoiceCandidate
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type VoiceConfirmation struct {
	Candidate VoiceCandidate
	Evidence  TranscriptEvidence
	Message   Message
	Audio     MessageAudio
	Run       Run
	Created   bool
}

type VoiceCleanupKind string

const (
	VoiceCleanupCandidate VoiceCleanupKind = "candidate"
	VoiceCleanupAudio     VoiceCleanupKind = "message_audio"
)

type VoiceCleanupClaim struct {
	Kind           VoiceCleanupKind
	OwnerID        string
	CandidateID    string
	AudioID        string
	ObjectKey      string
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type VoiceCleanupResult struct {
	Deleted int
	Failed  int
}

type StageVoiceCandidateCommand struct {
	Candidate VoiceCandidate
}

type ConfirmVoiceCandidateCommand struct {
	CandidateID      string
	CandidateVersion int64
	ClientMessageID  string
	ConfirmedText    string
	Configuration    RunConfiguration
}

// VoiceMessageRepository owns only Agent tables. It deliberately does not
// depend on or adapt the Conversation Repository.
type VoiceMessageRepository interface {
	StageVoiceCandidate(
		context.Context,
		StageVoiceCandidateCommand,
	) (VoiceCandidateStage, error)
	ClaimVoiceUpload(
		context.Context,
		string,
		string,
		time.Duration,
	) (VoiceUploadClaim, bool, error)
	CommitVoiceUpload(
		context.Context,
		string,
		string,
		uint64,
		string,
	) (VoiceCandidate, error)
	FindVoiceCandidate(
		context.Context,
		string,
		string,
	) (VoiceCandidate, error)
	ClaimVoiceTranscription(
		context.Context,
		string,
		string,
		time.Duration,
	) (VoiceTranscriptionClaim, bool, error)
	CompleteVoiceTranscription(
		context.Context,
		string,
		string,
		uint64,
		ai.TranscriptionResult,
	) (VoiceCandidate, error)
	FailVoiceTranscription(
		context.Context,
		string,
		string,
		uint64,
		string,
		bool,
	) (VoiceCandidate, error)
	ConfirmVoiceCandidate(
		context.Context,
		string,
		ConfirmVoiceCandidateCommand,
	) (VoiceConfirmation, error)
	FindMessageAudio(
		context.Context,
		string,
		string,
	) (MessageAudio, error)
	FindMessageByID(
		context.Context,
		string,
		string,
	) (Message, error)
	BeginVoiceCandidateDeletion(
		context.Context,
		string,
		string,
	) (VoiceCandidate, error)
	FinishVoiceCandidateDeletion(
		context.Context,
		string,
		string,
	) (VoiceCandidate, error)
	BeginMessageAudioDeletion(
		context.Context,
		string,
		string,
	) (MessageAudio, error)
	FinishMessageAudioDeletion(
		context.Context,
		string,
		string,
	) (MessageAudio, error)
	ClaimVoiceCleanup(
		context.Context,
		time.Duration,
		int,
	) ([]VoiceCleanupClaim, error)
	FinishVoiceCleanup(
		context.Context,
		VoiceCleanupClaim,
	) error
	ReleaseVoiceCleanup(
		context.Context,
		VoiceCleanupClaim,
	) error
}
