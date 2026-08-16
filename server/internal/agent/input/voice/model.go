// Package voice owns the Agent voice-draft workflow: ASR, user confirmation,
// playback authorization, and creation of the confirmed Message and Run.
package voice

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrInvalidRequest      = errors.New("agent voice input: invalid request")
	ErrNotFound            = errors.New("agent voice input: not found")
	ErrConflict            = errors.New("agent voice input: conflict")
	ErrIdempotencyConflict = errors.New("agent voice input: idempotency conflict")
	ErrInvalidContext      = errors.New("agent voice input: invalid context")
	ErrRepository          = errors.New("agent voice input repository: operation failed")
	ErrDraftProcessing     = errors.New("agent voice input: draft is processing")
	ErrDraftStale          = errors.New("agent voice input: draft version is stale")
	ErrCleanupPending      = errors.New("agent voice input: object cleanup is pending")
)

type DraftStatus string

const (
	StatusTranscribing DraftStatus = "transcribing"
	StatusReady        DraftStatus = "ready"
	StatusFailed       DraftStatus = "failed"
	StatusConfirmed    DraftStatus = "confirmed"
)

// Draft is one durable voice input. Media fields are a read projection of the
// referenced shared Asset; they are not duplicated in agent_voice_drafts.
type Draft struct {
	ID                 string
	OwnerID            string
	ThreadID           string
	ObjectKey          string
	ContentType        string
	Size               int64
	ChecksumSHA256     string
	Duration           time.Duration
	SampleRate         int
	ExpiresAt          time.Time
	Status             DraftStatus
	ASRAttempt         int
	Version            int64
	ASRLeaseUntil      time.Time
	ASRFencingToken    uint64
	ASRRequestID       string
	ASRProvider        string
	ASRModel           string
	Transcript         string
	ASRLanguage        string
	ASREmotion         string
	ASRFinishReason    string
	FailureKind        string
	FailureRetryable   bool
	ConfirmedMessageID string
	ConfirmedRunID     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ConfirmedAt        time.Time
}

type TranscriptionClaim struct {
	Draft          Draft
	FencingToken   uint64
	LeaseExpiresAt time.Time
}

type Confirmation struct {
	Draft      Draft
	Message    conversation.Message
	Attachment conversation.AudioAttachment
	Run        run.Run
	Created    bool
}

type ConfirmDraftCommand struct {
	DraftID         string
	Version         int64
	ClientMessageID string
	ConfirmedText   string
	Configuration   run.Configuration
}

type Repository interface {
	StageDraft(
		context.Context,
		string,
		string,
		string,
		time.Duration,
	) (TranscriptionClaim, bool, error)
	FindDraft(context.Context, string, string) (Draft, error)
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
		TranscriptionResult,
	) (Draft, error)
	FailTranscription(
		context.Context,
		string,
		string,
		uint64,
		string,
		bool,
	) (Draft, error)
	ConfirmDraft(
		context.Context,
		string,
		ConfirmDraftCommand,
	) (Confirmation, error)
	FindAudioAttachment(
		context.Context,
		string,
		string,
	) (conversation.AudioAttachment, error)
	FindMessageByID(
		context.Context,
		string,
		string,
	) (conversation.Message, error)
	DiscardDraft(context.Context, string, string) error
	DetachAudio(context.Context, string, string) error
}

type AudioSourceLoader interface {
	LoadVoiceAudio(context.Context, Draft) (platformmedia.ManagedAudioSource, error)
}

type PendingRunProcessor interface {
	ProcessPending(context.Context, requestcontext.Actor, run.Run) (run.Run, error)
	ProcessPendingStream(
		context.Context,
		requestcontext.Actor,
		run.Run,
		run.StreamObserver,
	) (run.Run, error)
}

type ConfirmationStreamObserver interface {
	OnConfirmationCommitted(context.Context, Confirmation) error
	OnAssistantStarted(context.Context, run.Run) error
	OnAssistantDelta(context.Context, string) error
}

type FeedbackReference struct {
	StatusURL string
}

type FeedbackPort interface {
	EnsureMessage(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (FeedbackReference, error)
}

type Config struct {
	Configuration    run.Configuration
	ScratchDirectory string
	DraftTTL         time.Duration
	ASRLease         time.Duration
}

type UploadRequest struct {
	ThreadID       string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type Application interface {
	UploadStream(
		context.Context,
		requestcontext.Actor,
		UploadRequest,
		TranscriptionObserver,
	) (Draft, error)
	GetDraft(context.Context, requestcontext.Actor, string) (Draft, error)
	Retry(context.Context, requestcontext.Actor, string) (Draft, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		ConfirmDraftCommand,
	) (Confirmation, error)
	ConfirmStream(
		context.Context,
		requestcontext.Actor,
		ConfirmDraftCommand,
		ConfirmationStreamObserver,
	) (Confirmation, error)
	Playback(
		context.Context,
		requestcontext.Actor,
		string,
	) (objectstore.SignedGetResult, error)
	DeleteDraft(context.Context, requestcontext.Actor, string) error
	DeleteAudio(context.Context, requestcontext.Actor, string) error
	SynthesizeMessage(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (SynthesisResult, error)
}
