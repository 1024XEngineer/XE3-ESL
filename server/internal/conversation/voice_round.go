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
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
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

type VoiceQuestion struct {
	ID                      string
	SessionID               string
	Text                    string
	SpeakerParticipantID    string
	AddresseeParticipantIDs []string
}

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

type ConfirmedVoiceTurn struct {
	ID                      string
	AudioAssetID            string
	SessionID               string
	QuestionID              string
	QuestionSpeakerID       string
	AddresseeParticipantIDs []string
	RespondentParticipantID string
	CandidateID             string
	TranscriptID            string
	EvidenceVersion         int64
	AnswerText              string
	EffectiveTurns          int
	SessionCompleted        bool
	ReviewID                string
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
// Confirmation persistence supplies local checkpoints to the Agent-owned
// cross-module saga. ReserveConfirmation must atomically bind actor +
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

// VoiceRecordingConfirmationStore is implemented by a production
// Conversation store that can create or replay a Turn and bind its durable
// recording in one transaction.
type VoiceRecordingConfirmation struct {
	Turn             ConfirmedVoiceTurn
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
	store       VoiceRoundStore
	vault       TemporaryAudioVault
	recognizer  ai.SpeechRecognizer
	synthesizer ai.SpeechSynthesizer
	recordings  VoiceRecordingLifecycle
	now         func() time.Time
}

func NewVoiceRoundService(
	store VoiceRoundStore,
	vault TemporaryAudioVault,
	recognizer ai.SpeechRecognizer,
	synthesizer ai.SpeechSynthesizer,
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
	recognizer ai.SpeechRecognizer,
	synthesizer ai.SpeechSynthesizer,
	recordings VoiceRecordingLifecycle,
) (*VoiceRoundService, error) {
	if store == nil || vault == nil || recognizer == nil || synthesizer == nil {
		return nil, errors.New("conversation voice round dependencies are required")
	}
	if recordings != nil {
		if _, ok := store.(VoiceRecordingConfirmationStore); !ok {
			return nil, errors.New(
				"conversation recording confirmation transaction is required",
			)
		}
	}
	return &VoiceRoundService{
		store:       store,
		vault:       vault,
		recognizer:  recognizer,
		synthesizer: synthesizer,
		recordings:  recordings,
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
		return TranscriptionCandidate{}, err
	}
	transcript := strings.TrimSpace(result.Transcript)
	if !validTranscriptionResult(result, transcript) {
		attempt := SafeProcessingAttempt{
			Operation:  ai.SpeechOperationTranscription,
			Kind:       ai.ErrorInvalidResponse,
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
	candidate, err = service.store.CompleteTranscription(
		persistenceContext,
		actor,
		CompleteTranscriptionCommand{
			ReservationID: reservation.ID,
			LeaseToken:    reservation.LeaseToken,
			TranscriptID:  result.ID,
			// Evidence versions describe the immutable Conversation snapshot,
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
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}
	return candidate, nil
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
	if service.recordings != nil {
		result, err := service.store.(VoiceRecordingConfirmationStore).
			ReserveRecordingConfirmation(
				ctx,
				actor,
				command,
				candidate.ReservationID,
			)
		if err != nil {
			return ConfirmedVoiceTurn{}, err
		}
		if !validRecordedVoiceTurn(
			candidate,
			result.Turn,
			result.RecordingDeleted,
		) {
			return ConfirmedVoiceTurn{}, ErrVoiceRoundConflict
		}
		return result.Turn, nil
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
	return turn, nil
}

// GetTranscriptionCandidate exposes an Actor-scoped Conversation resource to
// the Agent application layer so it can assemble downstream evidence without
// reaching into the Conversation Store.
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

// SaveTurnProgress records only Conversation's local saga checkpoint. The
// Agent application layer owns the cross-module ordering and recovery.
func (service *VoiceRoundService) SaveTurnProgress(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
	progress VoiceTurnProgress,
) (ConfirmedVoiceTurn, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(turnID) == "" ||
		progress.EffectiveTurns < 1 {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundInvalid
	}
	turn, err := service.store.SaveTurnProgress(
		ctx,
		actor,
		turnID,
		progress,
	)
	if err != nil || service.recordings == nil {
		return turn, err
	}
	candidate, err := service.store.GetTranscriptionCandidate(
		ctx,
		actor,
		turn.CandidateID,
	)
	if err != nil {
		return ConfirmedVoiceTurn{}, err
	}
	return service.withReadableRecording(ctx, actor, candidate, turn)
}

// SaveTurnReview records only Conversation's reference to a Review resource.
// Review creation and exactly-once semantics remain outside Conversation.
func (service *VoiceRoundService) SaveTurnReview(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
	reviewID string,
) (ConfirmedVoiceTurn, error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(turnID) == "" ||
		strings.TrimSpace(reviewID) == "" {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundInvalid
	}
	turn, err := service.store.SaveTurnReview(
		ctx,
		actor,
		turnID,
		reviewID,
	)
	if err != nil || service.recordings == nil {
		return turn, err
	}
	candidate, err := service.store.GetTranscriptionCandidate(
		ctx,
		actor,
		turn.CandidateID,
	)
	if err != nil {
		return ConfirmedVoiceTurn{}, err
	}
	return service.withReadableRecording(ctx, actor, candidate, turn)
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
	if !validSynthesisResult(result) {
		if result.Audio != nil {
			_ = result.Audio.Close()
		}
		attempt := SafeProcessingAttempt{
			Operation:  ai.SpeechOperationSynthesis,
			Kind:       ai.ErrorInvalidResponse,
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
	question VoiceQuestion,
	sessionID string,
	questionID string,
	respondentParticipantID string,
) bool {
	return validVoiceIdentifier(question.ID) &&
		question.ID == questionID &&
		question.SessionID == sessionID &&
		strings.TrimSpace(question.Text) != "" &&
		validVoiceIdentifier(question.SpeakerParticipantID) &&
		len(question.AddresseeParticipantIDs) > 0 &&
		slices.Contains(
			question.AddresseeParticipantIDs,
			respondentParticipantID,
		)
}

func validTranscriptionResult(
	result ai.TranscriptionResult,
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
	turn ConfirmedVoiceTurn,
	recordingDeleted bool,
) bool {
	if !validVoiceIdentifier(turn.ID) ||
		turn.SessionID != candidate.SessionID ||
		turn.QuestionID != candidate.QuestionID ||
		turn.QuestionSpeakerID != candidate.QuestionSpeakerID ||
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

func validSynthesisResult(result ai.SynthesisResult) bool {
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
