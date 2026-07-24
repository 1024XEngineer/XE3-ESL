package smoke

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

// Application is the smoke composition layer. It coordinates formal module
// services while leaving every resource mutation with its owning module.
type Application struct {
	preparation  *preparation.Service
	practice     *practice.Service
	conversation *conversation.Service
	review       *review.Service
	failures     FailureControl
}

func NewApplication(
	preparationService *preparation.Service,
	practiceService *practice.Service,
	conversationService *conversation.Service,
	reviewService *review.Service,
	failures FailureControl,
) *Application {
	return &Application{
		preparation:  preparationService,
		practice:     practiceService,
		conversation: conversationService,
		review:       reviewService,
		failures:     failures,
	}
}

func (a *Application) CreatePlan(
	request practice.CreatePlanRequest,
) (map[string]any, error) {
	if _, ok := a.preparation.GetScenario(request.ScenarioDefinitionID); !ok {
		return nil, ErrScenarioNotFound
	}
	if !a.preparation.ProfileExists(request.PreparationProfileID) {
		return nil, ErrProfileNotFound
	}
	return a.practice.CreatePlan(request)
}

func (a *Application) CreateSession(
	planID string,
	request practice.CreateSessionRequest,
) (map[string]any, error) {
	if !a.practice.PlanExists(planID) {
		return nil, ErrPlanNotFound
	}
	if !a.preparation.SnapshotExists(request.PreparationSnapshotID) {
		return nil, ErrSnapshotNotFound
	}
	return a.practice.CreateSession(planID, request)
}

func (a *Application) Bootstrap(sessionID string) (map[string]any, error) {
	session, ok := a.practice.GetSession(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}
	snapshot, ok := a.practice.GetSnapshot(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}
	result, err := a.conversation.Bootstrap(sessionID)
	if err != nil {
		return nil, err
	}
	result["practice_session"] = session
	result["snapshot"] = snapshot
	return result, nil
}

func (a *Application) EnsureCurrentQuestion(sessionID string) (Question, error) {
	version, started, err := a.practice.StartSession(sessionID)
	if err != nil {
		return Question{}, err
	}
	if started {
		a.conversation.PublishSessionStarted(version)
	}
	return a.conversation.EnsureCurrentQuestion(sessionID)
}

func (a *Application) SubmitTurn(
	questionID string,
	request conversation.SubmitTurnRequest,
	failOnce bool,
) (Turn, error) {
	question, ok := a.conversation.GetQuestion(questionID)
	if !ok {
		return Turn{}, ErrQuestionNotFound
	}
	if err := a.practice.AuthorizeTurn(
		question.SessionID,
		request.RetryRequestID != "",
	); err != nil {
		return Turn{}, err
	}
	turn, err := a.conversation.PrepareTurn(questionID, request)
	if err != nil {
		return Turn{}, err
	}
	if failOnce {
		a.failures.ArmFailure(questionID, turn.AnswerText)
	}
	if err := a.failures.CheckFailure(questionID, turn.AnswerText); err != nil {
		a.conversation.PublishProcessingFailure(questionID)
		return Turn{}, err
	}
	turn, err = a.conversation.CommitTurn(turn)
	if err != nil {
		return Turn{}, err
	}
	decision, err := a.practice.RecordTurnOutcome(practice.TurnOutcome{
		SessionID: turn.SessionID,
		TurnID:    turn.ID,
		IsRetry:   turn.IsRetry,
	})
	if err != nil {
		return Turn{}, err
	}
	if !turn.IsRetry {
		if decision.Completed {
			a.conversation.PublishSessionCompleted(
				decision.SessionVersion,
				decision.EndReason,
			)
		} else {
			if _, err := a.conversation.CreateNextQuestion(
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
	turn, ok := a.conversation.GetTurn(turnID)
	if !ok {
		return Analysis{}, ErrTurnNotFound
	}
	if turn.Status != "completed" {
		return Analysis{}, ErrResourceConflict
	}
	analysis, _, created, err := a.review.Evaluate(review.TurnInput{
		TurnID:            turn.ID,
		SessionID:         turn.SessionID,
		QuestionID:        turn.QuestionID,
		AnswerText:        turn.AnswerText,
		EffectiveSequence: turn.Sequence,
		CompletedAt:       turn.CompletedAt,
	})
	if err != nil {
		return Analysis{}, err
	}
	if created {
		a.conversation.PublishReviewCompleted(
			analysis.ID,
			analysis.TurnID,
			analysis.Score,
			analysis.Summary,
		)
	}
	return analysis, nil
}

func (a *Application) ListAnalyses(turnID string) ([]Analysis, error) {
	if _, ok := a.conversation.GetTurn(turnID); !ok {
		return nil, ErrTurnNotFound
	}
	return a.review.ListAnalyses(turnID), nil
}

func (a *Application) CreateRetry(feedbackID string) (RetryRequest, error) {
	retry, err := a.review.StartRetry(feedbackID)
	if err != nil {
		return RetryRequest{}, err
	}
	turn, err := a.conversation.CreateRetryTurn(retry.ID, retry.OriginalTurnID)
	if err != nil {
		return RetryRequest{}, err
	}
	return a.review.CompleteRetry(retry.ID, turn.ID)
}

func (a *Application) ListHistory(sessionID string) ([]HistoryRecord, error) {
	if _, ok := a.practice.GetSession(sessionID); !ok {
		return nil, ErrSessionNotFound
	}
	return a.review.ListHistory(sessionID), nil
}
