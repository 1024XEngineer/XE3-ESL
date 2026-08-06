package smoke

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

type preparationBackend struct {
	runtime *Runtime
}

func (b preparationBackend) CreateProfile(
	request preparation.CreateProfileRequest,
) (map[string]any, error) {
	result := b.runtime.createProfile()
	result["background_summary"] = request.BackgroundSummary
	if request.ResumeID == "" {
		delete(result, "resume_id")
		delete(result, "resume_revision")
	} else {
		result["resume_id"] = request.ResumeID
		result["resume_revision"] = request.ResumeRevision
	}
	if request.JobDescriptionRef == "" {
		delete(result, "job_description_ref")
	} else {
		result["job_description_ref"] = request.JobDescriptionRef
	}
	return result, nil
}

func (b preparationBackend) CreateSnapshot(
	profileID string,
	request preparation.CreateSnapshotRequest,
) (map[string]any, error) {
	if !b.ProfileExists(profileID) {
		return nil, ErrProfileNotFound
	}
	if request.SourceVersion != 1 {
		return nil, ErrVersionConflict
	}
	return b.runtime.createSnapshot()
}

func (b preparationBackend) ProfileExists(id string) bool {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	return id == demoPreparationProfile && b.runtime.profileCreated
}

func (b preparationBackend) SnapshotExists(id string) bool {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	return id == demoPreparationSnapshot && b.runtime.snapshotCreated
}

func (b preparationBackend) CreatePracticePlan(
	ctx context.Context,
	request preparation.CreatePlanRequest,
) (preparation.PracticePlan, error) {
	if !b.SnapshotExists(request.PreparationSnapshotID) {
		return preparation.PracticePlan{}, ErrSnapshotNotFound
	}
	if request.PracticeOptionID != DemoPracticeOption {
		return preparation.PracticePlan{}, ErrInvalidSelection
	}
	selection, err := b.runtime.catalog.ResolveSelection(
		ctx,
		request.SceneID,
		request.SceneVersion,
		request.SelectedRoleIDs,
		request.PracticeOptionID,
	)
	if err != nil {
		if errors.Is(err, scene.ErrSceneNotFound) {
			return preparation.PracticePlan{}, ErrSceneNotFound
		}
		return preparation.PracticePlan{}, ErrInvalidSelection
	}
	option, err := selection.PracticeOption()
	if err != nil {
		return preparation.PracticePlan{}, ErrInvalidSelection
	}
	policy, err := planpolicy.NewResolver().ResolveSessionPolicy(
		selection.Scene,
		option,
		request.MaxEffectiveTurns,
	)
	if err != nil {
		return preparation.PracticePlan{}, ErrInvalidSelection
	}
	return b.runtime.createPlan(request, selection, policy)
}

func (b preparationBackend) PracticePlanExists(
	planID string,
	revision int,
) bool {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	return b.runtime.plan != nil &&
		b.runtime.plan.ID == planID &&
		b.runtime.plan.Revision == revision &&
		b.runtime.plan.Status == preparation.PlanStatusReady
}

type practiceBackend struct {
	runtime *Runtime
}

type practiceTurnDecision struct {
	EffectiveTurns     int
	Completed          bool
	NextQuestionNumber int
	SessionVersion     int
	EndReason          string
	NextAction         practiceNextAction
}

type practiceNextAction string

const (
	practiceNextActionMoveToNextObjective practiceNextAction = "MOVE_TO_NEXT_OBJECTIVE"
	practiceNextActionCompleteSession     practiceNextAction = "COMPLETE_SESSION"
)

type practiceTurnOutcome struct {
	TurnID    string
	SessionID string
	IsRetry   bool
}

func (b practiceBackend) CreatePracticeSession() (
	practice.SessionBootstrap,
	error,
) {
	return b.runtime.createSession()
}

func (b practiceBackend) GetPracticeSession(
	sessionID string,
) (practice.Session, bool) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return practice.Session{}, false
	}
	return b.runtime.sessionLocked(), true
}

func (b practiceBackend) GetPracticeSessionSnapshot(
	sessionID string,
) (practice.SessionSnapshot, bool) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return practice.SessionSnapshot{}, false
	}
	return b.runtime.snapshotLocked(), true
}

func (b practiceBackend) StartPracticeSession(
	sessionID string,
) (sessionVersion int, started bool, err error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return 0, false, ErrSessionNotFound
	}
	switch b.runtime.sessionStatus {
	case practice.SessionStarting:
		b.runtime.sessionStatus = practice.SessionInProgress
		b.runtime.sessionVersion = 2
		return 2, true, nil
	case practice.SessionInProgress,
		practice.SessionCompleted:
		return b.runtime.sessionVersion, false, nil
	default:
		return 0, false, ErrResourceConflict
	}
}

func (b practiceBackend) AuthorizePracticeTurn(
	sessionID string,
	isRetry bool,
) error {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return ErrSessionNotFound
	}
	if isRetry {
		switch b.runtime.sessionStatus {
		case practice.SessionInProgress,
			practice.SessionCompleted:
			return nil
		default:
			return ErrResourceConflict
		}
	}
	if b.runtime.sessionStatus != practice.SessionInProgress {
		return ErrSessionCompleted
	}
	return nil
}

func (b practiceBackend) ApplyTurnOutcome(
	outcome practiceTurnOutcome,
) (practiceTurnDecision, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || outcome.SessionID != demoPracticeSession {
		return practiceTurnDecision{}, ErrSessionNotFound
	}
	if decision, ok := b.runtime.turnDecisions[outcome.TurnID]; ok {
		return decision, nil
	}
	if outcome.IsRetry {
		decision := practiceTurnDecision{
			EffectiveTurns: b.runtime.effectiveTurns,
			Completed: b.runtime.sessionStatus ==
				practice.SessionCompleted,
			SessionVersion: b.runtime.sessionVersion,
		}
		b.runtime.turnDecisions[outcome.TurnID] = decision
		return decision, nil
	}
	if b.runtime.sessionStatus != practice.SessionInProgress {
		return practiceTurnDecision{}, ErrSessionCompleted
	}
	b.runtime.effectiveTurns++
	b.runtime.sessionVersion++
	decision := practiceTurnDecision{
		EffectiveTurns:     b.runtime.effectiveTurns,
		NextQuestionNumber: b.runtime.effectiveTurns + 1,
		SessionVersion:     b.runtime.sessionVersion,
		NextAction:         practiceNextActionMoveToNextObjective,
	}
	if b.runtime.effectiveTurns == 4 {
		b.runtime.sessionStatus = practice.SessionCompleted
		b.runtime.sessionVersion = 6
		decision.Completed = true
		decision.NextQuestionNumber = 0
		decision.SessionVersion = 6
		decision.EndReason = coverageSatisfiedAtCheckpointEndReason
		decision.NextAction = practiceNextActionCompleteSession
	}
	b.runtime.turnDecisions[outcome.TurnID] = decision
	return decision, nil
}

type voiceBackend struct {
	runtime *Runtime
}

func (b voiceBackend) Bootstrap(sessionID string) (map[string]any, error) {
	if sessionID != demoPracticeSession {
		return nil, ErrSessionNotFound
	}
	return b.runtime.voiceBootstrap(), nil
}

func (b voiceBackend) CurrentQuestion(
	sessionID string,
) (Question, bool, error) {
	if sessionID != demoPracticeSession {
		return Question{}, false, ErrSessionNotFound
	}
	question, err := b.runtime.currentQuestion()
	if errors.Is(err, ErrQuestionNotFound) {
		return Question{}, false, nil
	}
	return question, err == nil, err
}

func (b voiceBackend) SaveQuestion(
	sessionID string,
	sequence int,
	draft practice.QuestionDraft,
) (Question, error) {
	return b.runtime.saveQuestion(sessionID, sequence, draft)
}

func (b voiceBackend) PrepareTurn(
	questionID string,
	request SubmitTurnRequest,
) (Turn, error) {
	return b.runtime.prepareTurn(questionID, request)
}

func (b voiceBackend) CommitTurn(turn Turn) (Turn, error) {
	return b.runtime.commitTurn(turn)
}

func (b voiceBackend) CreateRetryTurn(
	retryID string,
	originalTurnID string,
) (Turn, error) {
	return b.runtime.createRetryTurn(retryID, originalTurnID)
}

func (b voiceBackend) PublishProcessingFailure(questionID string) {
	b.runtime.publishProcessingFailure(questionID)
}

func (b voiceBackend) PublishReviewCompleted(
	analysisID string,
	turnID string,
	score int,
	summary string,
) {
	b.runtime.publishReviewCompleted(Analysis{
		ID:      analysisID,
		TurnID:  turnID,
		Score:   score,
		Summary: summary,
	})
}

func (b voiceBackend) PublishSessionStarted(version int) {
	b.runtime.publishSessionStarted(version)
}

func (b voiceBackend) PublishSessionCompleted(version int, reason string) {
	b.runtime.publishSessionCompleted(version, reason)
}

func (b voiceBackend) GetTurn(id string) (Turn, bool) {
	return b.runtime.getTurn(id)
}

func (b voiceBackend) GetQuestion(id string) (Question, bool) {
	return b.runtime.getQuestion(id)
}

func (b voiceBackend) Subscribe(
	sessionID string,
	afterSequence int,
) ([]Event, <-chan Event, func(), error) {
	if sessionID != demoPracticeSession {
		return nil, nil, nil, ErrSessionNotFound
	}
	replay, live, unsubscribe := b.runtime.subscribe(afterSequence)
	return replay, live, unsubscribe, nil
}

func (b voiceBackend) StreamReady(sessionID string) (Event, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if sessionID != demoPracticeSession {
		return Event{}, ErrSessionNotFound
	}
	return Event{
		ID:            "event_stream_ready_001",
		Type:          "stream.ready",
		Version:       1,
		OccurredAt:    b.runtime.timestamp(99),
		SessionID:     demoPracticeSession,
		CorrelationID: "correlation_stream_ready_001",
		Replayable:    false,
		Payload: map[string]any{
			"last_sequence": b.runtime.lastEventSequenceLocked(),
		},
	}, nil
}

type reviewBackend struct {
	runtime *Runtime
}

func (b reviewBackend) ListAnalyses(turnID string) []Analysis {
	return b.runtime.analysesForTurn(turnID)
}

func (b reviewBackend) ListFeedback(analysisID string) ([]Feedback, bool) {
	b.runtime.mu.Lock()
	found := false
	for _, analysis := range b.runtime.analyses {
		if analysis.ID == analysisID {
			found = true
			break
		}
	}
	b.runtime.mu.Unlock()
	if !found {
		return nil, false
	}
	return b.runtime.feedbackForAnalysis(analysisID), true
}

func (b reviewBackend) SaveEvaluation(
	turn turnEvaluationInput,
	evaluation evaluationResult,
) (Analysis, Feedback, bool, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	for _, existing := range b.runtime.analyses {
		if existing.TurnID == turn.TurnID {
			for _, feedback := range b.runtime.feedback {
				if feedback.AnalysisID == existing.ID {
					return existing, feedback, false, nil
				}
			}
		}
	}
	number := len(b.runtime.analyses) + 1
	analysis := Analysis{
		ID:                 formatID("analysis_demo", number),
		TurnID:             turn.TurnID,
		EvaluatorVersion:   "mock-review-v1",
		Status:             "completed",
		Score:              evaluation.Score,
		Summary:            evaluation.Summary,
		AnalysisTranscript: evaluation.Transcript,
		CreatedAt:          addSeconds(turn.CompletedAt, 1),
		CompletedAt:        addSeconds(turn.CompletedAt, 2),
	}
	feedback := Feedback{
		ID:         formatID("feedback_demo", number),
		AnalysisID: analysis.ID,
		Category:   evaluation.Category,
		Message:    evaluation.Message,
		Suggestion: evaluation.Suggestion,
		Evidence:   evaluation.Evidence,
		Retryable:  true,
		CreatedAt:  analysis.CompletedAt,
	}
	b.runtime.analyses = append(b.runtime.analyses, analysis)
	b.runtime.feedback = append(b.runtime.feedback, feedback)
	b.runtime.history = append(b.runtime.history, HistoryRecord{
		ID:         formatID("history_demo", number),
		SessionID:  turn.SessionID,
		TurnID:     turn.TurnID,
		AnalysisID: analysis.ID,
		Score:      analysis.Score,
		Summary:    analysis.Summary,
		ReviewedAt: analysis.CompletedAt,
	})
	return analysis, feedback, true, nil
}

func (b reviewBackend) StartRetry(feedbackID string) (RetryRequest, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	feedback, ok := b.runtime.findFeedbackLocked(feedbackID)
	if !ok {
		return RetryRequest{}, ErrFeedbackNotFound
	}
	for _, existing := range b.runtime.retries {
		if existing.FeedbackID == feedbackID {
			return RetryRequest{}, ErrRetryConflict
		}
	}
	originalTurnID := ""
	for _, analysis := range b.runtime.analyses {
		if analysis.ID == feedback.AnalysisID {
			originalTurnID = analysis.TurnID
			break
		}
	}
	if originalTurnID == "" {
		return RetryRequest{}, ErrTurnNotFound
	}
	number := len(b.runtime.retries) + 1
	retry := RetryRequest{
		ID:             formatID("retry_demo", number),
		OriginalTurnID: originalTurnID,
		FeedbackID:     feedbackID,
		Status:         "pending",
		CreatedAt:      b.runtime.timestamp(69 + number),
		UpdatedAt:      b.runtime.timestamp(69 + number),
	}
	b.runtime.retries = append(b.runtime.retries, retry)
	return retry, nil
}

func (b reviewBackend) CompleteRetry(
	retryID string,
	newTurnID string,
) (RetryRequest, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	for index := range b.runtime.retries {
		if b.runtime.retries[index].ID != retryID {
			continue
		}
		retry := &b.runtime.retries[index]
		if retry.Status == "turn_created" {
			if retry.NewTurnID != newTurnID {
				return RetryRequest{}, ErrRetryConflict
			}
			return *retry, nil
		}
		if retry.Status != "pending" {
			return RetryRequest{}, ErrRetryConflict
		}
		retry.NewTurnID = newTurnID
		retry.Status = "turn_created"
		retry.UpdatedAt = b.runtime.timestamp(71 + index)
		for historyIndex := range b.runtime.history {
			if b.runtime.history[historyIndex].TurnID == retry.OriginalTurnID {
				b.runtime.history[historyIndex].RetryRequestID = retry.ID
			}
		}
		return *retry, nil
	}
	return RetryRequest{}, ErrRetryNotFound
}

func (b reviewBackend) GetRetry(id string) (RetryRequest, bool) {
	return b.runtime.getRetry(id)
}

func (b reviewBackend) ListHistory(sessionID string) []HistoryRecord {
	return b.runtime.historyRecordsForSession(sessionID)
}

func mapServiceError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrSceneNotFound):
		return 404, "scene_not_found"
	case errors.Is(err, ErrProfileNotFound):
		return 404, "preparation_profile_not_found"
	case errors.Is(err, ErrSnapshotNotFound):
		return 404, "resource_not_found"
	case errors.Is(err, ErrPlanNotFound):
		return 404, "practice_plan_not_found"
	case errors.Is(err, ErrSessionNotFound):
		return 404, "practice_session_not_found"
	case errors.Is(err, ErrQuestionNotFound):
		return 404, "question_not_found"
	case errors.Is(err, ErrTurnNotFound):
		return 404, "turn_not_found"
	case errors.Is(err, ErrAnalysisNotFound):
		return 404, "turn_analysis_not_found"
	case errors.Is(err, ErrFeedbackNotFound):
		return 404, "feedback_item_not_found"
	case errors.Is(err, ErrRetryNotFound):
		return 404, "retry_request_not_found"
	case errors.Is(err, ErrVersionConflict), errors.Is(err, ErrInvalidSelection),
		errors.Is(err, ErrResourceConflict):
		return 409, "resource_conflict"
	case errors.Is(err, ErrRetryConflict):
		return 409, "retry_request_conflict"
	default:
		return 500, "internal_error"
	}
}

func formatID(prefix string, number int) string {
	return fmt.Sprintf("%s_%03d", prefix, number)
}

func addSeconds(value string, seconds int) string {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return timestamp.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339)
}
