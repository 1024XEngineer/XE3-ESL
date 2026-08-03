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

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	agentVoiceObjectPrefix       = "audio/v1/agent/"
	defaultVoiceCandidateTTL     = 24 * time.Hour
	defaultVoiceUploadLease      = 2 * time.Minute
	defaultVoiceASRLease         = 2 * time.Minute
	defaultVoiceCleanupLease     = 5 * time.Minute
	defaultVoicePlaybackTTL      = 2 * time.Minute
	defaultVoiceCleanupBatchSize = 4
)

// VoiceAudioSourceLoader reconstructs a provider-facing source from a private
// object after a process restart. The production adapter may use a short-lived
// signed GET internally; the URL never crosses the Agent application boundary.
type VoiceAudioSourceLoader interface {
	LoadVoiceAudio(
		context.Context,
		VoiceCandidate,
	) (platformmedia.ManagedAudioSource, error)
}

type VoicePendingRunProcessor interface {
	ProcessPending(
		context.Context,
		requestcontext.Actor,
		Run,
	) (Run, error)
}

type VoiceMessageFeedbackReference struct {
	StatusURL string
}

type VoiceMessageFeedbackPort interface {
	EnsureAgentVoiceMessage(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoiceMessageFeedbackReference, error)
}

type VoiceMessageConfig struct {
	RunConfiguration RunConfiguration
	ScratchDirectory string
	CandidateTTL     time.Duration
	UploadLease      time.Duration
	ASRLease         time.Duration
	PlaybackTTL      time.Duration
}

type UploadVoiceCandidateRequest struct {
	ThreadID       string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type VoiceMessageApplication interface {
	Upload(
		context.Context,
		requestcontext.Actor,
		UploadVoiceCandidateRequest,
	) (VoiceCandidate, error)
	GetCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (VoiceCandidate, error)
	Retry(
		context.Context,
		requestcontext.Actor,
		string,
	) (VoiceCandidate, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		ConfirmVoiceCandidateCommand,
	) (VoiceConfirmation, error)
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
	) (ai.SynthesisResult, error)
}

type VoiceMessageService struct {
	repository  VoiceMessageRepository
	store       objectstore.Store
	sources     VoiceAudioSourceLoader
	recognizer  ai.SpeechRecognizer
	synthesizer ai.SpeechSynthesizer
	runs        VoicePendingRunProcessor
	feedback    VoiceMessageFeedbackPort
	ids         IDGenerator
	clock       func() time.Time
	config      VoiceMessageConfig
}

func NewVoiceMessageService(
	repository VoiceMessageRepository,
	store objectstore.Store,
	sources VoiceAudioSourceLoader,
	recognizer ai.SpeechRecognizer,
	synthesizer ai.SpeechSynthesizer,
	runs VoicePendingRunProcessor,
	ids IDGenerator,
	config VoiceMessageConfig,
	feedbackPorts ...VoiceMessageFeedbackPort,
) (*VoiceMessageService, error) {
	if nilVoiceDependency(repository) ||
		nilVoiceDependency(store) ||
		nilVoiceDependency(sources) ||
		nilVoiceDependency(recognizer) ||
		nilVoiceDependency(synthesizer) ||
		nilVoiceDependency(runs) ||
		nilVoiceDependency(ids) ||
		!validRunConfiguration(config.RunConfiguration) {
		return nil, errors.New("agent: voice message dependency is required")
	}
	if len(feedbackPorts) > 1 ||
		(len(feedbackPorts) == 1 &&
			nilVoiceDependency(feedbackPorts[0])) {
		return nil, errors.New(
			"agent: voice message feedback dependency is invalid",
		)
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
	service := &VoiceMessageService{
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

func (service *VoiceMessageService) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	request UploadVoiceCandidateRequest,
) (VoiceCandidate, error) {
	if ctx == nil || !actor.Valid() || !validUUID(request.ThreadID) ||
		!validVoiceIdempotencyKey(request.IdempotencyKey) ||
		request.Audio == nil {
		return VoiceCandidate{}, ErrInvalidRequest
	}
	audio, err := platformmedia.CaptureTemporaryAudio(
		service.config.ScratchDirectory,
		request.ContentType,
		request.Audio,
	)
	if err != nil {
		return VoiceCandidate{}, ErrInvalidRequest
	}
	defer audio.Close()

	checksum, err := voiceAudioChecksum(audio)
	if err != nil {
		return VoiceCandidate{}, ErrRepository
	}
	candidateID, err := service.ids.NewID()
	if err != nil {
		return VoiceCandidate{}, ErrRepository
	}
	now := service.clock()
	stage, err := service.repository.StageVoiceCandidate(
		ctx,
		StageVoiceCandidateCommand{Candidate: VoiceCandidate{
			ID:              candidateID,
			OwnerID:         actor.UserID,
			ThreadID:        request.ThreadID,
			UploadRequestID: request.IdempotencyKey,
			ObjectKey:       agentVoiceObjectPrefix + candidateID + ".wav",
			ContentType:     audio.MediaType(),
			Size:            audio.Size(),
			ChecksumSHA256:  checksum,
			Duration:        audio.Duration(),
			SampleRate:      audio.SampleRate(),
			Status:          VoiceCandidateStaged,
			ExpiresAt:       now.Add(service.config.CandidateTTL),
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
	)
	if err != nil {
		return VoiceCandidate{}, err
	}
	candidate := stage.Candidate
	if !sameVoiceUpload(candidate, audio, checksum) {
		return VoiceCandidate{}, ErrIdempotencyConflict
	}
	if candidate.Status == VoiceCandidateTranscribing {
		// A live lease returns the current candidate; an expired lease is
		// atomically retaken and fenced by the Repository.
		return service.transcribe(ctx, actor, candidate.ID, audio)
	}
	if candidate.Status != VoiceCandidateStaged {
		return candidate, nil
	}
	if candidate.ETag == "" {
		upload, acquired, claimErr :=
			service.repository.ClaimVoiceUpload(
				ctx,
				actor.UserID,
				candidate.ID,
				service.config.UploadLease,
			)
		if claimErr != nil {
			return VoiceCandidate{}, claimErr
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
			return VoiceCandidate{}, ErrVoiceCandidateProcessing
		}
		putContext, cancelPut := context.WithDeadline(ctx, putDeadline)
		defer cancelPut()
		reader, openErr := audio.Open()
		if openErr != nil {
			return VoiceCandidate{}, ErrRepository
		}
		seeker, ok := reader.(io.ReadSeeker)
		if !ok {
			_ = reader.Close()
			return VoiceCandidate{}, ErrRepository
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
			return VoiceCandidate{}, errors.Join(putErr, closeErr)
		}
		candidate, err = service.repository.CommitVoiceUpload(
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
			return VoiceCandidate{}, err
		}
	}
	return service.transcribe(ctx, actor, candidate.ID, audio)
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

func (service *VoiceMessageService) GetCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (VoiceCandidate, error) {
	if ctx == nil || !actor.Valid() || !validUUID(candidateID) {
		return VoiceCandidate{}, ErrNotFound
	}
	return service.repository.FindVoiceCandidate(
		ctx,
		actor.UserID,
		candidateID,
	)
}

func (service *VoiceMessageService) Retry(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (VoiceCandidate, error) {
	if ctx == nil || !actor.Valid() || !validUUID(candidateID) {
		return VoiceCandidate{}, ErrNotFound
	}
	current, err := service.repository.FindVoiceCandidate(
		ctx,
		actor.UserID,
		candidateID,
	)
	if err != nil {
		return VoiceCandidate{}, err
	}
	if current.Status != VoiceCandidateTranscribing &&
		(current.Status != VoiceCandidateFailed || !current.FailureRetryable) {
		return VoiceCandidate{}, ErrConflict
	}
	return service.transcribe(ctx, actor, candidateID, nil)
}

func (service *VoiceMessageService) transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
	source platformmedia.ManagedAudioSource,
) (VoiceCandidate, error) {
	claim, acquired, err := service.repository.ClaimVoiceTranscription(
		ctx,
		actor.UserID,
		candidateID,
		service.config.ASRLease,
	)
	if err != nil {
		return VoiceCandidate{}, err
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
	result, providerErr := service.recognizer.Transcribe(
		ctx,
		ai.TranscriptionRequest{Audio: source},
	)
	if providerErr != nil {
		kind, retryable := speechFailure(providerErr)
		return service.failTranscription(ctx, claim, kind, retryable)
	}
	if !validVoiceTranscription(result) {
		return service.failTranscription(
			ctx,
			claim,
			string(ai.ErrorInvalidResponse),
			true,
		)
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.CompleteVoiceTranscription(
		persistContext,
		claim.Candidate.OwnerID,
		claim.Candidate.ID,
		claim.FencingToken,
		result,
	)
}

func (service *VoiceMessageService) failTranscription(
	ctx context.Context,
	claim VoiceTranscriptionClaim,
	kind string,
	retryable bool,
) (VoiceCandidate, error) {
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.FailVoiceTranscription(
		persistContext,
		claim.Candidate.OwnerID,
		claim.Candidate.ID,
		claim.FencingToken,
		kind,
		retryable,
	)
}

func (service *VoiceMessageService) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceCandidateCommand,
) (VoiceConfirmation, error) {
	if ctx == nil || !actor.Valid() || !validUUID(command.CandidateID) ||
		command.CandidateVersion < 1 ||
		!validClientMessageID(command.ClientMessageID) ||
		!validMessageContent(command.ConfirmedText) {
		return VoiceConfirmation{}, ErrInvalidRequest
	}
	command.Configuration = service.config.RunConfiguration
	confirmation, err := service.repository.ConfirmVoiceCandidate(
		ctx,
		actor.UserID,
		command,
	)
	if err != nil {
		return VoiceConfirmation{}, err
	}
	if service.feedback != nil {
		reference, feedbackErr :=
			service.feedback.EnsureAgentVoiceMessage(
				ctx,
				actor,
				confirmation.Message.ThreadID,
				confirmation.Message.ID,
			)
		if feedbackErr != nil {
			return VoiceConfirmation{}, feedbackErr
		}
		if strings.TrimSpace(reference.StatusURL) != "" {
			confirmation.Message.SpeechFeedbackStatusURL =
				reference.StatusURL
		}
	}
	if confirmation.Run.Status == RunStatusPending {
		confirmation.Run, err = service.runs.ProcessPending(
			ctx,
			actor,
			confirmation.Run,
		)
		if err != nil {
			return VoiceConfirmation{}, err
		}
	}
	return confirmation, nil
}

func (service *VoiceMessageService) Playback(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) (objectstore.SignedGetResult, error) {
	if ctx == nil || !actor.Valid() || !validUUID(audioID) {
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
	if audio.Status != MessageAudioReadable {
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

func (service *VoiceMessageService) DeleteCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) error {
	if ctx == nil || !actor.Valid() || !validUUID(candidateID) {
		return ErrNotFound
	}
	candidate, err := service.repository.BeginVoiceCandidateDeletion(
		ctx,
		actor.UserID,
		candidateID,
	)
	if err != nil {
		return err
	}
	if candidate.Status == VoiceCandidateDeleted {
		return nil
	}
	if err := service.store.Delete(ctx, candidate.ObjectKey); err != nil {
		return errors.Join(ErrVoiceCleanupPending, err)
	}
	_, err = service.repository.FinishVoiceCandidateDeletion(
		ctx,
		actor.UserID,
		candidate.ID,
	)
	return err
}

func (service *VoiceMessageService) DeleteAudio(
	ctx context.Context,
	actor requestcontext.Actor,
	audioID string,
) error {
	if ctx == nil || !actor.Valid() || !validUUID(audioID) {
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
	if audio.Status == MessageAudioDeleted {
		return nil
	}
	if err := service.store.Delete(ctx, audio.ObjectKey); err != nil {
		return errors.Join(ErrVoiceCleanupPending, err)
	}
	_, err = service.repository.FinishMessageAudioDeletion(
		ctx,
		actor.UserID,
		audio.ID,
	)
	return err
}

func (service *VoiceMessageService) SynthesizeMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
) (ai.SynthesisResult, error) {
	if ctx == nil || !actor.Valid() || !validUUID(messageID) {
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
	if message.Role != MessageRoleAssistant {
		return ai.SynthesisResult{}, ErrNotFound
	}
	return service.synthesizer.Synthesize(
		ctx,
		ai.SynthesisRequest{Text: message.Content},
	)
}

// ReclaimVoiceObjects retries deleting objects and expires unconfirmed
// candidates under a database lease. A failed delete is released for retry.
func (service *VoiceMessageService) ReclaimVoiceObjects(
	ctx context.Context,
	limit int,
) (VoiceCleanupResult, error) {
	if limit <= 0 || limit > defaultVoiceCleanupBatchSize {
		limit = defaultVoiceCleanupBatchSize
	}
	claims, err := service.repository.ClaimVoiceCleanup(
		ctx,
		defaultVoiceCleanupLease,
		limit,
	)
	if err != nil {
		return VoiceCleanupResult{}, err
	}
	result := VoiceCleanupResult{}
	for _, claim := range claims {
		if deleteErr := service.store.Delete(ctx, claim.ObjectKey); deleteErr != nil {
			result.Failed++
			_ = service.repository.ReleaseVoiceCleanup(ctx, claim)
			continue
		}
		if finishErr := service.repository.FinishVoiceCleanup(
			ctx,
			claim,
		); finishErr != nil {
			result.Failed++
			_ = service.repository.ReleaseVoiceCleanup(ctx, claim)
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func findOwnedMessageForSpeech(
	ctx context.Context,
	repository VoiceMessageRepository,
	ownerID string,
	messageID string,
) (Message, error) {
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
	candidate VoiceCandidate,
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
