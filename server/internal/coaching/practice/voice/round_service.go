package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrVoiceRoundInvalid    = errors.New("voice_round_invalid")
	ErrVoiceRoundNotFound   = errors.New("voice_round_not_found")
	ErrVoiceRoundConflict   = errors.New("voice_round_idempotency_conflict")
	ErrVoiceRoundProcessing = errors.New("voice_round_processing")
	ErrVoiceRoundCapacity   = errors.New("voice_round_capacity")
)

const voicePersistenceTimeout = 5 * time.Second

type TranscriptionCandidate struct {
	ID                      string
	ReservationID           string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	TranscriptID            string
	EvidenceVersion         int64
	Transcript              string
	Provider                string
	Model                   string
	ProviderRequestID       string
	CreatedAt               time.Time
}

type SafeProcessingAttempt struct {
	Operation  ProviderOperation
	Kind       ProviderErrorKind
	Retryable  bool
	RequestID  string
	Duration   time.Duration
	OccurredAt time.Time
}

type TranscriptionReservationStatus string

const (
	TranscriptionReserved   TranscriptionReservationStatus = "reserved"
	TranscriptionProcessing TranscriptionReservationStatus = "processing"
	TranscriptionCompleted  TranscriptionReservationStatus = "completed"
)

type TranscriptionReservation struct {
	ID         string
	LeaseToken string
	Status     TranscriptionReservationStatus
	Candidate  TranscriptionCandidate
}

type ReserveTranscriptionCommand struct {
	SessionID               string
	QuestionID              string
	RespondentParticipantID string
	IdempotencyKey          string
	InputFingerprint        string
}

type CompleteTranscriptionCommand struct {
	ReservationID     string
	LeaseToken        string
	TranscriptID      string
	EvidenceVersion   int64
	Transcript        string
	Provider          string
	Model             string
	ProviderRequestID string
	CompletedAt       time.Time
}

type FailTranscriptionCommand struct {
	ReservationID string
	LeaseToken    string
	Attempt       SafeProcessingAttempt
}

type ReserveConfirmationCommand struct {
	CandidateID             string
	IdempotencyKey          string
	RetryTurnID             string
	AdvanceAuthorized       *bool
	AnswerAssessment        *practice.AnswerAssessment
	AssessmentPolicyVersion string
}

// VoiceRoundStore is owned by Practice Voice. Implementations must scope every
// lookup and write to actor.UserID, atomically enforce idempotency, and return
// not-found for foreign resources without revealing their existence.
//
// ReserveTranscription also owns the crash-recovery lease. For the same
// actor/session/key/fingerprint it must return Completed, return Processing
// while the current lease is live, or atomically replace an expired lease and
// return Reserved with a new opaque LeaseToken. Complete/Fail must reject a
// stale LeaseToken so a timed-out worker cannot overwrite the recovered result.
//
// Confirmation persistence supplies local checkpoints to the Practice Voice
// round saga. ReserveConfirmation must atomically bind actor +
// operation + IdempotencyKey to CandidateID, replay the same Turn for an
// identical request, reject a different CandidateID, and return that same
// immutable Turn if recovery uses a new key for an already confirmed
// Candidate. SaveTurnProgress and SaveTurnReview must be monotonic idempotent
// updates so concurrent retries cannot erase an already saved Practice
// decision or Review ID.
type VoiceRoundStore interface {
	GetVoiceQuestion(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (practice.Question, error)
	ReserveTranscription(
		context.Context,
		requestcontext.Actor,
		ReserveTranscriptionCommand,
	) (TranscriptionReservation, error)
	CompleteTranscription(
		context.Context,
		requestcontext.Actor,
		CompleteTranscriptionCommand,
	) (TranscriptionCandidate, error)
	FailTranscription(
		context.Context,
		requestcontext.Actor,
		FailTranscriptionCommand,
	) error
	GetTranscriptionCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (TranscriptionCandidate, error)
	ReserveConfirmation(
		context.Context,
		requestcontext.Actor,
		ReserveConfirmationCommand,
	) (practice.Turn, error)
}

// VoiceRecordingConfirmationStore is implemented by a production
// Practice Voice store that can create or replay a Turn and bind its durable
// recording in one transaction.
type VoiceRecordingConfirmation struct {
	Turn             practice.Turn
	RecordingDeleted bool
}

type VoiceRecordingConfirmationStore interface {
	ReserveRecordingConfirmation(
		context.Context,
		requestcontext.Actor,
		ConfirmVoiceTurnCommand,
		string,
	) (VoiceRecordingConfirmation, error)
}

type TemporaryAudioVault interface {
	Capture(
		context.Context,
		requestcontext.Actor,
		string,
		io.Reader,
	) (platformmedia.TemporaryAudioMetadata, error)
	Source(
		requestcontext.Actor,
		string,
	) (platformmedia.AudioSource, error)
	Delete(requestcontext.Actor, string) error
}

type VoiceRoundService struct {
	store           VoiceRoundStore
	vault           TemporaryAudioVault
	recognizer      SpeechRecognizer
	synthesizer     SpeechSynthesizer
	recordings      VoiceRecordingLifecycle
	answerEvaluator QuestionGenerator
	now             func() time.Time
}

func NewVoiceRoundService(
	store VoiceRoundStore,
	vault TemporaryAudioVault,
	recognizer SpeechRecognizer,
	synthesizer SpeechSynthesizer,
) (*VoiceRoundService, error) {
	return NewVoiceRoundServiceWithRecordings(
		store,
		vault,
		recognizer,
		synthesizer,
		nil,
	)
}

func NewVoiceRoundServiceWithRecordings(
	store VoiceRoundStore,
	vault TemporaryAudioVault,
	recognizer SpeechRecognizer,
	synthesizer SpeechSynthesizer,
	recordings VoiceRecordingLifecycle,
	answerEvaluators ...QuestionGenerator,
) (*VoiceRoundService, error) {
	if store == nil || vault == nil || recognizer == nil || synthesizer == nil {
		return nil, errors.New("practice voice: round dependencies are required")
	}
	if len(answerEvaluators) > 1 ||
		(len(answerEvaluators) == 1 && answerEvaluators[0] == nil) {
		return nil, errors.New("practice voice: invalid answer evaluator")
	}
	if recordings != nil {
		if _, ok := store.(VoiceRecordingConfirmationStore); !ok {
			return nil, errors.New(
				"practice voice: recording confirmation transaction is required",
			)
		}
	}
	service := &VoiceRoundService{
		store:       store,
		vault:       vault,
		recognizer:  recognizer,
		synthesizer: synthesizer,
		recordings:  recordings,
		now:         time.Now,
	}
	if len(answerEvaluators) == 1 {
		service.answerEvaluator = answerEvaluators[0]
	}
	return service, nil
}

type TranscribeVoiceCommand struct {
	SessionID      string
	QuestionID     string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

type SubmitTextAnswerCommand struct {
	SessionID      string
	QuestionID     string
	IdempotencyKey string
	AnswerText     string
}

func (service *VoiceRoundService) SubmitTextAnswer(
	ctx context.Context,
	actor requestcontext.Actor,
	respondentParticipantID string,
	command SubmitTextAnswerCommand,
) (candidate TranscriptionCandidate, returnErr error) {
	answer := strings.TrimSpace(command.AnswerText)
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(respondentParticipantID) == "" ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		answer == "" ||
		len(answer) > 8000 ||
		!utf8.ValidString(answer) {
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	question, err := service.store.GetVoiceQuestion(
		ctx,
		actor,
		command.SessionID,
		command.QuestionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !validVoiceQuestion(
		question,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}

	fingerprint := textInputFingerprint(
		answer,
		command.SessionID,
		command.QuestionID,
	)
	reservation, err := service.store.ReserveTranscription(
		ctx,
		actor,
		ReserveTranscriptionCommand{
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: respondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        fingerprint,
		},
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	switch reservation.Status {
	case TranscriptionCompleted:
		if !validTranscriptionCandidate(
			reservation.Candidate,
			command.SessionID,
			command.QuestionID,
			respondentParticipantID,
		) {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
		return reservation.Candidate, nil
	case TranscriptionProcessing:
		if strings.TrimSpace(reservation.ID) == "" ||
			reservation.LeaseToken != "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
		return TranscriptionCandidate{}, ErrVoiceRoundProcessing
	case TranscriptionReserved:
		if strings.TrimSpace(reservation.ID) == "" ||
			strings.TrimSpace(reservation.LeaseToken) == "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
	default:
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}

	requestID := "text_" + fingerprint[:32]
	persistenceContext, cancel := voicePersistenceContext(ctx)
	candidate, err = service.store.CompleteTranscription(
		persistenceContext,
		actor,
		CompleteTranscriptionCommand{
			ReservationID:     reservation.ID,
			LeaseToken:        reservation.LeaseToken,
			TranscriptID:      requestID,
			EvidenceVersion:   1,
			Transcript:        answer,
			Provider:          "speakup",
			Model:             "direct_text",
			ProviderRequestID: requestID,
			CompletedAt:       service.now().UTC(),
		},
	)
	cancel()
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !validTranscriptionCandidate(
		candidate,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}
	return candidate, nil
}

func (service *VoiceRoundService) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	respondentParticipantID string,
	command TranscribeVoiceCommand,
) (candidate TranscriptionCandidate, returnErr error) {
	return service.transcribe(ctx, actor, respondentParticipantID, command)
}

func (service *VoiceRoundService) transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	respondentParticipantID string,
	command TranscribeVoiceCommand,
) (candidate TranscriptionCandidate, returnErr error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(respondentParticipantID) == "" ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		command.Audio == nil {
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	question, err := service.store.GetVoiceQuestion(
		ctx,
		actor,
		command.SessionID,
		command.QuestionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !validVoiceQuestion(
		question,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}

	metadata, err := service.vault.Capture(
		ctx,
		actor,
		command.ContentType,
		command.Audio,
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return TranscriptionCandidate{}, contextErr
		}
		if errors.Is(err, platformmedia.ErrTemporaryAudioCapacity) {
			return TranscriptionCandidate{}, ErrVoiceRoundCapacity
		}
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	defer func() {
		if cleanupErr := service.vault.Delete(
			actor,
			metadata.ID,
		); cleanupErr != nil {
			candidate = TranscriptionCandidate{}
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()

	source, err := service.vault.Source(actor, metadata.ID)
	if err != nil {
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	fingerprint, err := voiceInputFingerprint(
		source,
		command.SessionID,
		command.QuestionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	reservation, err := service.store.ReserveTranscription(
		ctx,
		actor,
		ReserveTranscriptionCommand{
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: respondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        fingerprint,
		},
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	switch reservation.Status {
	case TranscriptionCompleted:
		if !validTranscriptionCandidate(
			reservation.Candidate,
			command.SessionID,
			command.QuestionID,
			respondentParticipantID,
		) {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
	case TranscriptionProcessing:
		if strings.TrimSpace(reservation.ID) == "" ||
			reservation.LeaseToken != "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
		return TranscriptionCandidate{}, ErrVoiceRoundProcessing
	case TranscriptionReserved:
		if strings.TrimSpace(reservation.ID) == "" ||
			strings.TrimSpace(reservation.LeaseToken) == "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
	default:
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}
	if service.recordings != nil {
		if _, err := service.stageRecording(
			ctx,
			actor,
			reservation.ID,
			source,
		); err != nil {
			return TranscriptionCandidate{}, err
		}
	}
	if reservation.Status == TranscriptionCompleted {
		return reservation.Candidate, nil
	}

	startedAt := service.now()
	request := TranscriptionRequest{Audio: source}
	result, err := service.recognizer.Transcribe(ctx, request)
	if err != nil {
		if saveErr := service.failTranscription(
			ctx,
			actor,
			reservation,
			err,
			startedAt,
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, err
	}
	return service.completeTranscription(
		ctx,
		actor,
		reservation,
		result,
		startedAt,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	)
}

func (service *VoiceRoundService) completeTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	reservation TranscriptionReservation,
	result TranscriptionResult,
	startedAt time.Time,
	sessionID string,
	questionID string,
	respondentParticipantID string,
) (TranscriptionCandidate, error) {
	transcript := strings.TrimSpace(result.Transcript)
	if !validTranscriptionResult(result, transcript) {
		attempt := SafeProcessingAttempt{
			Operation:  ProviderOperationTranscription,
			Kind:       ProviderErrorInvalidResponse,
			Retryable:  true,
			RequestID:  result.ID,
			Duration:   service.now().Sub(startedAt),
			OccurredAt: service.now().UTC(),
		}
		persistenceContext, cancel := voicePersistenceContext(ctx)
		saveErr := service.store.FailTranscription(
			persistenceContext,
			actor,
			FailTranscriptionCommand{
				ReservationID: reservation.ID,
				LeaseToken:    reservation.LeaseToken,
				Attempt:       attempt,
			},
		)
		cancel()
		if saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	persistenceContext, cancel := voicePersistenceContext(ctx)
	candidate, err := service.store.CompleteTranscription(
		persistenceContext,
		actor,
		CompleteTranscriptionCommand{
			ReservationID: reservation.ID,
			LeaseToken:    reservation.LeaseToken,
			TranscriptID:  result.ID,
			// Evidence versions describe the immutable voice-round snapshot,
			// not the provider model. The first completed candidate is v1.
			EvidenceVersion:   1,
			Transcript:        transcript,
			Provider:          result.Provider,
			Model:             result.Model,
			ProviderRequestID: result.ID,
			CompletedAt:       service.now().UTC(),
		},
	)
	cancel()
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !validTranscriptionCandidate(
		candidate,
		sessionID,
		questionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}
	return candidate, nil
}

func (service *VoiceRoundService) failTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	reservation TranscriptionReservation,
	err error,
	startedAt time.Time,
) error {
	attempt := safeAttempt(
		err,
		ProviderOperationTranscription,
		service.now().Sub(startedAt),
		service.now(),
	)
	persistenceContext, cancel := voicePersistenceContext(ctx)
	defer cancel()
	return service.store.FailTranscription(
		persistenceContext,
		actor,
		FailTranscriptionCommand{
			ReservationID: reservation.ID,
			LeaseToken:    reservation.LeaseToken,
			Attempt:       attempt,
		},
	)
}

type ConfirmVoiceTurnCommand struct {
	CandidateID             string
	IdempotencyKey          string
	RetryTurnID             string
	AdvanceAuthorized       *bool
	AnswerAssessment        *practice.AnswerAssessment
	AssessmentPolicyVersion string
}

func (service *VoiceRoundService) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(command.CandidateID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" {
		return practice.Turn{}, ErrVoiceRoundInvalid
	}
	candidate, err := service.store.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if command.RetryTurnID == "" {
		assessment, advanceAuthorized, assessmentVersion, assessmentErr :=
			service.assessInterviewAnswer(ctx, actor, candidate)
		if assessmentErr != nil {
			return practice.Turn{}, assessmentErr
		}
		command.AnswerAssessment = assessment
		command.AdvanceAuthorized = advanceAuthorized
		command.AssessmentPolicyVersion = assessmentVersion
	}
	if service.recordings != nil {
		result, err := service.store.(VoiceRecordingConfirmationStore).
			ReserveRecordingConfirmation(
				ctx,
				actor,
				command,
				candidate.ReservationID,
			)
		if err != nil {
			return practice.Turn{}, err
		}
		if !validRecordedVoiceTurn(
			candidate,
			result.Turn,
			result.RecordingDeleted,
		) {
			return practice.Turn{}, ErrVoiceRoundConflict
		}
		return result.Turn, nil
	}
	turn, err := service.store.ReserveConfirmation(
		ctx,
		actor,
		ReserveConfirmationCommand{
			CandidateID:             candidate.ID,
			IdempotencyKey:          command.IdempotencyKey,
			RetryTurnID:             command.RetryTurnID,
			AdvanceAuthorized:       command.AdvanceAuthorized,
			AnswerAssessment:        command.AnswerAssessment,
			AssessmentPolicyVersion: command.AssessmentPolicyVersion,
		},
	)
	if err != nil {
		return practice.Turn{}, err
	}
	return turn, nil
}

func (service *VoiceRoundService) ConfirmText(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
) (practice.Turn, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(command.CandidateID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" {
		return practice.Turn{}, ErrVoiceRoundInvalid
	}
	candidate, err := service.store.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, err
	}
	var assessment *practice.AnswerAssessment
	var advanceAuthorized *bool
	assessmentVersion := ""
	if command.RetryTurnID == "" {
		assessment, advanceAuthorized, assessmentVersion, err =
			service.assessInterviewAnswer(ctx, actor, candidate)
		if err != nil {
			return practice.Turn{}, err
		}
	}
	turn, err := service.store.ReserveConfirmation(
		ctx,
		actor,
		ReserveConfirmationCommand{
			CandidateID:             candidate.ID,
			IdempotencyKey:          command.IdempotencyKey,
			RetryTurnID:             command.RetryTurnID,
			AdvanceAuthorized:       advanceAuthorized,
			AnswerAssessment:        assessment,
			AssessmentPolicyVersion: assessmentVersion,
		},
	)
	if err != nil {
		return practice.Turn{}, err
	}
	if !validRecordedVoiceTurn(candidate, turn, true) {
		return practice.Turn{}, ErrVoiceRoundConflict
	}
	return turn, nil
}

// GetTranscriptionCandidate exposes an Actor-scoped voice-round resource to
// the Agent application layer so it can assemble downstream evidence without
// reaching into the persistence Store.
func (service *VoiceRoundService) GetTranscriptionCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (TranscriptionCandidate, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(candidateID) == "" {
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	return service.store.GetTranscriptionCandidate(ctx, actor, candidateID)
}

type QuestionSpeech struct {
	Text    string
	Audio   platformmedia.ManagedAudioSource
	Failure *SafeProcessingAttempt
}

func (service *VoiceRoundService) SynthesizeQuestion(
	ctx context.Context,
	text string,
) (QuestionSpeech, error) {
	text = strings.TrimSpace(text)
	if ctx == nil || text == "" {
		return QuestionSpeech{}, ErrVoiceRoundInvalid
	}
	startedAt := service.now()
	result, err := service.synthesizer.Synthesize(
		ctx,
		SynthesisRequest{Text: text},
	)
	if err != nil {
		attempt := safeAttempt(
			err,
			ProviderOperationSynthesis,
			service.now().Sub(startedAt),
			service.now(),
		)
		return QuestionSpeech{Text: text, Failure: &attempt}, nil
	}
	if !validSynthesisResult(result) {
		if result.Audio != nil {
			_ = result.Audio.Close()
		}
		attempt := SafeProcessingAttempt{
			Operation:  ProviderOperationSynthesis,
			Kind:       ProviderErrorInvalidResponse,
			Retryable:  true,
			RequestID:  result.RequestID,
			Duration:   service.now().Sub(startedAt),
			OccurredAt: service.now().UTC(),
		}
		return QuestionSpeech{Text: text, Failure: &attempt}, nil
	}
	return QuestionSpeech{Text: text, Audio: result.Audio}, nil
}

func voicePersistenceContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		voicePersistenceTimeout,
	)
}

func validVoiceQuestion(
	question practice.Question,
	sessionID string,
	questionID string,
	respondentParticipantID string,
) bool {
	return validVoiceIdentifier(question.ID) &&
		question.ID == questionID &&
		question.SessionID == sessionID &&
		strings.TrimSpace(question.Content) != "" &&
		validVoiceIdentifier(question.SpeakerParticipantID) &&
		len(question.AddresseeParticipantIDs) > 0 &&
		slices.Contains(
			question.AddresseeParticipantIDs,
			respondentParticipantID,
		)
}

func validTranscriptionResult(
	result TranscriptionResult,
	transcript string,
) bool {
	return validVoiceIdentifier(result.ID) &&
		validVoiceIdentifier(result.Provider) &&
		validVoiceIdentifier(result.Model) &&
		transcript != "" &&
		utf8.ValidString(transcript) &&
		result.Usage.InputTokens >= 0 &&
		result.Usage.OutputTokens >= 0 &&
		result.Usage.TotalTokens >= 0 &&
		result.Usage.AudioSeconds >= 0 &&
		result.Usage.Characters >= 0
}

func validTranscriptionCandidate(
	candidate TranscriptionCandidate,
	sessionID string,
	questionID string,
	respondentParticipantID string,
) bool {
	return validVoiceIdentifier(candidate.ID) &&
		candidate.SessionID == sessionID &&
		candidate.QuestionID == questionID &&
		validVoiceIdentifier(candidate.QuestionSpeakerID) &&
		len(candidate.AddresseeParticipantIDs) > 0 &&
		slices.Contains(
			candidate.AddresseeParticipantIDs,
			respondentParticipantID,
		) &&
		candidate.RespondentParticipantID == respondentParticipantID &&
		validVoiceIdentifier(candidate.TranscriptID) &&
		candidate.EvidenceVersion >= 1 &&
		strings.TrimSpace(candidate.Transcript) != "" &&
		utf8.ValidString(candidate.Transcript) &&
		validVoiceIdentifier(candidate.Provider) &&
		validVoiceIdentifier(candidate.Model) &&
		validVoiceIdentifier(candidate.ProviderRequestID) &&
		!candidate.CreatedAt.IsZero()
}

func validRecordedVoiceTurn(
	candidate TranscriptionCandidate,
	turn practice.Turn,
	recordingDeleted bool,
) bool {
	if !validVoiceIdentifier(turn.ID) ||
		turn.SessionID != candidate.SessionID ||
		turn.QuestionID != candidate.QuestionID ||
		turn.SpeakerParticipantID != candidate.QuestionSpeakerID ||
		!slices.Equal(
			turn.AddresseeParticipantIDs,
			candidate.AddresseeParticipantIDs,
		) ||
		turn.RespondentParticipantID != candidate.RespondentParticipantID ||
		turn.CandidateID != candidate.ID ||
		turn.TranscriptID != candidate.TranscriptID ||
		turn.EvidenceVersion != candidate.EvidenceVersion ||
		turn.AnswerText != candidate.Transcript {
		return false
	}
	if recordingDeleted {
		return turn.AudioAssetID == ""
	}
	return validVoiceIdentifier(turn.AudioAssetID)
}

func validSynthesisResult(result SynthesisResult) bool {
	return validVoiceIdentifier(result.RequestID) &&
		validVoiceIdentifier(result.Provider) &&
		validVoiceIdentifier(result.Model) &&
		validVoiceIdentifier(result.AudioID) &&
		result.Audio != nil &&
		platformmedia.ValidateAudioSource(result.Audio) == nil &&
		result.Usage.InputTokens >= 0 &&
		result.Usage.OutputTokens >= 0 &&
		result.Usage.TotalTokens >= 0 &&
		result.Usage.AudioSeconds >= 0 &&
		result.Usage.Characters >= 0
}

func validVoiceIdentifier(value string) bool {
	return utf8.ValidString(value) &&
		len(value) >= 1 &&
		len(value) <= 128 &&
		strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, func(r rune) bool {
			return r < 0x21 || r == 0x7f
		}) == -1
}

func validateVoiceContext(
	ctx context.Context,
	actor requestcontext.Actor,
) error {
	if ctx == nil || !actor.Valid() {
		return ErrVoiceRoundInvalid
	}
	return ctx.Err()
}

func voiceInputFingerprint(
	source platformmedia.AudioSource,
	sessionID string,
	questionID string,
) (string, error) {
	if err := platformmedia.ValidateAudioSource(source); err != nil {
		return "", ErrVoiceRoundInvalid
	}
	reader, err := source.Open()
	if err != nil {
		return "", ErrVoiceRoundInvalid
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, io.LimitReader(
		reader,
		platformmedia.MaxAudioBytes+1,
	))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return "", ErrVoiceRoundInvalid
	}
	_, _ = io.WriteString(hash, "\x00"+sessionID+"\x00"+questionID)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func textInputFingerprint(text string, sessionID string, questionID string) string {
	hash := sha256.New()
	_, _ = io.WriteString(
		hash,
		"conversation.text-answer/v1\x00"+sessionID+"\x00"+questionID+"\x00"+text,
	)
	return hex.EncodeToString(hash.Sum(nil))
}

func safeAttempt(
	err error,
	defaultOperation ProviderOperation,
	duration time.Duration,
	occurredAt time.Time,
) SafeProcessingAttempt {
	attempt := SafeProcessingAttempt{
		Operation:  defaultOperation,
		Kind:       ProviderErrorUnavailable,
		Retryable:  true,
		Duration:   duration,
		OccurredAt: occurredAt.UTC(),
	}
	var speechError *ProviderError
	if errors.As(err, &speechError) {
		attempt.Operation = speechError.Operation
		attempt.Kind = speechError.Kind
		attempt.Retryable = speechError.Retryable()
		attempt.RequestID = speechError.RequestID
	}
	return attempt
}
