package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestVoiceSessionResumeRecoversSagaCrashWindows(t *testing.T) {
	tests := []struct {
		name             string
		initialTurns     int
		initialEffective int
		failReviewSaves  int
		wantEffective    int
		wantCompleted    bool
		wantQuestion     bool
		wantReview       bool
	}{
		{
			name:          "after ConfirmTurn before Practice progress",
			initialTurns:  1,
			wantEffective: 1,
			wantQuestion:  true,
		},
		{
			name:             "after Practice progress before Review checkpoint",
			initialTurns:     1,
			initialEffective: 2,
			failReviewSaves:  1,
			wantEffective:    3,
			wantCompleted:    true,
			wantReview:       true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := newAgentVoiceConversation(test.initialTurns)
			practice := newAgentVoicePractice(test.initialEffective)
			seedVoiceSessionHistory(
				conversations,
				test.initialEffective,
			)
			reviews := newAgentVoiceReview()
			orchestrator := newAgentVoiceOrchestrator(
				t,
				conversations,
				practice,
				reviews,
			)
			command := conversation.ConfirmVoiceTurnCommand{
				CandidateID:    "candidate-1",
				IdempotencyKey: "original-confirm",
			}

			if test.failReviewSaves == 0 {
				if _, err := conversations.Confirm(
					context.Background(),
					agentVoiceActor("a"),
					command,
				); err != nil {
					t.Fatalf("create unprogressed Turn: %v", err)
				}
			} else {
				conversations.reviewSaveFailures = test.failReviewSaves
				if _, err := orchestrator.Confirm(
					context.Background(),
					agentVoiceActor("a"),
					command,
				); err != nil {
					t.Fatalf("create missing Review checkpoint: %v", err)
				}
				orchestrator.completionTasks.Wait()
			}

			application := newVoiceSessionTestApplication(
				t,
				conversations,
				practice,
				reviews,
				orchestrator,
			)
			state, err := application.Resume(
				context.Background(),
				agentVoiceActor("a"),
				"thread-1",
				"matter-1",
			)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			orchestrator.completionTasks.Wait()
			if test.wantReview && state.Review == nil {
				state, err = application.Resume(
					context.Background(),
					agentVoiceActor("a"),
					"thread-1",
					"matter-1",
				)
				if err != nil {
					t.Fatalf("resume completed Review: %v", err)
				}
			}
			if state.Session.EffectiveTurns != test.wantEffective ||
				state.Session.Completed != test.wantCompleted ||
				(state.Question != nil) != test.wantQuestion ||
				(state.Review != nil) != test.wantReview {
				t.Fatalf("recovered state = %#v", state)
			}
			if state.Turn == nil ||
				state.Turn.EffectiveTurns != state.Session.EffectiveTurns ||
				state.Turn.SessionCompleted != state.Session.Completed {
				t.Fatalf("Turn/Session progress diverged: %#v", state)
			}
			if test.wantReview &&
				(state.Turn.ReviewID == "" ||
					state.Review.ID != state.Turn.ReviewID) {
				t.Fatalf("Turn/Review checkpoint diverged: %#v", state)
			}
			if practice.effectiveTurns != test.wantEffective ||
				conversations.turnCreations != 1 {
				t.Fatalf(
					"recovery duplicated authority: effective=%d turns=%d",
					practice.effectiveTurns,
					conversations.turnCreations,
				)
			}
		})
	}
}

func seedVoiceSessionHistory(
	conversations *agentVoiceConversation,
	effectiveTurns int,
) {
	for effectiveTurn := 1; effectiveTurn <= effectiveTurns; effectiveTurn++ {
		turnID := fmt.Sprintf("history-turn-%d", effectiveTurn)
		conversations.turns[turnID] = conversation.ConfirmedVoiceTurn{
			ID:                    turnID,
			SessionID:             "session-1",
			QuestionID:            fmt.Sprintf("history-question-%d", effectiveTurn),
			CountsTowardTurnLimit: true,
			EffectiveTurns:        effectiveTurn,
		}
	}
}

func TestVoiceSessionReviewRequiresFrozenCompletedShape(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	valid := SessionReview{
		ID:                    "review-1",
		SessionID:             "session-1",
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-3",
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &ReviewResult{
			OverallScore: 80,
			Summary:      "Clear answer.",
			Conclusions: []ReviewConclusion{{
				Key:      "overall",
				Category: "fluency",
				Message:  "Clear.",
			}},
		},
		CreatedAt:   now,
		UpdatedAt:   now,
		CompletedAt: &now,
	}
	if !validVoiceSessionReview(valid, valid.ID) {
		t.Fatal("valid completed Review was rejected")
	}
	for name, mutate := range map[string]func(*SessionReview){
		"wrong id": func(item *SessionReview) {
			item.ID = "review-other"
		},
		"invalid source version": func(item *SessionReview) {
			item.SourceTurnVersion = "provider-model"
		},
		"completed without result": func(item *SessionReview) {
			item.Result = nil
		},
		"empty evidence conclusion": func(item *SessionReview) {
			item.Result.Conclusions[0].Message = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			item := valid
			result := *valid.Result
			result.Conclusions = append(
				[]ReviewConclusion(nil),
				valid.Result.Conclusions...,
			)
			item.Result = &result
			mutate(&item)
			if validVoiceSessionReview(item, valid.ID) {
				t.Fatalf("invalid Review accepted: %#v", item)
			}
		})
	}
}

func TestVoiceSessionReviewEnforcesMetadataAndResultBudgets(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	valid := SessionReview{
		ID:                    "review-1",
		SessionID:             "session-1",
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-3",
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result:                maximumVoiceReviewResult(t),
		CreatedAt:             now,
		UpdatedAt:             now,
		CompletedAt:           &now,
	}
	if !validVoiceSessionReview(valid, valid.ID) {
		t.Fatal("maximum valid Review result was rejected")
	}

	invalidUTF8 := string([]byte{0xff})
	for name, mutate := range map[string]func(*SessionReview){
		"review id over 128 bytes": func(item *SessionReview) {
			item.ID = strings.Repeat("r", 129)
		},
		"session id over 128 bytes": func(item *SessionReview) {
			item.SessionID = strings.Repeat("s", 129)
		},
		"implementation version over 128 bytes": func(item *SessionReview) {
			item.ImplementationVersion = strings.Repeat("i", 129)
		},
		"source turn id over 128 bytes": func(item *SessionReview) {
			item.SourceTurnID = strings.Repeat("t", 129)
		},
		"metadata invalid UTF-8": func(item *SessionReview) {
			item.ImplementationVersion = invalidUTF8
		},
		"metadata contains NUL": func(item *SessionReview) {
			item.ImplementationVersion = "review\x00v1"
		},
		"summary over 2048 bytes": func(item *SessionReview) {
			item.Result.Summary = strings.Repeat("s", 2049)
		},
		"more than eight conclusions": func(item *SessionReview) {
			item.Result.Conclusions = append(
				item.Result.Conclusions,
				item.Result.Conclusions[0],
			)
		},
		"key over 64 bytes": func(item *SessionReview) {
			item.Result.Conclusions[0].Key = strings.Repeat("k", 65)
		},
		"category over 64 bytes": func(item *SessionReview) {
			item.Result.Conclusions[0].Category = strings.Repeat("c", 65)
		},
		"message over 2048 bytes": func(item *SessionReview) {
			item.Result.Conclusions[0].Message = strings.Repeat("m", 2049)
		},
		"suggestion over 2048 bytes": func(item *SessionReview) {
			item.Result.Conclusions[0].Suggestion = strings.Repeat("s", 2049)
		},
		"suggestion trims to empty": func(item *SessionReview) {
			item.Result.Conclusions[0].Suggestion = " \t\n"
		},
		"result string invalid UTF-8": func(item *SessionReview) {
			item.Result.Conclusions[0].Message = invalidUTF8
		},
		"result string contains NUL": func(item *SessionReview) {
			item.Result.Conclusions[0].Message = "clear\x00answer"
		},
		"marshaled result over 12 KiB": func(item *SessionReview) {
			for index := range item.Result.Conclusions {
				item.Result.Conclusions[index].Message =
					strings.Repeat("m", maxVoiceReviewTextUTF8Bytes)
				item.Result.Conclusions[index].Suggestion =
					strings.Repeat("s", maxVoiceReviewTextUTF8Bytes)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			item := valid
			result := *valid.Result
			result.Conclusions = append(
				[]ReviewConclusion(nil),
				valid.Result.Conclusions...,
			)
			item.Result = &result
			mutate(&item)
			if validVoiceSessionReview(item, item.ID) {
				t.Fatalf("invalid Review accepted: %#v", item)
			}
		})
	}
}

func TestVoiceSessionListReviewsRejectsNonUUIDCursorIdentifiers(
	t *testing.T,
) {
	now := time.Unix(2, 0).UTC()
	completedAt := now.Add(time.Second)
	valid := SessionReview{
		ID:                    "20000000-0000-4000-8000-000000000001",
		SessionID:             "session-1",
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-3",
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &ReviewResult{
			OverallScore: 80,
			Summary:      "Clear answer.",
			Conclusions: []ReviewConclusion{{
				Key:      "overall",
				Category: "fluency",
				Message:  "Clear.",
			}},
		},
		CreatedAt:   now,
		UpdatedAt:   completedAt,
		CompletedAt: &completedAt,
	}
	tests := []struct {
		name    string
		page    ReviewHistoryPage
		query   ReviewHistoryQuery
		wantErr error
	}{
		{
			name: "item Review ID is not UUID",
			page: ReviewHistoryPage{Items: []SessionReview{
				func() SessionReview {
					item := valid
					item.ID = "review-not-uuid"
					return item
				}(),
			}},
			wantErr: ErrInvalidContext,
		},
		{
			name: "next Review ID is not UUID",
			page: ReviewHistoryPage{
				Items: []SessionReview{valid},
				Next: &ReviewHistoryCursor{
					CreatedAt: valid.CreatedAt,
					ReviewID:  "review-not-uuid",
				},
			},
			wantErr: ErrInvalidContext,
		},
		{
			name: "input cursor Review ID is not UUID",
			page: ReviewHistoryPage{
				Items: []SessionReview{valid},
			},
			query: ReviewHistoryQuery{
				Limit: 1,
				Before: &ReviewHistoryCursor{
					CreatedAt: now.Add(time.Second),
					ReviewID:  "review-not-uuid",
				},
			},
			wantErr: ErrInvalidRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := newAgentVoiceConversation(3)
			practice := newAgentVoicePractice(0)
			reviews := newAgentVoiceReview()
			orchestrator := newAgentVoiceOrchestrator(
				t,
				conversations,
				practice,
				reviews,
			)
			application := newVoiceSessionTestApplication(
				t,
				conversations,
				practice,
				reviews,
				orchestrator,
			)
			application.reviews = fixedVoiceReviewPageReader{
				page: test.page,
			}
			query := test.query
			if query.Limit == 0 {
				query.Limit = 1
			}
			if _, err := application.ListReviews(
				context.Background(),
				agentVoiceActor("a"),
				query,
			); !errors.Is(err, test.wantErr) {
				t.Fatalf("non-UUID adapter identifier error = %v", err)
			}
		})
	}
}

func maximumVoiceReviewResult(t *testing.T) *ReviewResult {
	t.Helper()
	result := &ReviewResult{
		OverallScore: 100,
		Summary:      strings.Repeat("s", maxVoiceReviewSummaryUTF8Bytes),
		Conclusions: make(
			[]ReviewConclusion,
			maxVoiceReviewConclusions,
		),
	}
	for index := range result.Conclusions {
		result.Conclusions[index] = ReviewConclusion{
			Key: fmt.Sprintf("%02d", index) +
				strings.Repeat("k", maxVoiceReviewLabelUTF8Bytes-2),
			Category: strings.Repeat(
				"c",
				maxVoiceReviewLabelUTF8Bytes,
			),
			Message:    strings.Repeat("m", 700),
			Suggestion: strings.Repeat("s", 300),
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal Voice Review Result fixture: %v", err)
	}
	remaining := maxVoiceReviewResultJSONBytes - len(payload)
	last := &result.Conclusions[len(result.Conclusions)-1]
	if remaining < 0 ||
		len(last.Suggestion)+remaining > maxVoiceReviewTextUTF8Bytes {
		t.Fatalf(
			"Voice Review fixture cannot reach budget: bytes=%d remaining=%d",
			len(payload),
			remaining,
		)
	}
	last.Suggestion += strings.Repeat("x", remaining)
	return result
}

func TestVoiceSessionStartsWithoutMatterFromFrozenScenario(t *testing.T) {
	session := Session{
		ID:                       "session-1",
		PlanID:                   "plan-1",
		ThreadID:                 "thread-1",
		ScenarioType:             "DAILY",
		ScenarioModel:            "HOTEL_CHECKIN_AND_ISSUE_HANDLING",
		PromptModel:              voiceSessionTestPrompt(),
		SessionVersion:           1,
		TurnLimit:                3,
		Status:                   "in_progress",
		InterviewerParticipantID: "participant-facilitator",
		CandidateParticipantID:   "participant-learner",
	}
	conversations := newAgentVoiceConversation(3)
	reviews := newAgentVoiceReview()
	application, err := NewSessionApplication(
		fixedVoiceSessionPort{session: session},
		voiceSessionTestQuestions{},
		voiceSessionTestCheckpoints{conversations: conversations},
		newAgentVoiceOrchestrator(
			t,
			conversations,
			newAgentVoicePractice(0),
			reviews,
		),
		voiceSessionTestReviews{reviews: reviews},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Start(
		context.Background(),
		agentVoiceActor("a"),
		session.ThreadID,
		"",
		"start-without-matter",
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Matter.ID != "" || state.Question == nil {
		t.Fatalf("matter-free state = %+v", state)
	}
}

func TestNewVoiceSessionApplicationRequiresCheckpointPort(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	_, err := NewSessionApplication(
		fixedVoiceSessionPort{session: Session{}},
		voiceSessionTestQuestions{},
		nil,
		newAgentVoiceOrchestrator(
			t,
			conversations,
			newAgentVoicePractice(0),
			newAgentVoiceReview(),
		),
		voiceSessionTestReviews{reviews: newAgentVoiceReview()},
		voiceSessionTestMatters{},
	)
	if err == nil {
		t.Fatal("NewSessionApplication accepted a missing Checkpoint port")
	}
}

func TestVoiceSessionRestoresFormalEarlyCompletionBeforeMaximum(t *testing.T) {
	session := Session{
		ID:                       "session-early",
		PlanID:                   "plan-early",
		ThreadID:                 "thread-1",
		MatterID:                 "matter-1",
		ScenarioType:             "INTERVIEW",
		ScenarioModel:            "PROJECT_EXPERIENCE_DEEP_DIVE",
		PromptModel:              voiceSessionTestPrompt(),
		SessionVersion:           4,
		EffectiveTurns:           2,
		TurnLimit:                3,
		Completed:                true,
		Status:                   "completed",
		InterviewerParticipantID: "participant-interviewer",
		CandidateParticipantID:   "participant-a",
	}
	turn := conversation.ConfirmedVoiceTurn{
		ID:               "turn-early",
		SessionID:        session.ID,
		EffectiveTurns:   session.EffectiveTurns,
		SessionCompleted: true,
		ReviewID:         "review-early",
	}
	reviews := newAgentVoiceReview()
	reviews.bySession[session.ID] = ReviewCheckpoint{
		ID:           turn.ReviewID,
		SessionID:    session.ID,
		SourceTurnID: turn.ID,
	}
	orchestrator := newAgentVoiceOrchestrator(
		t,
		newAgentVoiceConversation(3),
		newAgentVoicePractice(0),
		reviews,
	)
	application, err := NewSessionApplication(
		fixedVoiceSessionPort{session: session},
		voiceSessionTestQuestions{},
		fixedVoiceCheckpoint{
			turn:    turn,
			history: fixedVoiceHistory(session, turn),
		},
		orchestrator,
		voiceSessionTestReviews{reviews: reviews},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Resume(
		context.Background(),
		agentVoiceActor("a"),
		session.ThreadID,
		session.MatterID,
	)
	if err != nil {
		t.Fatalf("Resume early-completed Session: %v", err)
	}
	if !state.Session.Completed ||
		state.Session.EffectiveTurns != 2 ||
		state.Session.TurnLimit != 3 ||
		state.Question != nil ||
		state.Turn == nil ||
		state.Review == nil ||
		state.Review.ID != turn.ReviewID {
		t.Fatalf("early-completed state = %#v", state)
	}
}

func TestVoiceSessionRestoresCompletedIELTSFullMockWithoutReview(
	t *testing.T,
) {
	session := Session{
		ID:                       "session-1",
		PlanID:                   "plan-ielts-full",
		ThreadID:                 "thread-1",
		MatterID:                 "matter-1",
		ScenarioType:             "EXAM",
		ScenarioModel:            ieltsSpeakingFullMockModel,
		PromptModel:              voiceSessionTestPrompt(),
		SessionVersion:           15,
		EffectiveTurns:           14,
		TurnLimit:                14,
		Completed:                true,
		Status:                   "completed",
		InterviewerParticipantID: "participant-interviewer",
		CandidateParticipantID:   "participant-a",
	}
	conversations := newAgentVoiceConversation(14)
	candidateID := agentVoiceCandidateID(14)
	candidate := conversations.candidates[candidateID]
	candidate.SessionID = session.ID
	conversations.candidates[candidateID] = candidate
	turn := conversation.ConfirmedVoiceTurn{
		ID:                      "turn-14",
		SessionID:               session.ID,
		QuestionID:              candidate.QuestionID,
		QuestionSpeakerID:       candidate.QuestionSpeakerID,
		AddresseeParticipantIDs: candidate.AddresseeParticipantIDs,
		RespondentParticipantID: candidate.RespondentParticipantID,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
		EffectiveTurns:          session.EffectiveTurns,
		SessionCompleted:        true,
	}
	conversations.turns[turn.ID] = turn
	practice := newAgentVoicePracticeWithLimit(14, 14)
	practice.skipReview = true
	practice.turns[turn.ID] = TurnProgress{
		EffectiveTurns:   14,
		SessionVersion:   15,
		TurnLimit:        14,
		SessionCompleted: true,
	}
	completions := newAgentVoiceCompletionEvaluation()
	application, err := NewSessionApplication(
		fixedVoiceSessionPort{session: session},
		voiceSessionTestQuestions{},
		fixedVoiceCheckpoint{
			turn:    turn,
			history: fixedVoiceHistory(session, turn),
		},
		newAgentVoiceOrchestratorWithCompletion(
			t,
			conversations,
			practice,
			newAgentVoiceReview(),
			completions,
		),
		voiceSessionTestReviews{reviews: newAgentVoiceReview()},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Resume(
		context.Background(),
		agentVoiceActor("a"),
		session.ThreadID,
		session.MatterID,
	)
	if err != nil {
		t.Fatalf("Resume completed IELTS full mock: %v", err)
	}
	if !state.Session.Completed ||
		state.Turn == nil ||
		state.Question != nil ||
		state.Review != nil {
		t.Fatalf("completed IELTS full mock state = %#v", state)
	}
	application.orchestrator.completionTasks.Wait()
	if completions.creations != 1 || completions.calls != 1 {
		t.Fatalf(
			"completion recovery = %d creations / %d calls",
			completions.creations,
			completions.calls,
		)
	}
}

func newVoiceSessionTestApplication(
	t *testing.T,
	conversations *agentVoiceConversation,
	practice *agentVoicePractice,
	reviews *agentVoiceReview,
	orchestrator *RoundOrchestrator,
) *SessionApplication {
	t.Helper()
	application, err := NewSessionApplication(
		&voiceSessionTestSessions{practice: practice},
		voiceSessionTestQuestions{},
		voiceSessionTestCheckpoints{conversations: conversations},
		orchestrator,
		voiceSessionTestReviews{reviews: reviews},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("new Voice Session application: %v", err)
	}
	return application
}

type voiceSessionTestSessions struct {
	practice *agentVoicePractice
}

type fixedVoiceSessionPort struct {
	session Session
}

func (port fixedVoiceSessionPort) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (Session, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return port.session, nil
}

type fixedVoiceCheckpoint struct {
	turn    conversation.ConfirmedVoiceTurn
	history []TurnExchange
}

func (checkpoint fixedVoiceCheckpoint) LatestTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	return checkpoint.turn, true, nil
}

func (checkpoint fixedVoiceCheckpoint) ListTurnHistory(
	context.Context,
	requestcontext.Actor,
	string,
) ([]TurnExchange, error) {
	return checkpoint.history, nil
}

func fixedVoiceHistory(
	session Session,
	latest conversation.ConfirmedVoiceTurn,
) []TurnExchange {
	history := make([]TurnExchange, 0, session.EffectiveTurns)
	for effectiveTurn := 1; effectiveTurn <= session.EffectiveTurns; effectiveTurn++ {
		questionID := fmt.Sprintf("question-%d", effectiveTurn)
		turn := conversation.ConfirmedVoiceTurn{
			ID:                    fmt.Sprintf("turn-%d", effectiveTurn),
			SessionID:             session.ID,
			QuestionID:            questionID,
			CountsTowardTurnLimit: true,
			EffectiveTurns:        effectiveTurn,
			SessionCompleted:      session.Completed && effectiveTurn == session.EffectiveTurns,
		}
		if effectiveTurn == session.EffectiveTurns {
			turn = latest
			turn.SessionID = session.ID
			turn.QuestionID = questionID
			turn.CountsTowardTurnLimit = true
			turn.EffectiveTurns = effectiveTurn
			turn.SessionCompleted = session.Completed
		}
		history = append(history, TurnExchange{
			Question: conversation.VoiceQuestion{
				ID:        questionID,
				SessionID: session.ID,
				Type:      "PRIMARY",
				Text:      fmt.Sprintf("Question %d", effectiveTurn),
			},
			Turn: turn,
		})
	}
	return history
}

func (sessions *voiceSessionTestSessions) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (Session, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) current() Session {
	sessions.practice.mu.Lock()
	defer sessions.practice.mu.Unlock()
	effective := sessions.practice.effectiveTurns
	return Session{
		ID:             "session-1",
		PlanID:         "plan-1",
		ThreadID:       "thread-1",
		MatterID:       "matter-1",
		ScenarioType:   "INTERVIEW",
		ScenarioModel:  "PROJECT_EXPERIENCE_DEEP_DIVE",
		PromptModel:    voiceSessionTestPrompt(),
		SessionVersion: effective + 1,
		EffectiveTurns: effective,
		TurnLimit:      3,
		Completed:      effective == 3,
		Status: func() string {
			if effective == 3 {
				return "completed"
			}
			return "in_progress"
		}(),
		InterviewerParticipantID: "participant-interviewer",
		CandidateParticipantID:   "participant-a",
	}
}

func voiceSessionTestPrompt() ScenarioPrompt {
	return ScenarioPrompt{
		PublicSceneBrief: "Discuss one project in a realistic conversation.",
		PracticeGoal:     "Explain decisions with clear evidence.",
		UserRole:         "Candidate",
		AIRole:           "Technical interviewer",
		PersonaSummary:   "Professional and concise",
		FocusAreas:       []string{"clarity"},
		TurnBlueprints:   []string{"Ask about the project"},
	}
}

type voiceSessionTestQuestions struct{}

func (voiceSessionTestQuestions) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	sequence int,
) (conversation.VoiceQuestion, error) {
	return conversation.VoiceQuestion{
		ID:                      "question-next",
		SessionID:               session.ID,
		Type:                    "PRIMARY",
		Text:                    "What happened next?",
		SpeakerParticipantID:    session.InterviewerParticipantID,
		AddresseeParticipantIDs: []string{session.CandidateParticipantID},
	}, nil
}

func (voiceSessionTestQuestions) GetQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	questionID string,
) (conversation.VoiceQuestion, error) {
	return conversation.VoiceQuestion{
		ID:        questionID,
		SessionID: "session-1",
		Type:      "PRIMARY",
		Text:      "What happened next?",
	}, nil
}

type voiceSessionTestCheckpoints struct {
	conversations *agentVoiceConversation
}

func (checkpoints voiceSessionTestCheckpoints) LatestTurn(
	_ context.Context,
	_ requestcontext.Actor,
	sessionID string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	checkpoints.conversations.mu.Lock()
	defer checkpoints.conversations.mu.Unlock()
	var latest conversation.ConfirmedVoiceTurn
	for _, turn := range checkpoints.conversations.turns {
		if turn.SessionID == sessionID &&
			(latest.ID == "" ||
				turn.EffectiveTurns > latest.EffectiveTurns) {
			latest = turn
		}
	}
	return latest, latest.ID != "", nil
}

func (checkpoints voiceSessionTestCheckpoints) ListTurnHistory(
	_ context.Context,
	_ requestcontext.Actor,
	sessionID string,
) ([]TurnExchange, error) {
	checkpoints.conversations.mu.Lock()
	defer checkpoints.conversations.mu.Unlock()
	history := make([]TurnExchange, 0, len(checkpoints.conversations.turns))
	for _, turn := range checkpoints.conversations.turns {
		if turn.SessionID != sessionID || turn.EffectiveTurns < 1 {
			continue
		}
		history = append(history, TurnExchange{
			Question: conversation.VoiceQuestion{
				ID:        turn.QuestionID,
				SessionID: sessionID,
				Type:      "PRIMARY",
				Text:      "Question " + turn.QuestionID,
			},
			Turn: turn,
		})
	}
	for index := 1; index < len(history); index++ {
		for current := index; current > 0 &&
			history[current].Turn.EffectiveTurns <
				history[current-1].Turn.EffectiveTurns; current-- {
			history[current], history[current-1] =
				history[current-1], history[current]
		}
	}
	return history, nil
}

var (
	_ CheckpointPort = fixedVoiceCheckpoint{}
	_ CheckpointPort = voiceSessionTestCheckpoints{}
)

type voiceSessionTestReviews struct {
	reviews *agentVoiceReview
	history []SessionReview
}

func TestVoiceReviewConclusionJSONPreservesScorePresence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		conclusion  ReviewConclusion
		wantPresent bool
		wantScore   float64
	}{
		{
			name: "explicit zero",
			conclusion: ReviewConclusion{
				Score:        0,
				ScorePresent: true,
			},
			wantPresent: true,
			wantScore:   0,
		},
		{
			name:        "legacy zero",
			conclusion:  ReviewConclusion{Score: 0},
			wantPresent: false,
		},
		{
			name:        "legacy nonzero",
			conclusion:  ReviewConclusion{Score: 72},
			wantPresent: true,
			wantScore:   72,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(test.conclusion)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			score, present := wire["score"]
			if present != test.wantPresent {
				t.Fatalf(
					"score presence=%t, want %t; JSON=%s",
					present,
					test.wantPresent,
					encoded,
				)
			}
			if test.wantPresent && score != test.wantScore {
				t.Fatalf("score=%v, want %v", score, test.wantScore)
			}
		})
	}
}

func (reader voiceSessionTestReviews) GetReview(
	_ context.Context,
	_ requestcontext.Actor,
	reviewID string,
) (SessionReview, error) {
	reader.reviews.mu.Lock()
	defer reader.reviews.mu.Unlock()
	for _, item := range reader.reviews.bySession {
		if item.ID == reviewID {
			now := time.Unix(2, 0).UTC()
			return SessionReview{
				ID:                    item.ID,
				SessionID:             item.SessionID,
				SourceTurnID:          item.SourceTurnID,
				Status:                "completed",
				ImplementationVersion: "review-v1",
				SourceTurnVersion:     "conversation-turn:evidence-v1",
				Result: &ReviewResult{
					OverallScore: 80,
					Summary:      "Clear answer.",
					Conclusions: []ReviewConclusion{{
						Key:      "overall",
						Category: "fluency",
						Message:  "Clear.",
					}},
				},
				CreatedAt:   now,
				UpdatedAt:   now,
				CompletedAt: &now,
			}, nil
		}
	}
	return SessionReview{}, ErrNotFound
}

func (reader voiceSessionTestReviews) ListReviews(
	_ context.Context,
	_ requestcontext.Actor,
	query ReviewHistoryQuery,
) (ReviewHistoryPage, error) {
	items := make([]SessionReview, 0, query.Limit)
	for _, item := range reader.history {
		if query.Before != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			continue
		}
		items = append(items, item)
		if len(items) == query.Limit {
			break
		}
	}
	page := ReviewHistoryPage{Items: items}
	consumed := 0
	for _, item := range reader.history {
		if query.Before == nil ||
			reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			consumed++
		}
	}
	if consumed > len(items) && len(items) > 0 {
		last := items[len(items)-1]
		page.Next = &ReviewHistoryCursor{
			CreatedAt: last.CreatedAt,
			ReviewID:  last.ID,
		}
	}
	return page, nil
}

type fixedVoiceReviewPageReader struct {
	page ReviewHistoryPage
}

func (reader fixedVoiceReviewPageReader) GetReview(
	context.Context,
	requestcontext.Actor,
	string,
) (SessionReview, error) {
	return SessionReview{}, ErrNotFound
}

func (reader fixedVoiceReviewPageReader) ListReviews(
	context.Context,
	requestcontext.Actor,
	ReviewHistoryQuery,
) (ReviewHistoryPage, error) {
	return reader.page, nil
}

type voiceSessionTestMatters struct{}

func (voiceSessionTestMatters) ReadOwned(
	_ context.Context,
	actor requestcontext.Actor,
	matterID string,
) (matter.Matter, error) {
	if actor.UserID != "user-a" || matterID != "matter-1" {
		return matter.Matter{}, matter.ErrNotFound
	}
	now := time.Unix(1, 0).UTC()
	return matter.Matter{
		ID:        matterID,
		OwnerID:   actor.UserID,
		Title:     "Customer renewal",
		Status:    matter.StatusActive,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
