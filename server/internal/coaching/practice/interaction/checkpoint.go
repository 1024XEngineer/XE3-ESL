package interaction

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type checkpointAdapter struct {
	repository PersistenceStore
	feedback   TurnFeedbackStatusReader
}

func (adapter *checkpointAdapter) ListTurnHistory(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) ([]TurnExchange, error) {
	turns, err := adapter.repository.ListSessionTurns(
		ctx,
		persistenceActor(actor),
		sessionID,
	)
	if err != nil {
		return nil, mapPersistenceError(err)
	}
	history := make([]TurnExchange, 0, len(turns))
	for _, persistedTurn := range turns {
		candidate, candidateErr := adapter.repository.GetCandidate(
			ctx,
			persistenceActor(actor),
			persistedTurn.CandidateID,
		)
		if candidateErr != nil {
			return nil, mapPersistenceError(candidateErr)
		}
		turn, turnErr := mapVoiceTurnWithCandidate(
			persistedTurn,
			candidate,
		)
		if turnErr != nil {
			return nil, turnErr
		}
		turn, turnErr = adapter.withSpeechFeedback(
			ctx,
			actor,
			turn,
		)
		if turnErr != nil {
			return nil, turnErr
		}
		question, questionErr := adapter.repository.GetQuestion(
			ctx,
			persistenceActor(actor),
			turn.QuestionID,
		)
		if questionErr != nil {
			return nil, mapPersistenceError(questionErr)
		}
		history = append(history, TurnExchange{
			Question: mapQuestion(question),
			Turn:     turn,
		})
	}
	return history, nil
}

func (adapter *checkpointAdapter) LatestTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (practice.Turn, bool, error) {
	turns, err := adapter.repository.ListSessionTurns(
		ctx,
		persistenceActor(actor),
		sessionID,
	)
	if err != nil {
		return practice.Turn{}, false,
			mapPersistenceError(err)
	}
	if len(turns) == 0 {
		return practice.Turn{}, false, nil
	}
	persistedTurn := turns[len(turns)-1]
	candidate, err := adapter.repository.GetCandidate(
		ctx,
		persistenceActor(actor),
		persistedTurn.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, false,
			mapPersistenceError(err)
	}
	turn, err := mapVoiceTurnWithCandidate(persistedTurn, candidate)
	if err != nil {
		return practice.Turn{}, false, err
	}
	turn, err = adapter.withSpeechFeedback(ctx, actor, turn)
	if err != nil {
		return practice.Turn{}, false, err
	}
	return turn, true, nil
}

func (adapter *checkpointAdapter) withSpeechFeedback(
	ctx context.Context,
	actor requestcontext.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if adapter.feedback == nil {
		return turn, nil
	}
	statusURL, found, err :=
		adapter.feedback.StatusURLForTurn(
			ctx,
			actor,
			turn.ID,
		)
	if err != nil {
		return practice.Turn{}, err
	}
	if found {
		turn.SpeechFeedbackStatusURL = statusURL
	}
	return turn, nil
}

var _ CheckpointPort = (*checkpointAdapter)(nil)
