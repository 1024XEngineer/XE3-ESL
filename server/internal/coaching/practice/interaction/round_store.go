package interaction

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type roundStoreAdapter struct {
	repository PersistenceStore
	recordings RecordingConfirmationStore
	asrLease   time.Duration
}

func (store *roundStoreAdapter) GetQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	questionID string,
) (practice.Question, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		persistenceActor(actor),
		questionID,
	)
	if err != nil || question.SessionID != sessionID {
		if err == nil {
			return practice.Question{},
				ErrVoiceRoundNotFound
		}
		return practice.Question{}, mapPersistenceError(err)
	}
	return mapQuestion(question), nil
}

func (store *roundStoreAdapter) ReserveTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command ReserveTranscriptionCommand,
) (TranscriptionReservation, error) {
	reservation, err := store.repository.ReserveTranscription(
		ctx,
		persistenceActor(actor),
		StoreReserveTranscriptionCommand{
			TurnID:                  command.TurnID,
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: command.RespondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        command.InputFingerprint,
			LeaseDuration:           store.asrLease,
		},
	)
	if err != nil {
		return TranscriptionReservation{}, mapPersistenceError(err)
	}
	result := TranscriptionReservation{
		ID:                      reservation.ID,
		SessionID:               reservation.SessionID,
		QuestionID:              reservation.QuestionID,
		RespondentParticipantID: reservation.RespondentParticipantID,
		IdempotencyKey:          reservation.IdempotencyKey,
		InputFingerprint:        reservation.InputFingerprint,
		AudioAssetID:            reservation.AudioAssetID,
	}
	switch reservation.Status {
	case StoredTranscriptionCompleted:
		result.Status = TranscriptionCompleted
		candidate, candidateErr := store.GetTranscriptionCandidate(
			ctx,
			actor,
			reservation.CandidateID,
		)
		if candidateErr != nil {
			return TranscriptionReservation{}, candidateErr
		}
		result.Candidate = candidate
	case StoredTranscriptionConfirmed:
		result.Status = TranscriptionConfirmed
		candidate, candidateErr := store.GetTranscriptionCandidate(
			ctx, actor, reservation.CandidateID,
		)
		if candidateErr != nil {
			return TranscriptionReservation{}, candidateErr
		}
		result.Candidate = candidate
	case StoredTranscriptionProcessing:
		if reservation.LeaseAcquired {
			result.Status = TranscriptionReserved
			result.LeaseToken = transcriptionLeaseToken(
				actor.UserID,
				reservation,
			)
		} else {
			result.Status = TranscriptionProcessing
		}
	case StoredTranscriptionFailed:
		result.Status = TranscriptionFailed
	default:
		return TranscriptionReservation{},
			ErrVoiceRoundConflict
	}
	return result, nil
}

func (store *roundStoreAdapter) GetTranscriptionReservation(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
) (TranscriptionReservation, error) {
	reservation, err := store.repository.GetReservation(
		ctx, persistenceActor(actor), reservationID,
	)
	if err != nil {
		return TranscriptionReservation{}, mapPersistenceError(err)
	}
	result := TranscriptionReservation{
		ID:                      reservation.ID,
		SessionID:               reservation.SessionID,
		QuestionID:              reservation.QuestionID,
		RespondentParticipantID: reservation.RespondentParticipantID,
		IdempotencyKey:          reservation.IdempotencyKey,
		InputFingerprint:        reservation.InputFingerprint,
		AudioAssetID:            reservation.AudioAssetID,
	}
	switch reservation.Status {
	case StoredTranscriptionProcessing:
		result.Status = TranscriptionProcessing
	case StoredTranscriptionCompleted:
		result.Status = TranscriptionCompleted
		result.Candidate, err = store.GetTranscriptionCandidate(
			ctx, actor, reservation.CandidateID,
		)
	case StoredTranscriptionConfirmed:
		result.Status = TranscriptionConfirmed
		result.Candidate, err = store.GetTranscriptionCandidate(
			ctx, actor, reservation.CandidateID,
		)
	case StoredTranscriptionFailed:
		result.Status = TranscriptionFailed
	default:
		return TranscriptionReservation{}, ErrVoiceRoundConflict
	}
	return result, err
}

func (store *roundStoreAdapter) AttachTranscriptionRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	assetID string,
) error {
	return mapPersistenceError(store.repository.AttachTranscriptionRecording(
		ctx, persistenceActor(actor), reservationID, assetID,
	))
}

func (store *roundStoreAdapter) CompleteTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command CompleteTranscriptionCommand,
) (TranscriptionCandidate, error) {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	candidate, err := store.repository.CompleteTranscription(
		ctx,
		job,
		StoreCompleteTranscriptionCommand{
			TranscriptID:      command.TranscriptID,
			EvidenceVersion:   command.EvidenceVersion,
			Provider:          command.Provider,
			Model:             command.Model,
			ProviderRequestID: command.ProviderRequestID,
			Text:              command.Transcript,
		},
	)
	if err != nil {
		return TranscriptionCandidate{}, mapPersistenceError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *roundStoreAdapter) FailTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command FailTranscriptionCommand,
) error {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return err
	}
	return mapPersistenceError(store.repository.FailTranscription(
		ctx,
		job,
		ProcessingFailure{
			Code:              string(command.Attempt.Kind),
			Retryable:         command.Attempt.Retryable,
			ProviderRequestID: command.Attempt.RequestID,
			Duration:          command.Attempt.Duration,
		},
	))
}

func (store *roundStoreAdapter) transcriptionJob(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	leaseToken string,
) (JobContext, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return JobContext{},
			ErrVoiceRoundConflict
	}
	reservation, err := store.repository.GetReservation(
		ctx,
		persistenceActor(actor),
		reservationID,
	)
	if err != nil {
		return JobContext{}, mapPersistenceError(err)
	}
	expectedToken := transcriptionLeaseToken(actor.UserID, reservation)
	if subtle.ConstantTimeCompare(
		[]byte(leaseToken),
		[]byte(expectedToken),
	) != 1 {
		return JobContext{},
			ErrVoiceRoundConflict
	}
	return JobContext{
		OwnerUserID:        actor.UserID,
		DeletionGeneration: reservation.DeletionGeneration,
		ReservationID:      reservation.ID,
		FencingToken:       reservation.FencingToken,
	}, nil
}

func transcriptionLeaseToken(
	ownerUserID string,
	reservation StoredTranscriptionReservation,
) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(
		digest,
		"practiceinput.asr-lease/v1\x00%s\x00%s\x00%d\x00%d",
		ownerUserID,
		reservation.ID,
		reservation.DeletionGeneration,
		reservation.FencingToken,
	)
	return hex.EncodeToString(digest.Sum(nil))
}

func (store *roundStoreAdapter) GetTranscriptionCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (TranscriptionCandidate, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		persistenceActor(actor),
		candidateID,
	)
	if err != nil {
		return TranscriptionCandidate{}, mapPersistenceError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *roundStoreAdapter) mapCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate StoredTranscriptCandidate,
) (TranscriptionCandidate, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		persistenceActor(actor),
		candidate.QuestionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, mapPersistenceError(err)
	}
	return TranscriptionCandidate{
		ID:                      candidate.ID,
		ReservationID:           candidate.ReservationID,
		SessionID:               candidate.SessionID,
		QuestionID:              candidate.QuestionID,
		QuestionSpeakerID:       question.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
		RespondentParticipantID: candidate.RespondentParticipantID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		Transcript:              candidate.Text,
		Provider:                candidate.Provider,
		Model:                   candidate.Model,
		ProviderRequestID:       candidate.ProviderRequestID,
		CreatedAt:               candidate.CreatedAt,
	}, nil
}

func (store *roundStoreAdapter) ReserveConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command ReserveConfirmationCommand,
) (practice.Turn, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		persistenceActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, mapPersistenceError(err)
	}
	turn, err := store.repository.ConfirmTurn(
		ctx,
		persistenceActor(actor),
		ConfirmTurnCommand{
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ConfirmedText:   candidate.Text,
			IdempotencyKey:  command.IdempotencyKey,
			RetryTurnID:     command.RetryTurnID,
		},
	)
	if err != nil {
		return practice.Turn{}, mapPersistenceError(err)
	}
	return mapVoiceTurnWithCandidate(turn, candidate)
}

func (store *roundStoreAdapter) ReserveRecordingConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
	uploadRequestID string,
) (practice.Turn, error) {
	if store.recordings == nil {
		return practice.Turn{}, ErrVoiceRoundInvalid
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		persistenceActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, mapPersistenceError(err)
	}
	persisted, err :=
		store.recordings.ConfirmTurnWithRecording(
			ctx,
			persistenceActor(actor),
			ConfirmTurnCommand{
				CandidateID:     candidate.ID,
				EvidenceVersion: candidate.EvidenceVersion,
				ConfirmedText:   candidate.Text,
				IdempotencyKey:  command.IdempotencyKey,
				RetryTurnID:     command.RetryTurnID,
			},
			uploadRequestID,
		)
	if err != nil {
		return practice.Turn{}, mapPersistenceError(err)
	}
	mapped, err := mapVoiceTurnWithCandidate(persisted, candidate)
	if err != nil {
		return practice.Turn{}, err
	}
	return mapped, nil
}

func persistenceActor(
	actor requestcontext.Actor,
) Actor {
	return Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapPersistenceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPersistenceInvalid):
		return ErrVoiceRoundInvalid
	case errors.Is(err, ErrPersistenceNotFound):
		return ErrVoiceRoundNotFound
	case errors.Is(err, ErrPersistenceConflict):
		return ErrVoiceRoundConflict
	case errors.Is(err, ErrActorDeleted):
		return ErrNotFound
	default:
		return err
	}
}

func mapQuestion(
	question practice.Question,
) practice.Question {
	return practice.Question{
		ID:                      question.ID,
		SessionID:               question.SessionID,
		Type:                    question.Type,
		ParentQuestionID:        question.ParentQuestionID,
		Content:                 question.Content,
		SpeakerParticipantID:    question.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
	}
}

func mapVoiceTurn(
	turn practice.Turn,
) practice.Turn {
	turn.AddresseeParticipantIDs = slices.Clone(
		turn.AddresseeParticipantIDs,
	)
	return turn
}

func mapVoiceTurnWithCandidate(
	turn practice.Turn,
	candidate StoredTranscriptCandidate,
) (practice.Turn, error) {
	if candidate.ID == "" ||
		candidate.ID != turn.CandidateID ||
		candidate.SessionID != turn.SessionID ||
		candidate.QuestionID != turn.QuestionID ||
		candidate.RespondentParticipantID != turn.RespondentParticipantID ||
		candidate.EvidenceVersion != turn.EvidenceVersion ||
		candidate.Text != turn.AnswerText ||
		candidate.TranscriptID == "" {
		return practice.Turn{},
			ErrVoiceRoundConflict
	}
	mapped := mapVoiceTurn(turn)
	mapped.TranscriptID = candidate.TranscriptID
	return mapped, nil
}

var _ RoundStore = (*roundStoreAdapter)(nil)
