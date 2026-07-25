package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrVoiceRoundInvalid    = errors.New("voice_round_invalid")
	ErrVoiceRoundNotFound   = errors.New("voice_round_not_found")
	ErrVoiceRoundConflict   = errors.New("voice_round_idempotency_conflict")
	ErrVoiceRoundProcessing = errors.New("voice_round_processing")
)

type VoiceQuestion struct {
	ID                      string
	SessionID               string
	Text                    string
	SpeakerParticipantID    string
	AddresseeParticipantIDs []string
}

type TranscriptionCandidate struct {
	ID                      string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	TranscriptID            string
	TranscriptVersion       string
	Transcript              string
	Provider                string
	Model                   string
	ProviderRequestID       string
	CreatedAt               time.Time
}

type ConfirmedVoiceTurn struct {
	ID                      string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	TranscriptID            string
	TranscriptVersion       string
	AnswerText              string
	EffectiveTurns          int
	SessionCompleted        bool
	ReviewID                string
}

type VoiceSessionReview struct {
	ID        string
	SessionID string
	TurnID    string
}

type VoiceReviewSource struct {
	TurnID                  string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	TranscriptID            string
	TranscriptVersion       string
	Transcript              string
	TranscriptionProvider   string
	TranscriptionModel      string
	TranscriptionRequestID  string
}

type SafeProcessingAttempt struct {
	Operation  ai.SpeechOperation
	Kind       ai.ErrorKind
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
	TranscriptVersion string
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
	CandidateID    string
	IdempotencyKey string
}

type VoiceTurnProgress struct {
	EffectiveTurns   int
	SessionCompleted bool
}

// VoiceRoundStore is owned by Conversation. Implementations must scope every
// lookup and write to actor.UserID, atomically enforce idempotency, and return
// not-found for foreign resources without revealing their existence.
//
// ReserveTranscription also owns the crash-recovery lease. For the same
// actor/session/key/fingerprint it must return Completed, return Processing
// while the current lease is live, or atomically replace an expired lease and
// return Reserved with a new opaque LeaseToken. Complete/Fail must reject a
// stale LeaseToken so a timed-out worker cannot overwrite the recovered result.
//
// Confirmation persistence is a recoverable local saga, not a cross-module
// transaction. ReserveConfirmation must atomically bind actor + operation +
// IdempotencyKey to CandidateID, replay the same Turn for an identical
// request, and reject a different CandidateID. SaveTurnProgress and
// SaveTurnReview must be monotonic idempotent updates so concurrent retries
// cannot erase an already saved Practice decision or Review ID.
type VoiceRoundStore interface {
	GetVoiceQuestion(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoiceQuestion, error)
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
	) (ConfirmedVoiceTurn, error)
	SaveTurnProgress(
		context.Context,
		requestcontext.Actor,
		string,
		VoiceTurnProgress,
	) (ConfirmedVoiceTurn, error)
	SaveTurnReview(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (ConfirmedVoiceTurn, error)
}

// VoicePracticePort is the Conversation-owned view of Practice. Practice
// remains authoritative for participant resolution and effective-turn count.
type VoicePracticePort interface {
	ResolveActorParticipant(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, error)
	ApplyEffectiveTurn(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoiceTurnProgress, error)
}

// VoiceReviewPort is the Conversation-owned view of Review. Review must use
// sessionID as the uniqueness scope so retries after the third Turn return the
// same formal Review instead of creating another one.
type VoiceReviewPort interface {
	EnsureSessionReview(
		context.Context,
		requestcontext.Actor,
		VoiceReviewSource,
	) (VoiceSessionReview, error)
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
	store       VoiceRoundStore
	practice    VoicePracticePort
	review      VoiceReviewPort
	vault       TemporaryAudioVault
	recognizer  ai.SpeechRecognizer
	synthesizer ai.SpeechSynthesizer
	now         func() time.Time
}

func NewVoiceRoundService(
	store VoiceRoundStore,
	practice VoicePracticePort,
	review VoiceReviewPort,
	vault TemporaryAudioVault,
	recognizer ai.SpeechRecognizer,
	synthesizer ai.SpeechSynthesizer,
) (*VoiceRoundService, error) {
	if store == nil || practice == nil || review == nil || vault == nil ||
		recognizer == nil || synthesizer == nil {
		return nil, errors.New("conversation voice round dependencies are required")
	}
	return &VoiceRoundService{
		store:       store,
		practice:    practice,
		review:      review,
		vault:       vault,
		recognizer:  recognizer,
		synthesizer: synthesizer,
		now:         time.Now,
	}, nil
}

type TranscribeVoiceCommand struct {
	SessionID      string
	QuestionID     string
	IdempotencyKey string
	ContentType    string
	Audio          io.Reader
}

func (service *VoiceRoundService) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command TranscribeVoiceCommand,
) (candidate TranscriptionCandidate, returnErr error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
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
	participantID, err := service.practice.ResolveActorParticipant(
		ctx,
		actor,
		command.SessionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !slices.Contains(
		question.AddresseeParticipantIDs,
		participantID,
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
		return TranscriptionCandidate{}, err
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
		return TranscriptionCandidate{}, err
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
			RespondentParticipantID: participantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        fingerprint,
		},
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	switch reservation.Status {
	case TranscriptionCompleted:
		return reservation.Candidate, nil
	case TranscriptionProcessing:
		return TranscriptionCandidate{}, ErrVoiceRoundProcessing
	case TranscriptionReserved:
	default:
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}

	startedAt := service.now()
	result, err := service.recognizer.Transcribe(
		ctx,
		ai.TranscriptionRequest{Audio: source},
	)
	if err != nil {
		attempt := safeAttempt(
			err,
			ai.SpeechOperationTranscription,
			service.now().Sub(startedAt),
			service.now(),
		)
		if saveErr := service.store.FailTranscription(
			ctx,
			actor,
			FailTranscriptionCommand{
				ReservationID: reservation.ID,
				LeaseToken:    reservation.LeaseToken,
				Attempt:       attempt,
			},
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, err
	}
	transcript := strings.TrimSpace(result.Transcript)
	if transcript == "" {
		attempt := SafeProcessingAttempt{
			Operation:  ai.SpeechOperationTranscription,
			Kind:       ai.ErrorInvalidResponse,
			Retryable:  true,
			RequestID:  result.ID,
			Duration:   service.now().Sub(startedAt),
			OccurredAt: service.now().UTC(),
		}
		if saveErr := service.store.FailTranscription(
			ctx,
			actor,
			FailTranscriptionCommand{
				ReservationID: reservation.ID,
				LeaseToken:    reservation.LeaseToken,
				Attempt:       attempt,
			},
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	return service.store.CompleteTranscription(
		ctx,
		actor,
		CompleteTranscriptionCommand{
			ReservationID:     reservation.ID,
			LeaseToken:        reservation.LeaseToken,
			TranscriptID:      result.ID,
			TranscriptVersion: result.Model,
			Transcript:        transcript,
			Provider:          result.Provider,
			Model:             result.Model,
			ProviderRequestID: result.ID,
			CompletedAt:       service.now().UTC(),
		},
	)
}

type ConfirmVoiceTurnCommand struct {
	CandidateID    string
	IdempotencyKey string
}

func (service *VoiceRoundService) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
) (ConfirmedVoiceTurn, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(command.CandidateID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundInvalid
	}
	candidate, err := service.store.GetTranscriptionCandidate(
		ctx,
		actor,
		command.CandidateID,
	)
	if err != nil {
		return ConfirmedVoiceTurn{}, err
	}
	turn, err := service.store.ReserveConfirmation(
		ctx,
		actor,
		ReserveConfirmationCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: command.IdempotencyKey,
		},
	)
	if err != nil {
		return ConfirmedVoiceTurn{}, err
	}
	if turn.EffectiveTurns == 0 {
		progress, err := service.practice.ApplyEffectiveTurn(
			ctx,
			actor,
			turn.SessionID,
			turn.ID,
		)
		if err != nil {
			return ConfirmedVoiceTurn{}, err
		}
		turn, err = service.store.SaveTurnProgress(
			ctx,
			actor,
			turn.ID,
			progress,
		)
		if err != nil {
			return ConfirmedVoiceTurn{}, err
		}
	}
	if !turn.SessionCompleted || turn.ReviewID != "" {
		return turn, nil
	}
	sessionReview, err := service.review.EnsureSessionReview(
		ctx,
		actor,
		VoiceReviewSource{
			TurnID:            turn.ID,
			SessionID:         turn.SessionID,
			QuestionID:        turn.QuestionID,
			QuestionSpeakerID: turn.QuestionSpeakerID,
			AddresseeParticipantIDs: slices.Clone(
				turn.AddresseeParticipantIDs,
			),
			RespondentParticipantID: turn.RespondentParticipantID,
			TranscriptID:            turn.TranscriptID,
			TranscriptVersion:       turn.TranscriptVersion,
			Transcript:              turn.AnswerText,
			TranscriptionProvider:   candidate.Provider,
			TranscriptionModel:      candidate.Model,
			TranscriptionRequestID:  candidate.ProviderRequestID,
		},
	)
	if err != nil {
		return ConfirmedVoiceTurn{}, err
	}
	return service.store.SaveTurnReview(
		ctx,
		actor,
		turn.ID,
		sessionReview.ID,
	)
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
		ai.SynthesisRequest{Text: text},
	)
	if err != nil {
		attempt := safeAttempt(
			err,
			ai.SpeechOperationSynthesis,
			service.now().Sub(startedAt),
			service.now(),
		)
		return QuestionSpeech{Text: text, Failure: &attempt}, nil
	}
	return QuestionSpeech{Text: text, Audio: result.Audio}, nil
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

func safeAttempt(
	err error,
	defaultOperation ai.SpeechOperation,
	duration time.Duration,
	occurredAt time.Time,
) SafeProcessingAttempt {
	attempt := SafeProcessingAttempt{
		Operation:  defaultOperation,
		Kind:       ai.ErrorProviderUnavailable,
		Retryable:  true,
		Duration:   duration,
		OccurredAt: occurredAt.UTC(),
	}
	var speechError *ai.SpeechError
	if errors.As(err, &speechError) {
		attempt.Operation = speechError.Operation
		attempt.Kind = speechError.Kind
		attempt.Retryable = speechError.Retryable()
		attempt.RequestID = speechError.RequestID
	}
	return attempt
}
