package smoke

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

const (
	DemoUserID              = "user_demo"
	DemoScenarioDefinition  = preparation.ProgrammerInterviewScenarioID
	DemoRoleDefinition      = preparation.TechnicalInterviewerRoleID
	DemoPracticeOption      = preparation.FullSimulationOptionID
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

	now     time.Time
	catalog preparation.CatalogReader

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
	turnDecisions          map[string]practice.ApplyTurnOutcomeResult
	subscribers            map[chan Event]struct{}
}

func NewRuntime() *Runtime {
	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		panic(fmt.Sprintf("build deterministic preparation catalog: %v", err))
	}
	return &Runtime{
		now:                    time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
		catalog:                catalog,
		retryTurnByRequest:     make(map[string]string),
		retryOriginalByRequest: make(map[string]string),
		turnDecisions:          make(map[string]practice.ApplyTurnOutcomeResult),
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

func (r *Runtime) createPlan(command practice.CreatePracticePlanCommand) (practice.PracticePlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.planCreated = true
	createdAt := r.now.Add(3 * time.Second)
	return practice.PracticePlan{
		ID:                        demoPracticePlan,
		UserID:                    DemoUserID,
		AgentThreadID:             command.AgentThreadID,
		MatterID:                  command.MatterID,
		ScenarioDefinitionID:      DemoScenarioDefinition,
		ScenarioDefinitionVersion: 1,
		ScenarioType:              practice.ScenarioTypeInterview,
		ScenarioModel:             practice.ScenarioModelProjectExperienceDeepDive,
		ScenarioConfigID:          preparation.BackendEngineerConfigID,
		ScenarioConfigVersion:     1,
		PreparationProfileID:      demoPreparationProfile,
		SelectedRoleIDs:           []string{DemoRoleDefinition},
		Revision:                  1,
		Status:                    practice.PracticePlanReady,
		CreatedAt:                 createdAt,
		UpdatedAt:                 createdAt,
	}, nil
}

func (r *Runtime) createSession() (practice.CreatePracticeSessionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planCreated {
		return practice.CreatePracticeSessionResult{}, ErrResourceConflict
	}
	if r.sessionCreated {
		return practice.CreatePracticeSessionResult{}, ErrResourceConflict
	}
	r.sessionCreated = true
	r.sessionStatus = string(practice.PracticeSessionStarting)
	r.sessionVersion = 1
	return practice.CreatePracticeSessionResult{
		Session:  r.sessionLocked(),
		Snapshot: r.snapshotLocked(),
	}, nil
}

func (r *Runtime) sessionLocked() practice.PracticeSession {
	session := practice.PracticeSession{
		ID:            demoPracticeSession,
		PlanID:        demoPracticePlan,
		ScenarioType:  practice.ScenarioTypeInterview,
		ScenarioModel: practice.ScenarioModelProjectExperienceDeepDive,
		SnapshotID:    "snapshot_session_demo_001",
		Status:        practice.PracticeSessionStatus(r.sessionStatus),
		Version:       r.sessionVersion,
		CreatedAt:     r.now.Add(4 * time.Second),
	}
	if r.sessionStatus != string(practice.PracticeSessionStarting) {
		startedAt := r.now.Add(5 * time.Second)
		session.StartedAt = &startedAt
	}
	if r.sessionStatus == string(practice.PracticeSessionCompleted) {
		endedAt := r.now.Add(80 * time.Second)
		session.EndedAt = &endedAt
		session.EndReason = practice.PracticeSessionEndCoverageSatisfiedAtCheckpoint
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

func (r *Runtime) snapshotLocked() practice.PracticeSessionSnapshot {
	catalogSnapshot, err := r.catalog.GetCatalogSnapshot(
		DemoScenarioDefinition,
		1,
		[]string{DemoRoleDefinition},
		DemoPracticeOption,
		1,
	)
	if err != nil {
		panic(fmt.Sprintf("resolve deterministic catalog snapshot: %v", err))
	}
	objectives := []practice.PracticeObjective{
		{ID: "introduction", Description: "Explain current experience clearly."},
		{ID: "system_design", Description: "Explain a technical design and its trade-offs."},
		{ID: "project_depth", Description: "Provide evidence of individual contribution."},
		{ID: "collaboration", Description: "Explain cross-team communication and outcomes."},
	}
	roleSnapshot := practice.RoleSnapshot{
		ID:                   catalogSnapshot.SelectedRoles[0].ID,
		ScenarioDefinitionID: catalogSnapshot.SelectedRoles[0].ScenarioDefinitionID,
		Type:                 catalogSnapshot.SelectedRoles[0].Type,
		DisplayName:          catalogSnapshot.SelectedRoles[0].DisplayName,
		Responsibilities:     catalogSnapshot.SelectedRoles[0].Responsibilities,
		Style:                catalogSnapshot.SelectedRoles[0].Style,
		FocusAreas:           catalogSnapshot.SelectedRoles[0].FocusAreas,
		VoiceConfigRef:       catalogSnapshot.SelectedRoles[0].VoiceConfigRef,
		Version:              catalogSnapshot.SelectedRoles[0].Version,
	}
	return practice.PracticeSessionSnapshot{
		ID:            "snapshot_session_demo_001",
		SessionID:     demoPracticeSession,
		PlanRevision:  1,
		ScenarioType:  practice.ScenarioTypeInterview,
		ScenarioModel: practice.ScenarioModelProjectExperienceDeepDive,
		ScenarioDefinition: practice.ScenarioDefinitionSnapshot{
			ID:      catalogSnapshot.ScenarioDefinition.ID,
			Type:    practice.ScenarioType(catalogSnapshot.ScenarioDefinition.Type),
			Model:   practice.ScenarioModel(catalogSnapshot.ScenarioDefinition.Model),
			Name:    catalogSnapshot.ScenarioDefinition.Name,
			Version: catalogSnapshot.ScenarioDefinition.Version,
			Status:  string(catalogSnapshot.ScenarioDefinition.Status),
		},
		ScenarioConfig: practice.ScenarioConfigSnapshot{
			ID:                   catalogSnapshot.ScenarioConfig.ID,
			ScenarioDefinitionID: catalogSnapshot.ScenarioConfig.ScenarioDefinitionID,
			Type:                 practice.ScenarioType(catalogSnapshot.ScenarioConfig.Type),
			Model:                practice.ScenarioModel(catalogSnapshot.ScenarioConfig.Model),
			Version:              catalogSnapshot.ScenarioConfig.Version,
			JobTitle:             catalogSnapshot.ScenarioConfig.JobTitle,
			JobDescription:       catalogSnapshot.ScenarioConfig.JobDescription,
			PromptModel: practice.ScenarioPromptModel{
				PublicSceneBrief: catalogSnapshot.ScenarioConfig.PromptModel.PublicSceneBrief,
				PracticeGoal:     catalogSnapshot.ScenarioConfig.PromptModel.PracticeGoal,
				UserRole:         catalogSnapshot.ScenarioConfig.PromptModel.UserRole,
				AIRole:           catalogSnapshot.ScenarioConfig.PromptModel.AIRole,
				PersonaSummary:   catalogSnapshot.ScenarioConfig.PromptModel.PersonaSummary,
				FocusAreas: append(
					[]string(nil),
					catalogSnapshot.ScenarioConfig.PromptModel.FocusAreas...,
				),
				TurnBlueprints: append(
					[]string(nil),
					catalogSnapshot.ScenarioConfig.PromptModel.TurnBlueprints...,
				),
				SuggestedDurationSeconds: catalogSnapshot.ScenarioConfig.PromptModel.SuggestedDurationSeconds,
			},
		},
		Preparation: practice.PreparationSnapshot{
			ID:                     demoPreparationSnapshot,
			SourceProfileID:        demoPreparationProfile,
			SourceVersion:          1,
			ResumeSnapshot:         "Go backend engineer; API reliability project.",
			JobDescriptionSnapshot: "Build reliable APIs and explain engineering trade-offs.",
			BackgroundSnapshot:     "Backend engineer preparing for an English technical interview.",
			CreatedAt:              r.now.Add(2 * time.Second),
		},
		Participants: []practice.PracticeParticipant{
			{
				ID:               demoInterviewerID,
				SessionID:        demoPracticeSession,
				ParticipantRole:  "INTERVIEWER",
				SubjectRef:       practice.SubjectRef{Namespace: "mock.actor", SubjectID: "interviewer_technical"},
				RoleDefinitionID: DemoRoleDefinition,
				RoleSnapshot:     &roleSnapshot,
				ParticipantOrder: 1,
			},
			{
				ID:               demoCandidateID,
				SessionID:        demoPracticeSession,
				ParticipantRole:  "CANDIDATE",
				SubjectRef:       practice.SubjectRef{Namespace: "speakup.user", SubjectID: DemoUserID},
				ParticipantOrder: 2,
			},
		},
		PracticeOption: practice.PracticeOptionSnapshot{
			ID:                   catalogSnapshot.PracticeOption.ID,
			ScenarioDefinitionID: catalogSnapshot.PracticeOption.ScenarioDefinitionID,
			RoleDefinitionID:     catalogSnapshot.PracticeOption.RoleDefinitionID,
			Type:                 string(catalogSnapshot.PracticeOption.Type),
			DisplayName:          catalogSnapshot.PracticeOption.DisplayName,
			Version:              catalogSnapshot.PracticeOption.Version,
		},
		SessionPolicy: practice.PracticeSessionPolicy{
			SuggestedDurationSeconds: 900,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			TargetObjectives:         objectives,
			EarlyCompletionRule:      practice.EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeFocuses: objectives,
		CreatedAt:       r.now.Add(4 * time.Second),
	}
}

func mustJSONMap(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode deterministic catalog value: %v", err))
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("decode deterministic catalog value: %v", err))
	}
	return result
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
