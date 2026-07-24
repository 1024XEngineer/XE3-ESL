package smoke

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

const (
	DemoUserID              = "user_demo"
	DemoScenarioDefinition  = "scenario_programmer_interview"
	DemoRoleDefinition      = "role_technical_interviewer"
	DemoPracticeOption      = "option_full_interview"
	demoInterviewerID       = "participant_interviewer_001"
	demoCandidateID         = "participant_candidate_001"
	demoPreparationProfile  = "profile_demo_001"
	demoPreparationSnapshot = "preparation_snapshot_demo_001"
	demoPracticePlan        = "plan_demo_001"
	demoPracticeSession     = "session_demo_001"
)

var (
	ErrInvalidAnswer      = errors.New("answer_text must not be empty")
	ErrRecoverableFailure = errors.New("deterministic provider temporarily unavailable")
	ErrScenarioNotFound   = errors.New("scenario definition not found")
	ErrProfileNotFound    = errors.New("preparation profile not found")
	ErrSnapshotNotFound   = errors.New("preparation snapshot not found")
	ErrPlanNotFound       = errors.New("practice plan not found")
	ErrSessionNotFound    = errors.New("practice session not found")
	ErrSessionCompleted   = errors.New("practice session is already completed")
	ErrQuestionNotFound   = errors.New("question not found")
	ErrTurnNotFound       = errors.New("turn not found")
	ErrAnalysisNotFound   = errors.New("turn analysis not found")
	ErrFeedbackNotFound   = errors.New("feedback item not found")
	ErrRetryNotFound      = errors.New("retry request not found")
	ErrRetryConflict      = errors.New("retry request already exists for feedback item")
	ErrVersionConflict    = errors.New("resource version does not match")
	ErrInvalidSelection   = errors.New("request does not match the deterministic scenario")
	ErrResourceConflict   = errors.New("resource already exists")
)

type Question = conversation.Question
type Turn = conversation.Turn
type Event = conversation.Event
type Analysis = review.Analysis
type Feedback = review.Feedback
type RetryRequest = review.RetryRequest
type HistoryRecord = review.HistoryRecord

type Runtime struct {
	mu sync.Mutex

	now time.Time

	profileCreated  bool
	snapshotCreated bool
	planCreated     bool
	sessionCreated  bool
	sessionStatus   string
	sessionVersion  int
	effectiveTurns  int

	questions []Question
	turns     []Turn
	analyses  []Analysis
	feedback  []Feedback
	retries   []RetryRequest
	history   []HistoryRecord
	events    []Event

	retryTurnByRequest     map[string]string
	retryOriginalByRequest map[string]string
	turnDecisions          map[string]practice.TurnDecision
	subscribers            map[chan Event]struct{}
}

func NewRuntime() *Runtime {
	return &Runtime{
		now:                    time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
		retryTurnByRequest:     make(map[string]string),
		retryOriginalByRequest: make(map[string]string),
		turnDecisions:          make(map[string]practice.TurnDecision),
		subscribers:            make(map[chan Event]struct{}),
		sessionStatus:          "not_started",
	}
}

func (r *Runtime) timestamp(offset int) string {
	return r.now.Add(time.Duration(offset) * time.Second).Format(time.RFC3339)
}

func (r *Runtime) createProfile() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profileCreated = true
	return map[string]any{
		"preparation_profile_id": demoPreparationProfile,
		"user_id":                DemoUserID,
		"resume_ref":             "resume_demo_backend_v1",
		"job_description_ref":    "jd_demo_backend_v1",
		"background_summary":     "Backend engineer preparing for an English technical interview.",
		"version":                1,
		"updated_at":             r.timestamp(1),
	}
}

func (r *Runtime) createSnapshot() (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.profileCreated {
		return nil, ErrResourceConflict
	}
	r.snapshotCreated = true
	return map[string]any{
		"preparation_snapshot_id":  demoPreparationSnapshot,
		"source_profile_id":        demoPreparationProfile,
		"source_version":           1,
		"resume_snapshot":          "Go backend engineer; API reliability project.",
		"job_description_snapshot": "Build reliable APIs and explain engineering trade-offs.",
		"background_snapshot":      "Backend engineer preparing for an English technical interview.",
		"created_at":               r.timestamp(2),
	}, nil
}

func (r *Runtime) createPlan() (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.planCreated = true
	return map[string]any{
		"practice_plan_id":            demoPracticePlan,
		"user_id":                     DemoUserID,
		"scenario_definition_id":      DemoScenarioDefinition,
		"scenario_definition_version": 1,
		"scenario_type":               "INTERVIEW",
		"scenario_config_id":          "scenario_config_backend",
		"scenario_config_version":     1,
		"preparation_profile_id":      demoPreparationProfile,
		"selected_role_ids":           []string{DemoRoleDefinition},
		"plan_revision":               1,
		"practice_plan_status":        "ready",
		"created_at":                  r.timestamp(3),
		"updated_at":                  r.timestamp(3),
	}, nil
}

func (r *Runtime) createSession() (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planCreated {
		return nil, ErrResourceConflict
	}
	if r.sessionCreated {
		return nil, ErrResourceConflict
	}
	r.sessionCreated = true
	r.sessionStatus = "starting"
	r.sessionVersion = 1
	return map[string]any{
		"practice_session": r.sessionLocked(),
		"snapshot":         r.snapshotLocked(),
	}, nil
}

func (r *Runtime) sessionLocked() map[string]any {
	session := map[string]any{
		"practice_session_id":     demoPracticeSession,
		"practice_plan_id":        demoPracticePlan,
		"scenario_type":           "INTERVIEW",
		"snapshot_id":             "snapshot_session_demo_001",
		"practice_session_status": r.sessionStatus,
		"session_version":         r.sessionVersion,
		"created_at":              r.timestamp(4),
	}
	if r.sessionStatus != "starting" {
		session["started_at"] = r.timestamp(5)
	}
	if r.sessionStatus == "completed" {
		session["ended_at"] = r.timestamp(80)
		session["end_reason"] = "COVERAGE_SATISFIED_AT_CHECKPOINT"
	}
	return session
}

func (r *Runtime) conversationBootstrap() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := map[string]any{
		"last_event_sequence": r.lastEventSequenceLocked(),
	}
	if len(r.questions) > 0 {
		result["current_question"] = r.questions[len(r.questions)-1]
	}
	return result
}

func (r *Runtime) currentQuestion() (Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.questions) == 0 {
		return Question{}, ErrQuestionNotFound
	}
	return r.questions[len(r.questions)-1], nil
}

func (r *Runtime) saveQuestion(
	sessionID string,
	sequence int,
	draft conversation.QuestionDraft,
) (Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sessionID != demoPracticeSession {
		return Question{}, ErrSessionNotFound
	}
	if sequence != len(r.questions)+1 {
		return Question{}, ErrResourceConflict
	}
	question := Question{
		ID:               fmt.Sprintf("question_demo_%03d", sequence),
		SessionID:        demoPracticeSession,
		SpeakerID:        demoInterviewerID,
		AddresseeIDs:     []string{demoCandidateID},
		ObjectiveID:      draft.ObjectiveID,
		Type:             draft.Type,
		ParentQuestionID: draft.ParentQuestionID,
		Content:          draft.Content,
		Sequence:         sequence,
		CreatedAt:        r.timestamp(10 + sequence*12),
	}
	r.questions = append(r.questions, question)
	payload := map[string]any{
		"question_id":               question.ID,
		"speaker_participant_id":    question.SpeakerID,
		"addressee_participant_ids": question.AddresseeIDs,
		"objective_id":              question.ObjectiveID,
		"question_type":             question.Type,
		"content":                   question.Content,
		"sequence":                  question.Sequence,
	}
	if question.ParentQuestionID != "" {
		payload["parent_question_id"] = question.ParentQuestionID
	}
	r.appendEventLocked("question.created", payload)
	return question, nil
}

func (r *Runtime) prepareTurn(
	questionID string,
	request conversation.SubmitTurnRequest,
) (Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	answer := strings.TrimSpace(request.AnswerText)
	if answer == "" {
		return Turn{}, ErrInvalidAnswer
	}
	question, ok := r.findQuestionLocked(questionID)
	if !ok {
		return Turn{}, ErrQuestionNotFound
	}

	turnNumber := len(r.turns) + 1
	turn := Turn{}
	if request.RetryRequestID != "" {
		retryTurnID, ok := r.retryTurnByRequest[request.RetryRequestID]
		if !ok {
			return Turn{}, ErrRetryNotFound
		}
		retryTurn, ok := r.findTurnLocked(retryTurnID)
		if !ok || retryTurn.QuestionID != questionID || retryTurn.Status != "answering" {
			return Turn{}, ErrRetryConflict
		}
		turn = retryTurn
		turn.AnswerText = answer
		turn.AudioAssetID = request.AudioAssetID
		turn.InteractionMode = request.InteractionMode
		turn.Status = "completed"
		turn.SubmittedAt = r.timestamp(72 + len(r.retryTurnByRequest)*2)
		turn.CompletedAt = r.timestamp(73 + len(r.retryTurnByRequest)*2)
	} else {
		for _, existing := range r.turns {
			if !existing.IsRetry && existing.QuestionID == questionID {
				return Turn{}, ErrResourceConflict
			}
		}
		turn = Turn{
			ID:              fmt.Sprintf("turn_demo_%03d", turnNumber),
			SessionID:       demoPracticeSession,
			QuestionID:      question.ID,
			RespondentID:    demoCandidateID,
			Sequence:        question.Sequence,
			InteractionMode: request.InteractionMode,
			AnswerText:      answer,
			AudioAssetID:    request.AudioAssetID,
			Status:          "completed",
			IsRetry:         false,
			SubmittedAt:     r.timestamp(13 + question.Sequence*12),
			CreatedAt:       r.timestamp(13 + question.Sequence*12),
			CompletedAt:     r.timestamp(14 + question.Sequence*12),
		}
	}
	return turn, nil
}

func (r *Runtime) commitTurn(turn Turn) (Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.findQuestionLocked(turn.QuestionID); !ok {
		return Turn{}, ErrQuestionNotFound
	}
	if turn.IsRetry {
		existing, ok := r.findTurnLocked(turn.ID)
		if !ok || existing.Status != "answering" {
			return Turn{}, ErrResourceConflict
		}
		r.replaceTurnLocked(turn)
	} else {
		for _, existing := range r.turns {
			if !existing.IsRetry && existing.QuestionID == turn.QuestionID {
				return Turn{}, ErrResourceConflict
			}
		}
		r.turns = append(r.turns, turn)
	}
	r.appendEventLocked("turn.submitted", map[string]any{
		"turn_id": turn.ID, "question_id": turn.QuestionID, "turn_status": "submitted",
	})
	r.appendEventLocked("turn.processing", map[string]any{
		"turn_id": turn.ID, "question_id": turn.QuestionID, "turn_status": "processing",
	})
	r.appendEventLocked("turn.completed", map[string]any{
		"turn_id":                   turn.ID,
		"question_id":               turn.QuestionID,
		"respondent_participant_id": turn.RespondentID,
		"turn_status":               "completed",
		"completed_at":              turn.CompletedAt,
	})
	return turn, nil
}

func (r *Runtime) publishProcessingFailure(questionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEventLocked("answer.processing_failed", map[string]any{
		"question_id": questionID,
		"code":        "mock_provider_temporarily_unavailable",
		"message":     "The deterministic provider failed once; retry the same answer.",
		"retryable":   true,
	})
}

func (r *Runtime) publishReviewCompleted(analysis Analysis) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEventLocked("turn_analysis.completed", map[string]any{
		"turn_id": analysis.TurnID, "turn_analysis_id": analysis.ID,
		"score": analysis.Score, "summary": analysis.Summary,
	})
}

func (r *Runtime) publishSessionStarted(version int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEventLocked("practice_session.started", map[string]any{
		"practice_session_status": "in_progress",
		"session_version":         version,
	})
}

func (r *Runtime) publishSessionCompleted(version int, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEventLocked("practice_session.completed", map[string]any{
		"practice_session_status": "completed",
		"session_version":         version,
		"end_reason":              reason,
	})
}

func (r *Runtime) createRetryTurn(retryID, originalTurnID string) (Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.retryTurnByRequest[retryID]; ok {
		if r.retryOriginalByRequest[retryID] != originalTurnID {
			return Turn{}, ErrRetryConflict
		}
		turn, found := r.findTurnLocked(existingID)
		if !found {
			return Turn{}, ErrTurnNotFound
		}
		return turn, nil
	}
	original, ok := r.findTurnLocked(originalTurnID)
	if !ok {
		return Turn{}, ErrTurnNotFound
	}
	if original.Status != "completed" {
		return Turn{}, ErrRetryConflict
	}
	retryNumber := len(r.retryTurnByRequest) + 1
	retryTurn := Turn{
		ID:              fmt.Sprintf("turn_retry_demo_%03d", retryNumber),
		SessionID:       original.SessionID,
		QuestionID:      original.QuestionID,
		RespondentID:    original.RespondentID,
		Sequence:        original.Sequence,
		InteractionMode: "PUSH_TO_TALK",
		Status:          "answering",
		IsRetry:         true,
		CreatedAt:       r.timestamp(70 + retryNumber),
	}
	r.turns = append(r.turns, retryTurn)
	r.retryTurnByRequest[retryID] = retryTurn.ID
	r.retryOriginalByRequest[retryID] = originalTurnID
	return retryTurn, nil
}

func (r *Runtime) historyRecordsForSession(sessionID string) []HistoryRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]HistoryRecord, 0, len(r.history))
	for index := len(r.history) - 1; index >= 0; index-- {
		if r.history[index].SessionID == sessionID {
			result = append(result, r.history[index])
		}
	}
	return result
}

func (r *Runtime) eventsSnapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func (r *Runtime) effectiveTurnCountLocked() int {
	count := 0
	for _, turn := range r.turns {
		if turn.Status == "completed" && !turn.IsRetry {
			count++
		}
	}
	return count
}

func (r *Runtime) lastEventSequenceLocked() int {
	last := 0
	for _, event := range r.events {
		if event.Sequence > last {
			last = event.Sequence
		}
	}
	return last
}

func (r *Runtime) snapshotLocked() map[string]any {
	objectives := []map[string]any{
		{"objective_id": "introduction", "description": "Explain current experience clearly."},
		{"objective_id": "system_design", "description": "Explain a technical design and its trade-offs."},
		{"objective_id": "project_depth", "description": "Provide evidence of individual contribution."},
		{"objective_id": "collaboration", "description": "Explain cross-team communication and outcomes."},
	}
	return map[string]any{
		"snapshot_id":         "snapshot_session_demo_001",
		"practice_session_id": demoPracticeSession,
		"plan_revision":       1,
		"scenario_type":       "INTERVIEW",
		"scenario_definition_snapshot": map[string]any{
			"scenario_definition_id": DemoScenarioDefinition,
			"scenario_type":          "INTERVIEW",
			"name":                   "Programmer English Interview",
			"version":                1,
			"status":                 "active",
		},
		"scenario_config_snapshot": map[string]any{
			"scenario_config_id":     "scenario_config_backend",
			"scenario_definition_id": DemoScenarioDefinition,
			"config_type":            "INTERVIEW",
			"version":                1,
			"job_title":              "Backend Engineer",
			"job_description":        "Build reliable APIs and explain engineering trade-offs.",
			"focus_areas":            []string{"reliability", "ownership", "collaboration"},
		},
		"preparation_snapshot": map[string]any{
			"preparation_snapshot_id":  demoPreparationSnapshot,
			"source_profile_id":        demoPreparationProfile,
			"source_version":           1,
			"resume_snapshot":          "Go backend engineer; API reliability project.",
			"job_description_snapshot": "Build reliable APIs and explain engineering trade-offs.",
			"background_snapshot":      "Backend engineer preparing for an English technical interview.",
			"created_at":               r.timestamp(2),
		},
		"participants": []map[string]any{
			{
				"practice_participant_id": demoInterviewerID,
				"practice_session_id":     demoPracticeSession,
				"participant_role":        "INTERVIEWER",
				"subject_ref":             map[string]any{"namespace": "mock.actor", "subject_id": "interviewer_technical"},
				"role_definition_id":      DemoRoleDefinition,
				"role_snapshot":           roleDefinition(),
				"participant_order":       1,
			},
			{
				"practice_participant_id": demoCandidateID,
				"practice_session_id":     demoPracticeSession,
				"participant_role":        "CANDIDATE",
				"subject_ref":             map[string]any{"namespace": "speakup.user", "subject_id": DemoUserID},
				"participant_order":       2,
			},
		},
		"practice_option": practiceOption(),
		"session_policy": map[string]any{
			"suggested_duration_seconds":  900,
			"min_effective_turns":         4,
			"max_effective_turns":         6,
			"coverage_checkpoint_turn":    4,
			"max_follow_ups_per_question": 1,
			"target_objectives":           objectives,
			"early_completion_rule":       "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
		},
		"practice_focuses": objectives,
		"created_at":       r.timestamp(4),
	}
}

func roleDefinition() map[string]any {
	return map[string]any{
		"role_definition_id":     DemoRoleDefinition,
		"scenario_definition_id": DemoScenarioDefinition,
		"role_type":              "INTERVIEWER",
		"display_name":           "Technical Interviewer",
		"responsibilities":       "Ask focused questions and evaluate trade-offs.",
		"style":                  "direct and constructive",
		"focus_areas":            []string{"reliability", "ownership", "collaboration"},
		"version":                1,
	}
}

func practiceOption() map[string]any {
	return map[string]any{
		"practice_option_id":     DemoPracticeOption,
		"scenario_definition_id": DemoScenarioDefinition,
		"role_definition_id":     DemoRoleDefinition,
		"practice_option_type":   "FULL_SIMULATION",
		"display_name":           "Full interview simulation",
		"version":                1,
	}
}

func (r *Runtime) appendEventLocked(eventType string, payload map[string]any) {
	eventNumber := len(r.events) + 1
	replayable := isReplayableEvent(eventType)
	sequence := 0
	if replayable {
		for _, event := range r.events {
			if event.Replayable {
				sequence++
			}
		}
		sequence++
	}
	r.events = append(r.events, Event{
		ID:            fmt.Sprintf("event_demo_%03d", eventNumber),
		Type:          eventType,
		Version:       1,
		OccurredAt:    r.timestamp(100 + eventNumber),
		SessionID:     demoPracticeSession,
		Sequence:      sequence,
		CorrelationID: fmt.Sprintf("correlation_demo_%03d", eventNumber),
		Replayable:    replayable,
		Payload:       payload,
	})
	event := r.events[len(r.events)-1]
	for subscriber := range r.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(r.subscribers, subscriber)
			close(subscriber)
		}
	}
}

func isReplayableEvent(eventType string) bool {
	switch eventType {
	case "question.created",
		"turn.submitted",
		"turn.processing",
		"turn.completed",
		"turn_analysis.completed",
		"practice_session.started",
		"practice_session.completed":
		return true
	case "answer.processing_failed":
		return false
	case "stream.ready":
		return false
	default:
		panic("unclassified smoke event type: " + eventType)
	}
}

func (r *Runtime) subscribe(afterSequence int) ([]Event, <-chan Event, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	replay := make([]Event, 0)
	for _, event := range r.events {
		if event.Replayable && event.Sequence > afterSequence {
			replay = append(replay, event)
		}
	}
	channel := make(chan Event, 128)
	r.subscribers[channel] = struct{}{}
	unsubscribe := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.subscribers[channel]; ok {
			delete(r.subscribers, channel)
			close(channel)
		}
	}
	return replay, channel, unsubscribe
}

func (r *Runtime) findQuestionLocked(id string) (Question, bool) {
	for _, question := range r.questions {
		if question.ID == id {
			return question, true
		}
	}
	return Question{}, false
}

func (r *Runtime) findTurnLocked(id string) (Turn, bool) {
	for _, turn := range r.turns {
		if turn.ID == id {
			return turn, true
		}
	}
	return Turn{}, false
}

func (r *Runtime) replaceTurnLocked(updated Turn) {
	for index := range r.turns {
		if r.turns[index].ID == updated.ID {
			r.turns[index] = updated
			return
		}
	}
}

func (r *Runtime) findRetryLocked(id string) (RetryRequest, bool) {
	for _, retry := range r.retries {
		if retry.ID == id {
			return retry, true
		}
	}
	return RetryRequest{}, false
}

func (r *Runtime) getTurn(id string) (Turn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findTurnLocked(id)
}

func (r *Runtime) getQuestion(id string) (Question, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findQuestionLocked(id)
}

func (r *Runtime) analysesForTurn(turnID string) []Analysis {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Analysis, 0)
	for _, analysis := range r.analyses {
		if analysis.TurnID == turnID {
			result = append(result, analysis)
		}
	}
	return result
}

func (r *Runtime) feedbackForAnalysis(analysisID string) []Feedback {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Feedback, 0)
	for _, feedback := range r.feedback {
		if feedback.AnalysisID == analysisID {
			result = append(result, feedback)
		}
	}
	return result
}

func (r *Runtime) getRetry(id string) (RetryRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findRetryLocked(id)
}

func (r *Runtime) findFeedbackLocked(id string) (Feedback, bool) {
	for _, feedback := range r.feedback {
		if feedback.ID == id {
			return feedback, true
		}
	}
	return Feedback{}, false
}
