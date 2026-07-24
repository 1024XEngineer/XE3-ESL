package smoke

import (
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

type preparationBackend struct {
	runtime *Runtime
}

func (b preparationBackend) GetScenario(id string) (map[string]any, bool) {
	if id != DemoScenarioDefinition {
		return nil, false
	}
	return map[string]any{
		"scenario_definition": map[string]any{
			"scenario_definition_id": DemoScenarioDefinition,
			"scenario_type":          "INTERVIEW",
			"name":                   "Programmer English Interview",
			"version":                1,
			"status":                 "active",
		},
		"scenario_config": map[string]any{
			"scenario_config_id":     "scenario_config_backend",
			"scenario_definition_id": DemoScenarioDefinition,
			"config_type":            "INTERVIEW",
			"version":                1,
			"job_title":              "Backend Engineer",
			"job_description":        "Build reliable APIs and explain engineering trade-offs.",
			"focus_areas":            []string{"reliability", "ownership", "collaboration"},
		},
		"practice_options": []map[string]any{practiceOption()},
	}, true
}

func (b preparationBackend) ListRoles(scenarioID string) ([]map[string]any, bool) {
	if scenarioID != DemoScenarioDefinition {
		return nil, false
	}
	return []map[string]any{roleDefinition()}, true
}

func (b preparationBackend) CreateProfile(
	request preparation.CreateProfileRequest,
) (map[string]any, error) {
	result := b.runtime.createProfile()
	result["background_summary"] = request.BackgroundSummary
	if request.ResumeRef == "" {
		delete(result, "resume_ref")
	} else {
		result["resume_ref"] = request.ResumeRef
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

type practiceBackend struct {
	runtime *Runtime
}

func (b practiceBackend) CreatePlan(
	request practice.CreatePlanRequest,
) (map[string]any, error) {
	if request.ScenarioDefinitionID != DemoScenarioDefinition {
		return nil, ErrScenarioNotFound
	}
	if request.PreparationProfileID != demoPreparationProfile {
		return nil, ErrProfileNotFound
	}
	if request.ScenarioDefinitionVersion != 1 ||
		request.ScenarioConfigID != "scenario_config_backend" ||
		request.ScenarioConfigVersion != 1 ||
		len(request.SelectedRoleIDs) != 1 ||
		request.SelectedRoleIDs[0] != DemoRoleDefinition {
		return nil, ErrInvalidSelection
	}
	return b.runtime.createPlan()
}

func (b practiceBackend) PlanExists(id string) bool {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	return id == demoPracticePlan && b.runtime.planCreated
}

func (b practiceBackend) CreateSession(
	planID string,
	request practice.CreateSessionRequest,
) (map[string]any, error) {
	if planID != demoPracticePlan {
		return nil, ErrPlanNotFound
	}
	if request.ExpectedPlanRevision != 1 {
		return nil, ErrVersionConflict
	}
	if request.PreparationSnapshotID != demoPreparationSnapshot ||
		request.PracticeOptionID != DemoPracticeOption ||
		len(request.RoleDefinitionIDs) != 1 ||
		request.RoleDefinitionIDs[0] != DemoRoleDefinition {
		return nil, ErrInvalidSelection
	}
	return b.runtime.createSession()
}

func (b practiceBackend) GetSession(id string) (map[string]any, bool) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || id != demoPracticeSession {
		return nil, false
	}
	return b.runtime.sessionLocked(), true
}

func (b practiceBackend) GetSnapshot(sessionID string) (map[string]any, bool) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return nil, false
	}
	return b.runtime.snapshotLocked(), true
}

func (b practiceBackend) StartSession(sessionID string) (int, bool, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return 0, false, ErrSessionNotFound
	}
	switch b.runtime.sessionStatus {
	case "starting":
		b.runtime.sessionStatus = "in_progress"
		b.runtime.sessionVersion = 2
		return 2, true, nil
	case "in_progress":
		return b.runtime.sessionVersion, false, nil
	case "completed":
		return b.runtime.sessionVersion, false, nil
	default:
		return 0, false, ErrResourceConflict
	}
}

func (b practiceBackend) AuthorizeTurn(sessionID string, retry bool) error {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || sessionID != demoPracticeSession {
		return ErrSessionNotFound
	}
	if retry {
		switch b.runtime.sessionStatus {
		case "in_progress", "completed":
			return nil
		default:
			return ErrResourceConflict
		}
	}
	if b.runtime.sessionStatus != "in_progress" {
		return ErrSessionCompleted
	}
	return nil
}

func (b practiceBackend) RecordTurnOutcome(
	outcome practice.TurnOutcome,
) (practice.TurnDecision, error) {
	b.runtime.mu.Lock()
	defer b.runtime.mu.Unlock()
	if !b.runtime.sessionCreated || outcome.SessionID != demoPracticeSession {
		return practice.TurnDecision{}, ErrSessionNotFound
	}
	if decision, ok := b.runtime.turnDecisions[outcome.TurnID]; ok {
		return decision, nil
	}
	if outcome.IsRetry {
		decision := practice.TurnDecision{
			EffectiveTurns: b.runtime.effectiveTurns,
			Completed:      b.runtime.sessionStatus == "completed",
			SessionVersion: b.runtime.sessionVersion,
		}
		b.runtime.turnDecisions[outcome.TurnID] = decision
		return decision, nil
	}
	if b.runtime.sessionStatus != "in_progress" {
		return practice.TurnDecision{}, ErrSessionCompleted
	}
	b.runtime.effectiveTurns++
	b.runtime.sessionVersion++
	decision := practice.TurnDecision{
		EffectiveTurns:     b.runtime.effectiveTurns,
		NextQuestionNumber: b.runtime.effectiveTurns + 1,
		SessionVersion:     b.runtime.sessionVersion,
	}
	if b.runtime.effectiveTurns == 4 {
		b.runtime.sessionStatus = "completed"
		b.runtime.sessionVersion = 6
		decision.Completed = true
		decision.NextQuestionNumber = 0
		decision.SessionVersion = 6
		decision.EndReason = "COVERAGE_SATISFIED_AT_CHECKPOINT"
	}
	b.runtime.turnDecisions[outcome.TurnID] = decision
	return decision, nil
}

type conversationBackend struct {
	runtime *Runtime
}

func (b conversationBackend) Bootstrap(sessionID string) (map[string]any, error) {
	if sessionID != demoPracticeSession {
		return nil, ErrSessionNotFound
	}
	return b.runtime.conversationBootstrap(), nil
}

func (b conversationBackend) CurrentQuestion(
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

func (b conversationBackend) SaveQuestion(
	sessionID string,
	sequence int,
	draft conversation.QuestionDraft,
) (Question, error) {
	return b.runtime.saveQuestion(sessionID, sequence, draft)
}

func (b conversationBackend) PrepareTurn(
	questionID string,
	request conversation.SubmitTurnRequest,
) (Turn, error) {
	return b.runtime.prepareTurn(questionID, request)
}

func (b conversationBackend) CommitTurn(turn Turn) (Turn, error) {
	return b.runtime.commitTurn(turn)
}

func (b conversationBackend) CreateRetryTurn(
	retryID string,
	originalTurnID string,
) (Turn, error) {
	return b.runtime.createRetryTurn(retryID, originalTurnID)
}

func (b conversationBackend) PublishProcessingFailure(questionID string) {
	b.runtime.publishProcessingFailure(questionID)
}

func (b conversationBackend) PublishReviewCompleted(
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

func (b conversationBackend) PublishSessionStarted(version int) {
	b.runtime.publishSessionStarted(version)
}

func (b conversationBackend) PublishSessionCompleted(version int, reason string) {
	b.runtime.publishSessionCompleted(version, reason)
}

func (b conversationBackend) GetTurn(id string) (Turn, bool) {
	return b.runtime.getTurn(id)
}

func (b conversationBackend) GetQuestion(id string) (Question, bool) {
	return b.runtime.getQuestion(id)
}

func (b conversationBackend) Subscribe(
	sessionID string,
	afterSequence int,
) ([]Event, <-chan Event, func(), error) {
	if sessionID != demoPracticeSession {
		return nil, nil, nil, ErrSessionNotFound
	}
	replay, live, unsubscribe := b.runtime.subscribe(afterSequence)
	return replay, live, unsubscribe, nil
}

func (b conversationBackend) StreamReady(sessionID string) (Event, error) {
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
	turn review.TurnInput,
	evaluation review.Evaluation,
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
	case errors.Is(err, ErrScenarioNotFound):
		return 404, "scenario_definition_not_found"
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
