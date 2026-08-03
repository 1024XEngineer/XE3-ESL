// Package voice owns voice supplied as Agent input, from private upload and
// transcription through confirmation, playback, and object cleanup.
package voice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const ObjectPrefix = "audio/v1/agent/"

var (
	ErrInvalidRequest      = errors.New("agent voice input: invalid request")
	ErrNotFound            = errors.New("agent voice input: not found")
	ErrConflict            = errors.New("agent voice input: conflict")
	ErrIdempotencyConflict = errors.New("agent voice input: idempotency conflict")
	ErrInvalidContext      = errors.New("agent voice input: invalid context")
	ErrRepository          = errors.New("agent voice input repository: operation failed")
	ErrCandidateProcessing = errors.New("agent voice input: candidate is processing")
	ErrCandidateStale      = errors.New("agent voice input: candidate version is stale")
	ErrCleanupPending      = errors.New("agent voice input: object cleanup is pending")
)

type CandidateStatus string

const (
	StatusStaged       CandidateStatus = "staged"
	StatusTranscribing CandidateStatus = "transcribing"
	StatusReady        CandidateStatus = "candidate_ready"
	StatusFailed       CandidateStatus = "failed"
	StatusConfirming   CandidateStatus = "confirming"
	StatusConfirmed    CandidateStatus = "confirmed"
	StatusDeleting     CandidateStatus = "deleting"
	StatusDeleted      CandidateStatus = "deleted"
)

// Candidate is the durable workflow state for one voice input. ObjectKey is
// server-only and must never be copied into an HTTP response.
type Candidate struct {
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
	Status             CandidateStatus
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

func (candidate Candidate) ValidNew() bool {
	return ValidUUID(candidate.ID) &&
		ValidUUID(candidate.OwnerID) &&
		ValidUUID(candidate.ThreadID) &&
		validIdempotencyKey(candidate.UploadRequestID) &&
		strings.HasPrefix(candidate.ObjectKey, ObjectPrefix) &&
		strings.HasSuffix(candidate.ObjectKey, ".wav") &&
		!strings.Contains(candidate.ObjectKey, "..") &&
		candidate.ContentType == "audio/wav" &&
		candidate.Size > 0 &&
		candidate.Size <= 7_400_000 &&
		len(candidate.ChecksumSHA256) == 64 &&
		candidate.Duration > 0 &&
		candidate.Duration <= 60*time.Second &&
		candidate.SampleRate >= 8_000 &&
		candidate.SampleRate <= 48_000 &&
		candidate.Status == StatusStaged &&
		candidate.ExpiresAt.After(candidate.CreatedAt) &&
		candidate.ExpiresAt.Sub(candidate.CreatedAt) <= 30*24*time.Hour
}

// TranscriptEvidence preserves the provider candidate independently from the
// text the user confirmed into the Conversation message.
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

type CandidateStage struct {
	Candidate Candidate
	Created   bool
}

type UploadClaim struct {
	Candidate      Candidate
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type TranscriptionClaim struct {
	Candidate      Candidate
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type Confirmation struct {
	Candidate Candidate
	Evidence  TranscriptEvidence
	Message   conversation.Message
	Audio     conversation.MessageAudio
	Run       run.Run
	Created   bool
}

type CleanupKind string

const (
	CleanupCandidate CleanupKind = "candidate"
	CleanupAudio     CleanupKind = "message_audio"
)

type CleanupClaim struct {
	Kind           CleanupKind
	OwnerID        string
	CandidateID    string
	AudioID        string
	ObjectKey      string
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

func (claim CleanupClaim) Valid() bool {
	if !ValidUUID(claim.OwnerID) ||
		!strings.HasPrefix(claim.ObjectKey, ObjectPrefix) ||
		strings.Contains(claim.ObjectKey, "..") ||
		claim.FencingToken == 0 ||
		claim.FencingToken > uint64(1<<63-1) {
		return false
	}
	switch claim.Kind {
	case CleanupCandidate:
		return ValidUUID(claim.CandidateID) &&
			(claim.AudioID == "" || ValidUUID(claim.AudioID))
	case CleanupAudio:
		return ValidUUID(claim.AudioID) && claim.CandidateID == ""
	default:
		return false
	}
}

type CleanupResult struct {
	Deleted int
	Failed  int
}

type StageCandidateCommand struct {
	Candidate Candidate
}

type ConfirmCandidateCommand struct {
	CandidateID      string
	CandidateVersion int64
	ClientMessageID  string
	ConfirmedText    string
	Configuration    run.Configuration
}

type Repository interface {
	StageCandidate(
		context.Context,
		StageCandidateCommand,
	) (CandidateStage, error)
	ClaimUpload(
		context.Context,
		string,
		string,
		time.Duration,
	) (UploadClaim, bool, error)
	CommitUpload(
		context.Context,
		string,
		string,
		uint64,
		string,
	) (Candidate, error)
	FindCandidate(context.Context, string, string) (Candidate, error)
	ClaimTranscription(
		context.Context,
		string,
		string,
		time.Duration,
	) (TranscriptionClaim, bool, error)
	CompleteTranscription(
		context.Context,
		string,
		string,
		uint64,
		ai.TranscriptionResult,
	) (Candidate, error)
	FailTranscription(
		context.Context,
		string,
		string,
		uint64,
		string,
		bool,
	) (Candidate, error)
	ConfirmCandidate(
		context.Context,
		string,
		ConfirmCandidateCommand,
	) (Confirmation, error)
	FindMessageAudio(
		context.Context,
		string,
		string,
	) (conversation.MessageAudio, error)
	FindMessageByID(
		context.Context,
		string,
		string,
	) (conversation.Message, error)
	BeginCandidateDeletion(
		context.Context,
		string,
		string,
	) (Candidate, error)
	FinishCandidateDeletion(
		context.Context,
		string,
		string,
	) (Candidate, error)
	BeginMessageAudioDeletion(
		context.Context,
		string,
		string,
	) (conversation.MessageAudio, error)
	FinishMessageAudioDeletion(
		context.Context,
		string,
		string,
	) (conversation.MessageAudio, error)
	ClaimCleanup(
		context.Context,
		time.Duration,
		int,
	) ([]CleanupClaim, error)
	FinishCleanup(context.Context, CleanupClaim) error
	ReleaseCleanup(context.Context, CleanupClaim) error
}

type IDGenerator interface {
	NewID() (string, error)
}
