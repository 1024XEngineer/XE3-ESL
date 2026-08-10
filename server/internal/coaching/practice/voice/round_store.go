package voice

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

type interviewSessionContextRepository interface {
	GetSession(
		context.Context,
		practice.Actor,
		string,
	) (practice.Session, error)
	GetSessionSnapshot(
		context.Context,
		practice.Actor,
		string,
	) (practice.SessionSnapshot, error)
}

func (store *roundStoreAdapter) GetInterviewAnswerContext(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate TranscriptionCandidate,
) (InterviewAnswerContext, error) {
	repository, ok := store.repository.(interviewSessionContextRepository)
	if !ok {
		return InterviewAnswerContext{}, nil
	}
	practiceActor := practiceActor(actor)
	session, err := repository.GetSession(ctx, practiceActor, candidate.SessionID)
	if err != nil {
		return InterviewAnswerContext{}, mapPersistenceError(err)
	}
	snapshot, err := repository.GetSessionSnapshot(ctx, practiceActor, candidate.SessionID)
	if err != nil {
		return InterviewAnswerContext{}, mapPersistenceError(err)
	}
	option, err := snapshot.SceneSelection.PracticeOption()
	if err != nil {
		return InterviewAnswerContext{}, ErrInvalidContext
	}
	policy, err := practice.ResolveTurnPolicy(option.TurnPolicyRef)
	if err != nil {
		return InterviewAnswerContext{}, ErrInvalidContext
	}
	if policy.Kind != practice.TurnPolicyInterview {
		return InterviewAnswerContext{}, nil
	}
	question, err := store.GetVoiceQuestion(
		ctx,
		actor,
		candidate.SessionID,
		candidate.QuestionID,
	)
	if err != nil {
		return InterviewAnswerContext{}, err
	}
	blueprints := snapshot.SceneSelection.Scene.Prompt.TurnBlueprints
	if len(blueprints) == 0 {
		return InterviewAnswerContext{}, ErrInvalidContext
	}
	blueprintIndex := session.EffectiveTurns
	if blueprintIndex >= len(blueprints) {
		blueprintIndex = len(blueprints) - 1
	}
	return InterviewAnswerContext{
		Applicable:       true,
		Question:         question,
		Scene:            snapshot.SceneSelection.Scene.Prompt.PublicSceneBrief,
		PracticeGoal:     snapshot.SceneSelection.Scene.Prompt.PracticeGoal,
		FocusAreas:       slices.Clone(snapshot.SceneSelection.Scene.Prompt.FocusAreas),
		CurrentBlueprint: blueprints[blueprintIndex],
	}, nil
}

func (store *roundStoreAdapter) GetVoiceQuestion(
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
	return mapVoiceQuestion(question), nil
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
		ID: reservation.ID,
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
	default:
		return TranscriptionReservation{},
			ErrVoiceRoundConflict
	}
	return result, nil
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
			CandidateID:             candidate.ID,
			EvidenceVersion:         candidate.EvidenceVersion,
			ConfirmedText:           candidate.Text,
			IdempotencyKey:          command.IdempotencyKey,
			RetryTurnID:             command.RetryTurnID,
			AdvanceAuthorized:       command.AdvanceAuthorized,
			AnswerAssessment:        command.AnswerAssessment,
			AssessmentPolicyVersion: command.AssessmentPolicyVersion,
			ReplayOnly:              command.ReplayOnly,
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
) (VoiceRecordingConfirmation, error) {
	if store.recordings == nil {
		return VoiceRecordingConfirmation{},
			ErrVoiceRoundInvalid
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		persistenceActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return VoiceRecordingConfirmation{},
			mapPersistenceError(err)
	}
	persisted, err :=
		store.recordings.ConfirmTurnWithRecording(
			ctx,
			persistenceActor(actor),
			ConfirmTurnCommand{
				CandidateID:             candidate.ID,
				EvidenceVersion:         candidate.EvidenceVersion,
				ConfirmedText:           candidate.Text,
				IdempotencyKey:          command.IdempotencyKey,
				RetryTurnID:             command.RetryTurnID,
				AdvanceAuthorized:       command.AdvanceAuthorized,
				AnswerAssessment:        command.AnswerAssessment,
				AssessmentPolicyVersion: command.AssessmentPolicyVersion,
				ReplayOnly:              command.ReplayOnly,
			},
			uploadRequestID,
		)
	if err != nil {
		return VoiceRecordingConfirmation{},
			mapRecordingConfirmationError(err)
	}
	mapped, err := mapVoiceTurnWithCandidate(persisted.Turn, candidate)
	if err != nil {
		return VoiceRecordingConfirmation{}, err
	}
	mapped.AudioAssetID = persisted.AudioAssetID
	return VoiceRecordingConfirmation{
		Turn:             mapped,
		RecordingDeleted: persisted.RecordingDeleted,
	}, nil
}

func mapRecordingConfirmationError(err error) error {
	switch {
	case errors.Is(err, ErrAudioAssetNotFound),
		errors.Is(err, ErrAudioAssetForbidden),
		errors.Is(err, ErrAudioAssetAlreadyBound),
		errors.Is(err, ErrAudioAssetInvalidTransition),
		errors.Is(err, ErrAudioAssetUploadTerminated):
		// A recording that cleanup removed, another Turn already bound, or
		// otherwise left the confirmable state is a terminal request conflict.
		return ErrConflict
	case errors.Is(err, ErrAudioAssetConcurrentUpdate):
		// A lost optimistic update can be retried safely with the same
		// idempotency key. The HTTP boundary exposes resource_processing,
		// Retry-After: 1, and retryable=true for this sentinel.
		return ErrVoiceRoundProcessing
	default:
		return mapPersistenceError(err)
	}
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

func mapVoiceQuestion(
	question practice.Question,
) practice.Question {
	return practice.Question{
		ID:                      question.ID,
		SessionID:               question.SessionID,
		Type:                    question.Type,
		DialogueAct:             question.DialogueAct,
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

var _ VoiceRoundStore = (*roundStoreAdapter)(nil)
