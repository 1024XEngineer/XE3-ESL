package smoke

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
)

// Application is the smoke composition layer. It coordinates formal module
// services while leaving every resource mutation with its owning module.
type Application struct {
	preparation *preparationBackend
	practice    *practiceBackend
	voice       *voiceService
	review      *reviewService
	failures    FailureControl
}

func NewApplication(
	preparationStore *preparationBackend,
	practiceStore *practiceBackend,
	voiceService *voiceService,
	reviewService *reviewService,
	failures FailureControl,
) *Application {
	return &Application{
		preparation: preparationStore,
		practice:    practiceStore,
		voice:       voiceService,
		review:      reviewService,
		failures:    failures,
	}
}

func (a *Application) CreatePlan(
	ctx context.Context,
	request preparation.CreatePlanRequest,
) (preparation.PracticePlan, error) {
	return a.preparation.CreatePracticePlan(ctx, request)
}

func (a *Application) CreateSession(
	planID string,
	expectedPlanRevision int,
) (practice.SessionBootstrap, error) {
	if !a.preparation.PracticePlanExists(
		planID,
		expectedPlanRevision,
	) {
		return practice.SessionBootstrap{}, ErrPlanNotFound
	}
	return a.practice.CreatePracticeSession()
}

func (a *Application) Bootstrap(sessionID string) (map[string]any, error) {
	session, ok := a.practice.GetPracticeSession(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}
	snapshot, ok := a.practice.GetPracticeSessionSnapshot(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}
	result, err := a.voice.Bootstrap(sessionID)
	if err != nil {
		return nil, err
	}
	result["practice_session"] = session
	result["snapshot"] = snapshot
	return result, nil
}

func (a *Application) EnsureCurrentQuestion(sessionID string) (Question, error) {
	sessionVersion, started, err := a.practice.StartPracticeSession(sessionID)
	if err != nil {
		return Question{}, err
	}
	if started {
		a.voice.PublishSessionStarted(sessionVersion)
	}
	return a.voice.EnsureCurrentQuestion(sessionID)
}

func (a *Application) SubmitTurn(
	questionID string,
	request SubmitTurnRequest,
	failOnce bool,
) (Turn, error) {
	question, ok := a.voice.GetQuestion(questionID)
	if !ok {
		return Turn{}, ErrQuestionNotFound
	}
	if err := a.practice.AuthorizePracticeTurn(
		question.SessionID,
		request.RetryRequestID != "",
	); err != nil {
		return Turn{}, err
	}
	turn, err := a.voice.PrepareTurn(questionID, request)
	if err != nil {
		return Turn{}, err
	}
	if failOnce {
		a.failures.ArmFailure(questionID, turn.AnswerText)
	}
	if err := a.failures.CheckFailure(questionID, turn.AnswerText); err != nil {
		a.voice.PublishProcessingFailure(questionID)
		return Turn{}, err
	}
	turn, err = a.voice.CommitTurn(turn)
	if err != nil {
		return Turn{}, err
	}
	decision, err := a.practice.ApplyTurnOutcome(practiceTurnOutcome{
		SessionID: turn.SessionID,
		TurnID:    turn.ID,
		IsRetry:   turn.Kind == practice.TurnKindRetry,
	})
	if err != nil {
		return Turn{}, err
	}
	if turn.Kind != practice.TurnKindRetry {
		if decision.Completed {
			a.voice.PublishSessionCompleted(
				decision.SessionVersion,
				decision.EndReason,
			)
		} else {
			if _, err := a.voice.CreateNextQuestion(
				turn.SessionID,
				decision.NextQuestionNumber,
			); err != nil {
				return Turn{}, err
			}
		}
	}
	return turn, nil
}

func (a *Application) AnalyzeTurn(turnID string) (Analysis, error) {
	turn, ok := a.voice.GetTurn(turnID)
	if !ok {
		return Analysis{}, ErrTurnNotFound
	}
	if turn.Status != "completed" {
		return Analysis{}, ErrResourceConflict
	}
	analysis, _, created, err := a.review.Evaluate(turnEvaluationInput{
		TurnID:            turn.ID,
		SessionID:         turn.SessionID,
		QuestionID:        turn.QuestionID,
		AnswerText:        turn.AnswerText,
		EffectiveSequence: turn.Sequence,
		CompletedAt:       turn.CompletedAt.Format(time.RFC3339),
	})
	if err != nil {
		return Analysis{}, err
	}
	if created {
		a.voice.PublishReviewCompleted(
			analysis.ID,
			analysis.TurnID,
			analysis.Score,
			analysis.Summary,
		)
	}
	return analysis, nil
}

func (a *Application) ListAnalyses(turnID string) ([]Analysis, error) {
	if _, ok := a.voice.GetTurn(turnID); !ok {
		return nil, ErrTurnNotFound
	}
	return a.review.ListAnalyses(turnID), nil
}

func (a *Application) CreateRetry(feedbackID string) (RetryRequest, error) {
	retry, err := a.review.StartRetry(feedbackID)
	if err != nil {
		return RetryRequest{}, err
	}
	turn, err := a.voice.CreateRetryTurn(retry.ID, retry.OriginalTurnID)
	if err != nil {
		return RetryRequest{}, err
	}
	return a.review.CompleteRetry(retry.ID, turn.ID)
}

func (a *Application) ListHistory(sessionID string) ([]HistoryRecord, error) {
	if _, ok := a.practice.GetPracticeSession(sessionID); !ok {
		return nil, ErrSessionNotFound
	}
	return a.review.ListHistory(sessionID), nil
}
