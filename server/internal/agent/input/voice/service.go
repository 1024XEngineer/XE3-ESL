package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	defaultVoiceCandidateTTL     = 24 * time.Hour
	defaultVoiceUploadLease      = 2 * time.Minute
	defaultVoiceASRLease         = 2 * time.Minute
	defaultVoiceCleanupLease     = 5 * time.Minute
	defaultVoicePlaybackTTL      = 2 * time.Minute
	defaultVoiceCleanupBatchSize = 4
)

// AudioSourceLoader reconstructs a provider-facing source from a private
// object after a process restart. The production adapter may use a short-lived
// signed GET internally; the URL never crosses the Agent application boundary.
type AudioSourceLoader interface {
	LoadVoiceAudio(
		context.Context,
		Candidate,
	) (platformmedia.ManagedAudioSource, error)
}

type PendingRunProcessor interface {
	ProcessPending(
		context.Context,
		requestcontext.Actor,
		run.Run,
	) (run.Run, error)
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
	CandidateTTL     time.Duration
	UploadLease      time.Duration
	ASRLease         time.Duration
	PlaybackTTL      time.Duration
}

type UploadRequest struct {
	ThreadID       string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type Application interface {
	Upload(
		context.Context,
		requestcontext.Actor,
		UploadRequest,
	) (Candidate, error)
	UploadStream(
		context.Context,
		requestcontext.Actor,
		UploadRequest,
		ai.TranscriptionObserver,
	) (Candidate, error)
	GetCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (Candidate, error)
	Retry(
		context.Context,
		requestcontext.Actor,
		string,
	) (Candidate, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		ConfirmCandidateCommand,
	) (Confirmation, error)
	Playback(
		context.Context,
		requestcontext.Actor,
		string,
	) (objectstore.SignedGetResult, error)
	DeleteCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) error
	DeleteAudio(
		context.Context,
		requestcontext.Actor,
		string,
	) error
	SynthesizeMessage(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (ai.SynthesisResult, error)
}

type Service struct {
	repository  Repository
	store       objectstore.Store
	sources     AudioSourceLoader
	recognizer  ai.StreamingSpeechRecognizer
	synthesizer ai.SpeechSynthesizer
	runs        PendingRunProcessor
	feedback    FeedbackPort
	ids         IDGenerator
	clock       func() time.Time
	config      Config
}

func NewService(
	repository Repository,
	store objectstore.Store,
	sources AudioSourceLoader,
	recognizer ai.StreamingSpeechRecognizer,
	synthesizer ai.SpeechSynthesizer,
	runs PendingRunProcessor,
	ids IDGenerator,
	config Config,
	feedbackPorts ...FeedbackPort,
) (*Service, error) {
	if nilDependency(repository) ||
		nilDependency(store) ||
		nilDependency(sources) ||
		nilDependency(recognizer) ||
		nilDependency(synthesizer) ||
		nilDependency(runs) ||
		nilDependency(ids) ||
		!validConfiguration(config.Configuration) {
		return nil, errors.New("agent voice input: dependencies are required")
	}
	if len(feedbackPorts) > 1 ||
		(len(feedbackPorts) == 1 &&
			nilDependency(feedbackPorts[0])) {
		return nil, errors.New("agent voice input: feedback port is invalid")
	}
	if config.CandidateTTL <= 0 {
		config.CandidateTTL = defaultVoiceCandidateTTL
	}
	if config.UploadLease <= 0 {
		config.UploadLease = defaultVoiceUploadLease
	}
	if config.UploadLease < time.Second ||
		config.UploadLease > 10*time.Minute {
		return nil, ErrInvalidRequest
	}
	if config.ASRLease <= 0 {
		config.ASRLease = defaultVoiceASRLease
	}
	if config.PlaybackTTL <= 0 {
		config.PlaybackTTL = defaultVoicePlaybackTTL
	}
	if config.PlaybackTTL > 2*time.Minute {
		return nil, ErrInvalidRequest
	}
	service := &Service{
		repository:  repository,
		store:       store,
		sources:     sources,
		recognizer:  recognizer,
		synthesizer: synthesizer,
		runs:        runs,
		ids:         ids,
		clock:       func() time.Time { return time.Now().UTC() },
		config:      config,
	}
	if len(feedbackPorts) == 1 {
		service.feedback = feedbackPorts[0]
	}
	return service, nil
}

func (service *Service) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
) (Candidate, error) {
	return service.upload(ctx, actor, request, nil)
}

func (service *Service) UploadStream(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
	observer ai.TranscriptionObserver,
) (Candidate, error) {
	if observer == nil {
		return Candidate{}, ErrInvalidRequest
	}
	return service.upload(ctx, actor, request, observer)
}

func (service *Service) upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadRequest,
	observer ai.TranscriptionObserver,
) (Candidate, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(request.ThreadID) ||
		!validIdempotencyKey(request.IdempotencyKey) ||
		request.Audio == nil {
		return Candidate{}, ErrInvalidRequest
	}
	audio, err := platformmedia.CaptureTemporaryAudio(
		service.config.ScratchDirectory,
		request.ContentType,
		request.Audio,
	)
	if err != nil {
		return Candidate{}, ErrInvalidRequest
	}
	defer audio.Close()

	checksum, err := voiceAudioChecksum(audio)
	if err != nil {
		return Candidate{}, ErrRepository
	}
	candidateID, err := service.ids.NewID()
	if err != nil {
		return Candidate{}, ErrRepository
	}
	now := service.clock()
	stage, err := service.repository.StageCandidate(
		ctx,
		StageCandidateCommand{Candidate: Candidate{
			ID:              candidateID,
			OwnerID:         actor.UserID,
			ThreadID:        request.ThreadID,
			UploadRequestID: request.IdempotencyKey,
			ObjectKey:       ObjectPrefix + candidateID + ".wav",
			ContentType:     audio.MediaType(),
			Size:            audio.Size(),
			ChecksumSHA256:  checksum,
			Duration:        audio.Duration(),
			SampleRate:      audio.SampleRate(),
			Status:          StatusStaged,
			ExpiresAt:       now.Add(service.config.CandidateTTL),
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
	)
	if err != nil {
		return Candidate{}, err
	}
	candidate := stage.Candidate
	if !sameVoiceUpload(candidate, audio, checksum) {
		return Candidate{}, ErrIdempotencyConflict
	}
	if candidate.Status == StatusTranscribing {
		// A live lease returns the current candidate; an expired lease is
		// atomically retaken and fenced by the Repository.
		return service.transcribe(ctx, actor, candidate.ID, audio, observer)
	}
	if candidate.Status != StatusStaged {
		return candidate, nil
	}
	if candidate.ETag == "" {
		upload, acquired, claimErr :=
			service.repository.ClaimUpload(
				ctx,
				actor.UserID,
				candidate.ID,
				service.config.UploadLease,
			)
		if claimErr != nil {
			return Candidate{}, claimErr
		}
		candidate = upload.Candidate
		if !acquired {
			return candidate, nil
		}
		putDeadline, ok := voiceUploadPutDeadline(
			service.clock(),
			upload.LeaseExpiresAt,
			service.config.UploadLease,
		)
		if !ok {
			return Candidate{}, ErrCandidateProcessing
		}
		putContext, cancelPut := context.WithDeadline(ctx, putDeadline)
		defer cancelPut()
		reader, openErr := audio.Open()
		if openErr != nil {
			return Candidate{}, ErrRepository
		}
		seeker, ok := reader.(io.ReadSeeker)
		if !ok {
			_ = reader.Close()
			return Candidate{}, ErrRepository
		}
		put, putErr := service.store.Put(
			putContext,
			objectstore.PutRequest{
				Key:            candidate.ObjectKey,
				Body:           seeker,
				Size:           candidate.Size,
				ContentType:    candidate.ContentType,
				ChecksumSHA256: candidate.ChecksumSHA256,
			},
		)
		closeErr := reader.Close()
		if putErr != nil || closeErr != nil {
			return Candidate{}, errors.Join(putErr, closeErr)
		}
		candidate, err = service.repository.CommitUpload(
			ctx,
			actor.UserID,
			candidate.ID,
			upload.FencingToken,
			put.ETag,
		)
		if err != nil {
			// The durable upload lease remains. Replay can reconcile an
			// ambiguous Commit, while cleanup waits for lease expiry before
			// deleting a possibly late object.
			return Candidate{}, err
		}
	}
	return service.transcribe(ctx, actor, candidate.ID, audio, observer)
}

func voiceUploadPutDeadline(
	now time.Time,
	leaseExpiresAt time.Time,
	leaseDuration time.Duration,
) (time.Time, bool) {
	reserve := leaseDuration / 10
	if reserve < 500*time.Millisecond {
		reserve = 500 * time.Millisecond
	}
	if reserve > 5*time.Second {
		reserve = 5 * time.Second
	}
	deadline := leaseExpiresAt.Add(-reserve)
	return deadline, deadline.After(now)
}

func (service *Service) GetCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (Candidate, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(candidateID) {
		return Candidate{}, ErrNotFound
	}
	return service.repository.FindCandidate(
		ctx,
		actor.UserID,
		candidateID,
	)
}

func (service *Service) Retry(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (Candidate, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(candidateID) {
		return Candidate{}, ErrNotFound
	}
	current, err := service.repository.FindCandidate(
		ctx,
		actor.UserID,
		candidateID,
	)
	if err != nil {
		return Candidate{}, err
	}
	if current.Status != StatusTranscribing &&
		(current.Status != StatusFailed || !current.FailureRetryable) {
		return Candidate{}, ErrConflict
	}
	return service.transcribe(ctx, actor, candidateID, nil, nil)
}

func (service *Service) transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
	source platformmedia.ManagedAudioSource,
	observer ai.TranscriptionObserver,
) (Candidate, error) {
	claim, acquired, err := service.repository.ClaimTranscription(
		ctx,
		actor.UserID,
		candidateID,
		service.config.ASRLease,
	)
	if err != nil {
		return Candidate{}, err
	}
	if !acquired {
		return claim.Candidate, nil
	}
	if source == nil {
		source, err = service.sources.LoadVoiceAudio(ctx, claim.Candidate)
		if err != nil {
			return service.failTranscription(
				ctx,
				claim,
				string(ai.ErrorProviderUnavailable),
				true,
			)
		}
		defer source.Close()
	}
	request := ai.TranscriptionRequest{Audio: source}
	var result ai.TranscriptionResult
	var providerErr error
	if observer == nil {
		result, providerErr = service.recognizer.Transcribe(ctx, request)
	} else {
		result, providerErr = service.recognizer.TranscribeStream(
			ctx,
			request,
			observer,
		)
	}
	if providerErr != nil {
		kind, retryable := speechFailure(providerErr)
		return service.failTranscription(ctx, claim, kind, retryable)
	}
	if !ValidTranscription(result) {
		return service.failTranscription(
			ctx,
			claim,
			string(ai.ErrorInvalidResponse),
			true,
		)
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.CompleteTranscription(
		persistContext,
		claim.Candidate.OwnerID,
		claim.Candidate.ID,
		claim.FencingToken,
		result,
	)
}

func (service *Service) failTranscription(
	ctx context.Context,
	claim TranscriptionClaim,
	kind string,
	retryable bool,
) (Candidate, error) {
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.FailTranscription(
		persistContext,
		claim.Candidate.OwnerID,
		claim.Candidate.ID,
		claim.FencingToken,
		kind,
		retryable,
	)
}

func (service *Service) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmCandidateCommand,
) (Confirmation, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(command.CandidateID) ||
		command.CandidateVersion < 1 ||
		!ValidClientMessageID(command.ClientMessageID) ||
		!ValidMessageContent(command.ConfirmedText) {
		return Confirmation{}, ErrInvalidRequest
	}
	command.Configuration = service.config.Configuration
	confirmation, err := service.repository.ConfirmCandidate(
		ctx,
		actor.UserID,
		command,
	)
	if err != nil {
		return Confirmation{}, err
	}
	if service.feedback != nil {
		reference, feedbackErr :=
			service.feedback.EnsureMessage(
				ctx,
				actor,
				confirmation.Message.ThreadID,
				confirmation.Message.ID,
			)
		if feedbackErr != nil {
			return Confirmation{}, feedbackErr
		}
		if strings.TrimSpace(reference.StatusURL) != "" {
			confirmation.Message.SpeechFeedbackStatusURL =
				reference.StatusURL
		}
	}
	if confirmation.Run.Status == run.StatusPending {
		confirmation.Run, err = service.runs.ProcessPending(
			ctx,
			actor,
			confirmation.Run,
		)
		if err != nil {
			return Confirmation{}, err
		}
	}
	return confirmation, nil
}

func (service *Service) Playback(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) (objectstore.SignedGetResult, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(audioID) {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	audio, err := service.repository.FindMessageAudio(
		ctx,
		actor.UserID,
		audioID,
	)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if audio.Status != conversation.MessageAudioReadable {
		return objectstore.SignedGetResult{}, ErrNotFound
	}
	result, err := service.store.SignedGet(ctx, audio.ObjectKey)
	if err != nil {
		return objectstore.SignedGetResult{}, err
	}
	now := service.clock()
	if !result.ExpiresAt.After(now) ||
		result.ExpiresAt.After(now.Add(service.config.PlaybackTTL)) {
		return objectstore.SignedGetResult{}, ErrRepository
	}
	signedURL, parseErr := url.Parse(result.URL)
	if parseErr != nil ||
		!strings.EqualFold(signedURL.Scheme, "https") ||
		signedURL.Host == "" {
		return objectstore.SignedGetResult{}, ErrRepository
	}
	return result, nil
}

func (service *Service) DeleteCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) error {
	if ctx == nil || !actor.Valid() || !ValidUUID(candidateID) {
		return ErrNotFound
	}
	candidate, err := service.repository.BeginCandidateDeletion(
		ctx,
		actor.UserID,
		candidateID,
	)
	if err != nil {
		return err
	}
	if candidate.Status == StatusDeleted {
		return nil
	}
	if err := service.store.Delete(ctx, candidate.ObjectKey); err != nil {
		return errors.Join(ErrCleanupPending, err)
	}
	_, err = service.repository.FinishCandidateDeletion(
		ctx,
		actor.UserID,
		candidate.ID,
	)
	return err
}

func (service *Service) DeleteAudio(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) error {
	if ctx == nil || !actor.Valid() || !ValidUUID(audioID) {
		return ErrNotFound
	}
	audio, err := service.repository.BeginMessageAudioDeletion(
		ctx,
		actor.UserID,
		audioID,
	)
	if err != nil {
		return err
	}
	if audio.Status == conversation.MessageAudioDeleted {
		return nil
	}
	if err := service.store.Delete(ctx, audio.ObjectKey); err != nil {
		return errors.Join(ErrCleanupPending, err)
	}
	_, err = service.repository.FinishMessageAudioDeletion(
		ctx,
		actor.UserID,
		audio.ID,
	)
	return err
}

func (service *Service) SynthesizeMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
	previewText string,
) (ai.SynthesisResult, error) {
	if ctx == nil || !actor.Valid() || !ValidUUID(messageID) {
		return ai.SynthesisResult{}, ErrNotFound
	}
	message, err := findOwnedMessageForSpeech(
		ctx,
		service.repository,
		actor.UserID,
		messageID,
	)
	if err != nil {
		return ai.SynthesisResult{}, err
	}
	text := message.Content
	if previewText != "" {
		text = previewText
	} else if message.Role != conversation.MessageRoleAssistant {
		return ai.SynthesisResult{}, ErrNotFound
	}
	return service.synthesizer.Synthesize(
		ctx,
		ai.SynthesisRequest{Text: text},
	)
}

// ReclaimObjects retries deleting objects and expires unconfirmed
// candidates under a database lease. A failed delete is released for retry.
func (service *Service) ReclaimObjects(
	ctx context.Context,
	limit int,
) (CleanupResult, error) {
	if limit <= 0 || limit > defaultVoiceCleanupBatchSize {
		limit = defaultVoiceCleanupBatchSize
	}
	claims, err := service.repository.ClaimCleanup(
		ctx,
		defaultVoiceCleanupLease,
		limit,
	)
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{}
	for _, claim := range claims {
		if deleteErr := service.store.Delete(ctx, claim.ObjectKey); deleteErr != nil {
			result.Failed++
			_ = service.repository.ReleaseCleanup(ctx, claim)
			continue
		}
		if finishErr := service.repository.FinishCleanup(
			ctx,
			claim,
		); finishErr != nil {
			result.Failed++
			_ = service.repository.ReleaseCleanup(ctx, claim)
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func findOwnedMessageForSpeech(
	ctx context.Context,
	repository Repository,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	return repository.FindMessageByID(ctx, ownerID, messageID)
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
	candidate Candidate,
	audio platformmedia.AudioSource,
	checksum string,
) bool {
	return candidate.ContentType == audio.MediaType() &&
		candidate.Size == audio.Size() &&
		candidate.Duration == audio.Duration() &&
		candidate.SampleRate == audio.SampleRate() &&
		candidate.ChecksumSHA256 == checksum
}

func speechFailure(err error) (string, bool) {
	var speechError *ai.SpeechError
	if errors.As(err, &speechError) {
		return string(speechError.Kind), speechError.Retryable()
	}
	return string(ai.ErrorProviderUnavailable), true
}

var _ Application = (*Service)(nil)
