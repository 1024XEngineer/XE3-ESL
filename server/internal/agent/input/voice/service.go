package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Service struct {
	repository  Repository
	media       *sharedmedia.Service
	sources     AudioSourceLoader
	recognizer  StreamingSpeechRecognizer
	synthesizer SpeechSynthesizer
	runs        PendingRunProcessor
	feedback    FeedbackPort
	clock       func() time.Time
	config      Config
}

func NewService(
	repository Repository,
	mediaService *sharedmedia.Service,
	sources AudioSourceLoader,
	recognizer StreamingSpeechRecognizer,
	synthesizer SpeechSynthesizer,
	runs PendingRunProcessor,
	config Config,
	feedback FeedbackPort,
) (*Service, error) {
	if repository == nil || mediaService == nil || sources == nil ||
		recognizer == nil || synthesizer == nil || runs == nil || feedback == nil ||
		!validConfiguration(config.Configuration) ||
		config.DraftTTL < time.Minute || config.DraftTTL > 30*24*time.Hour ||
		config.ASRLease < time.Second || config.ASRLease > 10*time.Minute {
		return nil, errors.New("agent voice input: dependencies are invalid")
	}
	return &Service{
		repository: repository, media: mediaService, sources: sources,
		recognizer: recognizer, synthesizer: synthesizer, runs: runs,
		feedback: feedback, clock: func() time.Time { return time.Now().UTC() },
		config: config,
	}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
) (Draft, error) {
	return service.upload(ctx, actor, request, nil, nil)
}

func (service *Service) UploadStream(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
	observer TranscriptionObserver,
) (Draft, error) {
	if observer == nil {
		return Draft{}, ErrInvalidRequest
	}
	return service.upload(ctx, actor, request, observer, nil)
}

// UploadRecognized persists audio that was transcribed while it arrived. The
// provider result becomes the durable Draft result without a second ASR pass.
func (service *Service) UploadRecognized(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
	result TranscriptionResult,
) (Draft, error) {
	if !ValidTranscription(result) {
		return Draft{}, ErrInvalidRequest
	}
	return service.upload(ctx, actor, request, nil, &result)
}

func (service *Service) upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
	observer TranscriptionObserver,
	recognized *TranscriptionResult,
) (Draft, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(request.ThreadID) ||
		!validIdempotencyKey(request.IdempotencyKey) || request.Audio == nil {
		return Draft{}, ErrInvalidRequest
	}
	audio, err := platformmedia.CaptureTemporaryAudio(
		service.config.ScratchDirectory,
		request.ContentType,
		request.Audio,
	)
	if err != nil {
		return Draft{}, ErrInvalidRequest
	}
	defer audio.Close()
	checksum, err := voiceAudioChecksum(audio)
	if err != nil {
		return Draft{}, ErrRepository
	}
	reader, err := audio.Open()
	if err != nil {
		return Draft{}, ErrRepository
	}
	seeker, ok := reader.(io.ReadSeeker)
	if !ok {
		_ = reader.Close()
		return Draft{}, ErrRepository
	}
	asset, uploadErr := service.media.Upload(ctx, sharedmedia.Upload{
		UserID: actor.UserID, Kind: sharedmedia.KindAudio,
		IdempotencyKey: request.IdempotencyKey,
		ContentType:    audio.MediaType(), Body: seeker, Size: audio.Size(),
		ChecksumSHA256: checksum, Duration: audio.Duration(),
		SampleRate: audio.SampleRate(),
		ExpiresAt:  service.clock().Add(service.config.DraftTTL),
	})
	closeErr := reader.Close()
	if uploadErr != nil || closeErr != nil {
		return Draft{}, mapMediaError(errors.Join(uploadErr, closeErr))
	}
	if asset.Status != sharedmedia.StatusReady {
		return Draft{}, ErrDraftProcessing
	}
	claim, acquired, err := service.repository.StageDraft(
		ctx,
		actor.UserID,
		request.ThreadID,
		asset.ID,
		service.config.ASRLease,
	)
	if err != nil {
		return Draft{}, err
	}
	if !acquired {
		return claim.Draft, nil
	}
	if recognized != nil {
		persistContext, cancel := runPersistenceContext(ctx)
		defer cancel()
		return service.repository.CompleteTranscription(
			persistContext,
			claim.Draft.OwnerID,
			claim.Draft.ID,
			claim.FencingToken,
			*recognized,
		)
	}
	return service.transcribeClaim(ctx, claim, audio, observer)
}

func (service *Service) GetDraft(
	ctx context.Context,
	actor requestcontext.Actor,
	draftID string,
) (Draft, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(draftID) {
		return Draft{}, ErrNotFound
	}
	return service.repository.FindDraft(ctx, actor.UserID, draftID)
}

func (service *Service) Retry(
	ctx context.Context,
	actor requestcontext.Actor,
	draftID string,
) (Draft, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(draftID) {
		return Draft{}, ErrNotFound
	}
	current, err := service.repository.FindDraft(ctx, actor.UserID, draftID)
	if err != nil {
		return Draft{}, err
	}
	if current.Status != StatusTranscribing &&
		(current.Status != StatusFailed || !current.FailureRetryable) {
		return Draft{}, ErrConflict
	}
	return service.transcribe(ctx, actor, draftID, nil)
}

func (service *Service) transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	draftID string,
	observer TranscriptionObserver,
) (Draft, error) {
	claim, acquired, err := service.repository.ClaimTranscription(
		ctx, actor.UserID, draftID, service.config.ASRLease,
	)
	if err != nil {
		return Draft{}, err
	}
	if !acquired {
		return claim.Draft, nil
	}
	return service.transcribeClaim(ctx, claim, nil, observer)
}

func (service *Service) transcribeClaim(
	ctx context.Context,
	claim TranscriptionClaim,
	source platformmedia.ManagedAudioSource,
	observer TranscriptionObserver,
) (Draft, error) {
	var err error
	if source == nil {
		source, err = service.sources.LoadVoiceAudio(ctx, claim.Draft)
		if err != nil {
			return service.failTranscription(
				ctx, claim, string(ErrorProviderUnavailable), true,
			)
		}
		defer source.Close()
	}
	request := TranscriptionRequest{Audio: source}
	var result TranscriptionResult
	if observer == nil {
		result, err = service.recognizer.Transcribe(ctx, request)
	} else {
		result, err = service.recognizer.TranscribeStream(ctx, request, observer)
	}
	if err != nil {
		kind, retryable := speechFailure(err)
		return service.failTranscription(ctx, claim, kind, retryable)
	}
	if !ValidTranscription(result) {
		return service.failTranscription(
			ctx, claim, string(ErrorInvalidResponse), true,
		)
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.CompleteTranscription(
		persistContext,
		claim.Draft.OwnerID,
		claim.Draft.ID,
		claim.FencingToken,
		result,
	)
}

func (service *Service) failTranscription(
	ctx context.Context,
	claim TranscriptionClaim,
	kind string,
	retryable bool,
) (Draft, error) {
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.FailTranscription(
		persistContext,
		claim.Draft.OwnerID,
		claim.Draft.ID,
		claim.FencingToken,
		kind,
		retryable,
	)
}

func (service *Service) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmDraftCommand,
) (Confirmation, error) {
	return service.confirm(ctx, actor, command, nil)
}

func (service *Service) ConfirmStream(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmDraftCommand,
	observer ConfirmationStreamObserver,
) (Confirmation, error) {
	if observer == nil {
		return Confirmation{}, ErrInvalidRequest
	}
	return service.confirm(ctx, actor, command, observer)
}

func (service *Service) confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmDraftCommand,
	observer ConfirmationStreamObserver,
) (Confirmation, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(command.DraftID) ||
		command.Version < 1 ||
		!ValidClientMessageID(command.ClientMessageID) ||
		!ValidMessageContent(command.ConfirmedText) {
		return Confirmation{}, ErrInvalidRequest
	}
	command.Configuration = service.config.Configuration
	confirmation, err := service.repository.ConfirmDraft(ctx, actor.UserID, command)
	if err != nil {
		return Confirmation{}, err
	}
	reference, feedbackErr := service.feedback.EnsureMessage(
		ctx, actor, confirmation.Message.ThreadID, confirmation.Message.ID,
	)
	if feedbackErr != nil {
		return Confirmation{}, feedbackErr
	}
	if strings.TrimSpace(reference.StatusURL) != "" {
		confirmation.Message.SpeechFeedbackStatusURL = reference.StatusURL
	}
	if observer != nil {
		if err := observer.OnConfirmationCommitted(ctx, confirmation); err != nil {
			return Confirmation{}, err
		}
	}
	if confirmation.Run.Status != run.StatusPending {
		return confirmation, nil
	}
	if observer == nil {
		confirmation.Run, err = service.runs.ProcessPending(
			ctx, actor, confirmation.Run,
		)
	} else {
		confirmation.Run, err = service.runs.ProcessPendingStream(
			ctx,
			actor,
			confirmation.Run,
			confirmationRunObserver{delegate: observer},
		)
	}
	return confirmation, err
}

type confirmationRunObserver struct {
	delegate ConfirmationStreamObserver
}

func (confirmationRunObserver) OnInputCommitted(context.Context, run.Submission) error {
	return nil
}

func (observer confirmationRunObserver) OnToolStarted(
	ctx context.Context,
	step run.ToolStep,
) error {
	return observer.delegate.OnToolStarted(ctx, step)
}

func (observer confirmationRunObserver) OnToolCompleted(
	ctx context.Context,
	step run.ToolStep,
) error {
	return observer.delegate.OnToolCompleted(ctx, step)
}

func (observer confirmationRunObserver) OnToolFailed(
	ctx context.Context,
	step run.ToolStep,
) error {
	return observer.delegate.OnToolFailed(ctx, step)
}

func (observer confirmationRunObserver) OnAssistantOutputStarted(
	ctx context.Context,
	output run.AssistantOutput,
) error {
	return observer.delegate.OnAssistantOutputStarted(ctx, output)
}

func (observer confirmationRunObserver) OnAssistantOutputDelta(
	ctx context.Context,
	delta run.AssistantOutputDelta,
) error {
	return observer.delegate.OnAssistantOutputDelta(ctx, delta)
}

func (observer confirmationRunObserver) OnAssistantOutputCompleted(
	ctx context.Context,
	output run.AssistantOutput,
) error {
	return observer.delegate.OnAssistantOutputCompleted(ctx, output)
}

func (service *Service) Playback(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) (objectstore.SignedGetResult, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(audioID) {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	attachment, err := service.repository.FindAudioAttachment(
		ctx, actor.UserID, audioID,
	)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	result, err := service.media.SignedGet(ctx, actor.UserID, attachment.ID)
	if err != nil {
		return objectstore.SignedGetResult{}, mapMediaError(err)
	}
	return result, nil
}

func (service *Service) DeleteDraft(
	ctx context.Context,
	actor requestcontext.Actor,
	draftID string,
) error {
	if ctx == nil || !actor.Valid() || !ValidUUID(draftID) {
		return ErrNotFound
	}
	if err := service.repository.DiscardDraft(ctx, actor.UserID, draftID); err != nil &&
		!errors.Is(err, ErrNotFound) {
		return err
	}
	if err := service.media.Delete(ctx, actor.UserID, draftID); err != nil &&
		!errors.Is(err, sharedmedia.ErrNotFound) {
		return mapMediaError(err)
	}
	return nil
}

func (service *Service) DeleteAudio(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) error {
	if ctx == nil || !actor.Valid() || !ValidUUID(audioID) {
		return ErrNotFound
	}
	err := service.repository.DetachAudio(ctx, actor.UserID, audioID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := service.media.Delete(ctx, actor.UserID, audioID); err != nil {
		return errors.Join(ErrCleanupPending, mapMediaError(err))
	}
	return nil
}

func (service *Service) SynthesizeMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
	previewText string,
) (SynthesisResult, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(messageID) {
		return SynthesisResult{}, ErrNotFound
	}
	message, err := service.repository.FindMessageByID(ctx, actor.UserID, messageID)
	if err != nil {
		return SynthesisResult{}, err
	}
	text := message.Content
	if previewText != "" {
		text = previewText
	} else if message.Role != conversation.MessageRoleAssistant {
		return SynthesisResult{}, ErrNotFound
	}
	return service.synthesizer.Synthesize(ctx, SynthesisRequest{Text: text})
}

func voiceAudioChecksum(source platformmedia.AudioSource) (string, error) {
	reader, err := source.Open()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, io.LimitReader(reader, source.Size()+1))
	closeErr := reader.Close()
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(copyErr, closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sameVoiceUpload(
	draft Draft,
	audio platformmedia.AudioSource,
	checksum string,
) bool {
	return draft.ContentType == audio.MediaType() &&
		draft.Size == audio.Size() && draft.Duration == audio.Duration() &&
		draft.SampleRate == audio.SampleRate() && draft.ChecksumSHA256 == checksum
}

func speechFailure(err error) (string, bool) {
	var speechError *SpeechError
	if errors.As(err, &speechError) {
		return string(speechError.Kind), speechError.Retryable()
	}
	return string(ErrorProviderUnavailable), true
}

func mapMediaError(err error) error {
	switch {
	case errors.Is(err, sharedmedia.ErrInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, sharedmedia.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, sharedmedia.ErrConflict):
		return ErrConflict
	case errors.Is(err, sharedmedia.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	default:
		return err
	}
}

var _ Application = (*Service)(nil)
