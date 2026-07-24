package smoke

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
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
	ErrInvalidAnswer       = errors.New("answer_text must not be empty")
	ErrRecoverableFailure  = errors.New("deterministic provider temporarily unavailable")
	ErrSessionCompleted    = errors.New("practice session is already completed")
	ErrQuestionNotFound    = errors.New("question not found")
	ErrTurnNotFound        = errors.New("turn not found")
	ErrFeedbackNotFound    = errors.New("feedback item not found")
	ErrIdempotencyConflict = errors.New("idempotency key was reused with a different request")
)

type Question struct {
	ID               string   `json:"question_id"`
	SessionID        string   `json:"practice_session_id"`
	SpeakerID        string   `json:"speaker_participant_id"`
	AddresseeIDs     []string `json:"addressee_participant_ids"`
	ObjectiveID      string   `json:"objective_id"`
	Type             string   `json:"question_type"`
	ParentQuestionID string   `json:"parent_question_id,omitempty"`
	Content          string   `json:"content"`
	Sequence         int      `json:"sequence"`
	CreatedAt        string   `json:"created_at"`
}

type Turn struct {
	ID              string `json:"turn_id"`
	SessionID       string `json:"practice_session_id"`
	QuestionID      string `json:"question_id"`
	RespondentID    string `json:"respondent_participant_id"`
	Sequence        int    `json:"sequence"`
	InteractionMode string `json:"interaction_mode"`
	AnswerText      string `json:"answer_text,omitempty"`
	Status          string `json:"turn_status"`
	IsRetry         bool   `json:"-"`
	SubmittedAt     string `json:"submitted_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	CompletedAt     string `json:"completed_at,omitempty"`
}

type Analysis struct {
	ID                 string `json:"turn_analysis_id"`
	TurnID             string `json:"turn_id"`
	EvaluatorVersion   string `json:"evaluator_version"`
	Status             string `json:"analysis_status"`
	Score              int    `json:"score"`
	Summary            string `json:"summary"`
	AnalysisTranscript string `json:"analysis_transcript"`
	CreatedAt          string `json:"created_at"`
	CompletedAt        string `json:"completed_at"`
}

type Feedback struct {
	ID         string           `json:"feedback_item_id"`
	AnalysisID string           `json:"turn_analysis_id"`
	Category   string           `json:"feedback_category"`
	Message    string           `json:"message"`
	Suggestion string           `json:"suggestion"`
	Evidence   []map[string]any `json:"evidence"`
	Retryable  bool             `json:"retryable"`
	CreatedAt  string           `json:"created_at"`
}

type RetryRequest struct {
	ID             string `json:"retry_request_id"`
	OriginalTurnID string `json:"original_turn_id"`
	FeedbackID     string `json:"feedback_item_id"`
	NewTurnID      string `json:"new_turn_id"`
	Status         string `json:"retry_status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type HistoryRecord struct {
	ID             string `json:"history_record_id"`
	SessionID      string `json:"practice_session_id"`
	TurnID         string `json:"turn_id"`
	AnalysisID     string `json:"turn_analysis_id"`
	RetryRequestID string `json:"retry_request_id,omitempty"`
	Score          int    `json:"score"`
	Summary        string `json:"summary"`
	ReviewedAt     string `json:"reviewed_at"`
}

type Event struct {
	ID            string         `json:"event_id"`
	Type          string         `json:"event_type"`
	Version       int            `json:"event_version"`
	OccurredAt    string         `json:"occurred_at"`
	SessionID     string         `json:"practice_session_id"`
	Sequence      int            `json:"sequence,omitempty"`
	CorrelationID string         `json:"correlation_id"`
	CausationID   string         `json:"causation_id,omitempty"`
	Replayable    bool           `json:"replayable"`
	Payload       map[string]any `json:"payload"`
}

type submitResult struct {
	Turn     Turn     `json:"turn"`
	Analysis Analysis `json:"turn_analysis"`
	Feedback Feedback `json:"feedback_item"`
}

type idempotentTurn struct {
	answer string
	result submitResult
}

type Runtime struct {
	mu sync.Mutex

	now time.Time

	profileCreated  bool
	snapshotCreated bool
	planCreated     bool
	sessionCreated  bool
	sessionStatus   string

	questions []Question
	turns     []Turn
	analyses  []Analysis
	feedback  []Feedback
	retries   []RetryRequest
	history   []HistoryRecord
	events    []Event

	failedOnce  map[string]bool
	submitted   map[string]idempotentTurn
	retryByKey  map[string]RetryRequest
	subscribers map[chan Event]struct{}
}

func NewRuntime() *Runtime {
	return &Runtime{
		now:           time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
		failedOnce:    make(map[string]bool),
		submitted:     make(map[string]idempotentTurn),
		retryByKey:    make(map[string]RetryRequest),
		subscribers:   make(map[chan Event]struct{}),
		sessionStatus: "not_started",
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
		return nil, errors.New("preparation profile must be created first")
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
	if !r.snapshotCreated {
		return nil, errors.New("preparation snapshot must be created first")
	}
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
		return nil, errors.New("practice plan must be created first")
	}
	r.sessionCreated = true
	r.sessionStatus = "starting"
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
		"session_version":         1,
		"created_at":              r.timestamp(4),
	}
	if r.sessionStatus != "starting" {
		session["started_at"] = r.timestamp(5)
		session["session_version"] = 2
	}
	if r.sessionStatus == "completed" {
		session["session_version"] = 6
		session["ended_at"] = r.timestamp(80)
		session["end_reason"] = "COVERAGE_SATISFIED_AT_CHECKPOINT"
	}
	return session
}

func (r *Runtime) bootstrap() (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sessionCreated {
		return nil, errors.New("practice session must be created first")
	}
	result := map[string]any{
		"practice_session":    r.sessionLocked(),
		"snapshot":            r.snapshotLocked(),
		"last_event_sequence": r.lastEventSequenceLocked(),
	}
	if len(r.questions) > 0 {
		result["current_question"] = r.questions[len(r.questions)-1]
	}
	return result, nil
}

func (r *Runtime) ensureCurrentQuestion() (Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sessionCreated {
		return Question{}, errors.New("practice session must be created first")
	}
	if len(r.questions) == 0 {
		r.sessionStatus = "in_progress"
		r.appendEventLocked("practice_session.started", map[string]any{
			"practice_session_status": r.sessionStatus,
			"session_version":         2,
		})
		r.questions = append(r.questions, r.newQuestionLocked(1))
	}
	return r.questions[len(r.questions)-1], nil
}

func (r *Runtime) currentQuestion() (Question, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.questions) == 0 {
		return Question{}, ErrQuestionNotFound
	}
	return r.questions[len(r.questions)-1], nil
}

func (r *Runtime) submitTurn(questionID, answer, retryRequestID, key string, failOnce bool) (submitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	answer = strings.TrimSpace(answer)
	if answer == "" {
		return submitResult{}, ErrInvalidAnswer
	}
	if existing, ok := r.submitted[key]; ok {
		if existing.answer != answer {
			return submitResult{}, ErrIdempotencyConflict
		}
		return existing.result, nil
	}
	if failOnce && !r.failedOnce[key] {
		r.failedOnce[key] = true
		r.appendEventLocked("answer.processing_failed", map[string]any{
			"question_id": questionID,
			"code":        "mock_provider_temporarily_unavailable",
			"message":     "The deterministic provider failed once; retry the same answer.",
			"retryable":   true,
		})
		return submitResult{}, ErrRecoverableFailure
	}
	isRetry := retryRequestID != ""
	if r.sessionStatus == "completed" && !isRetry {
		return submitResult{}, ErrSessionCompleted
	}
	question, ok := r.findQuestionLocked(questionID)
	if !ok {
		return submitResult{}, ErrQuestionNotFound
	}

	turnNumber := len(r.turns) + 1
	effectiveSequence := r.effectiveTurnCountLocked() + 1
	turn := Turn{}
	if isRetry {
		retry, ok := r.findRetryLocked(retryRequestID)
		if !ok {
			return submitResult{}, errors.New("retry request not found")
		}
		retryTurn, ok := r.findTurnLocked(retry.NewTurnID)
		if !ok || retryTurn.QuestionID != questionID {
			return submitResult{}, errors.New("retry turn does not match question")
		}
		turn = retryTurn
		turn.AnswerText = answer
		turn.InteractionMode = "PUSH_TO_TALK"
		turn.Status = "completed"
		turn.SubmittedAt = r.timestamp(20 + turnNumber*3)
		turn.CompletedAt = r.timestamp(21 + turnNumber*3)
		r.replaceTurnLocked(turn)
	} else {
		turn = Turn{
			ID:              fmt.Sprintf("turn_demo_%03d", turnNumber),
			SessionID:       demoPracticeSession,
			QuestionID:      question.ID,
			RespondentID:    demoCandidateID,
			Sequence:        question.Sequence,
			InteractionMode: "PUSH_TO_TALK",
			AnswerText:      answer,
			Status:          "completed",
			IsRetry:         false,
			SubmittedAt:     r.timestamp(20 + turnNumber*3),
			CreatedAt:       r.timestamp(20 + turnNumber*3),
			CompletedAt:     r.timestamp(21 + turnNumber*3),
		}
		r.turns = append(r.turns, turn)
	}
	r.appendEventLocked("turn.submitted", map[string]any{
		"turn_id": turn.ID, "question_id": question.ID, "turn_status": "submitted",
	})
	r.appendEventLocked("turn.processing", map[string]any{
		"turn_id": turn.ID, "question_id": question.ID, "turn_status": "processing",
	})
	r.appendEventLocked("turn.completed", map[string]any{
		"turn_id":                   turn.ID,
		"question_id":               question.ID,
		"respondent_participant_id": turn.RespondentID,
		"turn_status":               "completed",
		"completed_at":              turn.CompletedAt,
	})

	analysisNumber := len(r.analyses) + 1
	analysis := Analysis{
		ID:                 fmt.Sprintf("analysis_demo_%03d", analysisNumber),
		TurnID:             turn.ID,
		EvaluatorVersion:   "mock-review-v1",
		Status:             "completed",
		Score:              80 + effectiveSequence,
		Summary:            "Deterministic review completed for the submitted answer.",
		AnalysisTranscript: answer,
		CreatedAt:          r.timestamp(22 + turnNumber*3),
		CompletedAt:        r.timestamp(23 + turnNumber*3),
	}
	feedback := Feedback{
		ID:         fmt.Sprintf("feedback_demo_%03d", analysisNumber),
		AnalysisID: analysis.ID,
		Category:   "STRUCTURE",
		Message:    "The answer is clear and grounded in an example.",
		Suggestion: "State the trade-off and measurable outcome explicitly.",
		Evidence:   []map[string]any{{"transcript_text": answer}},
		Retryable:  true,
		CreatedAt:  analysis.CompletedAt,
	}
	r.analyses = append(r.analyses, analysis)
	r.feedback = append(r.feedback, feedback)
	r.history = append(r.history, HistoryRecord{
		ID:         fmt.Sprintf("history_demo_%03d", analysisNumber),
		SessionID:  demoPracticeSession,
		TurnID:     turn.ID,
		AnalysisID: analysis.ID,
		Score:      analysis.Score,
		Summary:    analysis.Summary,
		ReviewedAt: analysis.CompletedAt,
	})
	r.appendEventLocked("turn_analysis.completed", map[string]any{
		"turn_id": turn.ID, "turn_analysis_id": analysis.ID,
		"score": analysis.Score, "summary": analysis.Summary,
	})

	if !isRetry {
		if r.effectiveTurnCountLocked() == 4 {
			r.sessionStatus = "completed"
			r.appendEventLocked("practice_session.completed", map[string]any{
				"practice_session_status": "completed",
				"session_version":         6,
				"end_reason":              "COVERAGE_SATISFIED_AT_CHECKPOINT",
			})
		} else {
			next := r.newQuestionLocked(len(r.questions) + 1)
			r.questions = append(r.questions, next)
		}
	}

	result := submitResult{Turn: turn, Analysis: analysis, Feedback: feedback}
	r.submitted[key] = idempotentTurn{answer: answer, result: result}
	return result, nil
}

func (r *Runtime) createRetry(feedbackID, key string) (RetryRequest, Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if retry, ok := r.retryByKey[key]; ok {
		turn, _ := r.findTurnLocked(retry.NewTurnID)
		return retry, turn, nil
	}
	feedback, ok := r.findFeedbackLocked(feedbackID)
	if !ok {
		return RetryRequest{}, Turn{}, ErrFeedbackNotFound
	}
	var original Turn
	for _, analysis := range r.analyses {
		if analysis.ID == feedback.AnalysisID {
			original, ok = r.findTurnLocked(analysis.TurnID)
			break
		}
	}
	if !ok {
		return RetryRequest{}, Turn{}, ErrTurnNotFound
	}
	retryNumber := len(r.retries) + 1
	retryID := fmt.Sprintf("retry_demo_%03d", retryNumber)
	retryTurn := Turn{
		ID:              fmt.Sprintf("turn_retry_demo_%03d", retryNumber),
		SessionID:       original.SessionID,
		QuestionID:      original.QuestionID,
		RespondentID:    original.RespondentID,
		Sequence:        original.Sequence,
		InteractionMode: "PUSH_TO_TALK",
		Status:          "answering",
		IsRetry:         true,
		CreatedAt:       r.timestamp(60 + retryNumber),
	}
	r.turns = append(r.turns, retryTurn)
	retry := RetryRequest{
		ID:             retryID,
		OriginalTurnID: original.ID,
		FeedbackID:     feedback.ID,
		NewTurnID:      retryTurn.ID,
		Status:         "turn_created",
		CreatedAt:      r.timestamp(59 + retryNumber),
		UpdatedAt:      retryTurn.CreatedAt,
	}
	r.retries = append(r.retries, retry)
	r.retryByKey[key] = retry
	for index := range r.history {
		if r.history[index].TurnID == original.ID {
			r.history[index].RetryRequestID = retry.ID
		}
	}
	return retry, retryTurn, nil
}

func (r *Runtime) historyRecords() []HistoryRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]HistoryRecord, len(r.history))
	for index := range r.history {
		result[len(r.history)-1-index] = r.history[index]
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

func (r *Runtime) newQuestionLocked(sequence int) Question {
	objectives := []string{"introduction", "system_design", "project_depth", "collaboration"}
	contents := []string{
		"Please introduce yourself and the backend project you are most proud of.",
		"How did you design the reliability controls for that API?",
		"Which design decision was yours, and what trade-off did you make?",
		"How did you align the rollout with the teams that consumed the API?",
	}
	questionType := "PRIMARY"
	parentID := ""
	if sequence == 3 {
		questionType = "FOLLOW_UP"
		parentID = "question_demo_002"
	}
	question := Question{
		ID:               fmt.Sprintf("question_demo_%03d", sequence),
		SessionID:        demoPracticeSession,
		SpeakerID:        demoInterviewerID,
		AddresseeIDs:     []string{demoCandidateID},
		ObjectiveID:      objectives[sequence-1],
		Type:             questionType,
		ParentQuestionID: parentID,
		Content:          contents[sequence-1],
		Sequence:         sequence,
		CreatedAt:        r.timestamp(10 + sequence*4),
	}
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
	return question
}

func (r *Runtime) appendEventLocked(eventType string, payload map[string]any) {
	eventNumber := len(r.events) + 1
	replayable := eventType != "answer.processing_failed"
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
		}
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
	channel := make(chan Event, 64)
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
