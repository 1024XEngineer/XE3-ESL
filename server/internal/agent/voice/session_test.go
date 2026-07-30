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
				); !errors.Is(err, errAgentVoiceCheckpoint) {
					t.Fatalf("create missing Review checkpoint: %v", err)
				}
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

func TestVoiceSessionReviewRequiresFrozenCompletedShape(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	valid := VoiceSessionReview{
		ID:                    "review-1",
		SessionID:             "session-1",
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-3",
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &VoiceReviewResult{
			OverallScore: 80,
			Summary:      "Clear answer.",
			Conclusions: []VoiceReviewConclusion{{
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
	for name, mutate := range map[string]func(*VoiceSessionReview){
		"wrong id": func(item *VoiceSessionReview) {
			item.ID = "review-other"
		},
		"invalid source version": func(item *VoiceSessionReview) {
			item.SourceTurnVersion = "provider-model"
		},
		"completed without result": func(item *VoiceSessionReview) {
			item.Result = nil
		},
		"empty evidence conclusion": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Message = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			item := valid
			result := *valid.Result
			result.Conclusions = append(
				[]VoiceReviewConclusion(nil),
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
	valid := VoiceSessionReview{
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
	for name, mutate := range map[string]func(*VoiceSessionReview){
		"review id over 128 bytes": func(item *VoiceSessionReview) {
			item.ID = strings.Repeat("r", 129)
		},
		"session id over 128 bytes": func(item *VoiceSessionReview) {
			item.SessionID = strings.Repeat("s", 129)
		},
		"implementation version over 128 bytes": func(item *VoiceSessionReview) {
			item.ImplementationVersion = strings.Repeat("i", 129)
		},
		"source turn id over 128 bytes": func(item *VoiceSessionReview) {
			item.SourceTurnID = strings.Repeat("t", 129)
		},
		"metadata invalid UTF-8": func(item *VoiceSessionReview) {
			item.ImplementationVersion = invalidUTF8
		},
		"metadata contains NUL": func(item *VoiceSessionReview) {
			item.ImplementationVersion = "review\x00v1"
		},
		"summary over 2048 bytes": func(item *VoiceSessionReview) {
			item.Result.Summary = strings.Repeat("s", 2049)
		},
		"more than eight conclusions": func(item *VoiceSessionReview) {
			item.Result.Conclusions = append(
				item.Result.Conclusions,
				item.Result.Conclusions[0],
			)
		},
		"key over 64 bytes": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Key = strings.Repeat("k", 65)
		},
		"category over 64 bytes": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Category = strings.Repeat("c", 65)
		},
		"message over 2048 bytes": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Message = strings.Repeat("m", 2049)
		},
		"suggestion over 2048 bytes": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Suggestion = strings.Repeat("s", 2049)
		},
		"suggestion trims to empty": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Suggestion = " \t\n"
		},
		"result string invalid UTF-8": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Message = invalidUTF8
		},
		"result string contains NUL": func(item *VoiceSessionReview) {
			item.Result.Conclusions[0].Message = "clear\x00answer"
		},
		"marshaled result over 12 KiB": func(item *VoiceSessionReview) {
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
				[]VoiceReviewConclusion(nil),
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
	valid := VoiceSessionReview{
		ID:                    "20000000-0000-4000-8000-000000000001",
		SessionID:             "session-1",
		Status:                "completed",
		ImplementationVersion: "review-v1",
		SourceTurnID:          "turn-3",
		SourceTurnVersion:     "conversation-turn:evidence-v1",
		Result: &VoiceReviewResult{
			OverallScore: 80,
			Summary:      "Clear answer.",
			Conclusions: []VoiceReviewConclusion{{
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
		page    VoiceReviewHistoryPage
		query   VoiceReviewHistoryQuery
		wantErr error
	}{
		{
			name: "item Review ID is not UUID",
			page: VoiceReviewHistoryPage{Items: []VoiceSessionReview{
				func() VoiceSessionReview {
					item := valid
					item.ID = "review-not-uuid"
					return item
				}(),
			}},
			wantErr: ErrInvalidContext,
		},
		{
			name: "next Review ID is not UUID",
			page: VoiceReviewHistoryPage{
				Items: []VoiceSessionReview{valid},
				Next: &VoiceReviewHistoryCursor{
					CreatedAt: valid.CreatedAt,
					ReviewID:  "review-not-uuid",
				},
			},
			wantErr: ErrInvalidContext,
		},
		{
			name: "input cursor Review ID is not UUID",
			page: VoiceReviewHistoryPage{
				Items: []VoiceSessionReview{valid},
			},
			query: VoiceReviewHistoryQuery{
				Limit: 1,
				Before: &VoiceReviewHistoryCursor{
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

func maximumVoiceReviewResult(t *testing.T) *VoiceReviewResult {
	t.Helper()
	result := &VoiceReviewResult{
		OverallScore: 100,
		Summary:      strings.Repeat("s", maxVoiceReviewSummaryUTF8Bytes),
		Conclusions: make(
			[]VoiceReviewConclusion,
			maxVoiceReviewConclusions,
		),
	}
	for index := range result.Conclusions {
		result.Conclusions[index] = VoiceReviewConclusion{
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
	session := VoicePracticeSession{
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
	application, err := NewVoiceSessionApplication(
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
		t.Fatalf("NewVoiceSessionApplication: %v", err)
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

func TestVoiceSessionRestoresFormalEarlyCompletionBeforeMaximum(t *testing.T) {
	session := VoicePracticeSession{
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
	reviews.bySession[session.ID] = VoiceReviewCheckpoint{
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
	application, err := NewVoiceSessionApplication(
		fixedVoiceSessionPort{session: session},
		voiceSessionTestQuestions{},
		fixedVoiceCheckpoint{turn: turn},
		orchestrator,
		voiceSessionTestReviews{reviews: reviews},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("NewVoiceSessionApplication: %v", err)
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
	session := VoicePracticeSession{
		ID:                       "session-ielts-full",
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
	turn := conversation.ConfirmedVoiceTurn{
		ID:               "turn-14",
		SessionID:        session.ID,
		EffectiveTurns:   session.EffectiveTurns,
		SessionCompleted: true,
	}
	application, err := NewVoiceSessionApplication(
		fixedVoiceSessionPort{session: session},
		voiceSessionTestQuestions{},
		fixedVoiceCheckpoint{turn: turn},
		newAgentVoiceOrchestrator(
			t,
			newAgentVoiceConversation(14),
			newAgentVoicePractice(14),
			newAgentVoiceReview(),
		),
		voiceSessionTestReviews{reviews: newAgentVoiceReview()},
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("NewVoiceSessionApplication: %v", err)
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
}

func newVoiceSessionTestApplication(
	t *testing.T,
	conversations *agentVoiceConversation,
	practice *agentVoicePractice,
	reviews *agentVoiceReview,
	orchestrator *VoiceRoundOrchestrator,
) *VoiceSessionApplication {
	t.Helper()
	application, err := NewVoiceSessionApplication(
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
	session VoicePracticeSession
}

func (port fixedVoiceSessionPort) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

type fixedVoiceCheckpoint struct {
	turn conversation.ConfirmedVoiceTurn
}

func (checkpoint fixedVoiceCheckpoint) LatestTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	return checkpoint.turn, true, nil
}

func (sessions *voiceSessionTestSessions) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) current() VoicePracticeSession {
	sessions.practice.mu.Lock()
	defer sessions.practice.mu.Unlock()
	effective := sessions.practice.effectiveTurns
	return VoicePracticeSession{
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

func voiceSessionTestPrompt() VoiceScenarioPrompt {
	return VoiceScenarioPrompt{
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
	session VoicePracticeSession,
	sequence int,
) (conversation.VoiceQuestion, error) {
	return conversation.VoiceQuestion{
		ID:                      "question-next",
		SessionID:               session.ID,
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

type voiceSessionTestReviews struct {
	reviews *agentVoiceReview
	history []VoiceSessionReview
}

func TestVoiceReviewConclusionJSONPreservesScorePresence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		conclusion  VoiceReviewConclusion
		wantPresent bool
		wantScore   float64
	}{
		{
			name: "explicit zero",
			conclusion: VoiceReviewConclusion{
				Score:        0,
				ScorePresent: true,
			},
			wantPresent: true,
			wantScore:   0,
		},
		{
			name:        "legacy zero",
			conclusion:  VoiceReviewConclusion{Score: 0},
			wantPresent: false,
		},
		{
			name:        "legacy nonzero",
			conclusion:  VoiceReviewConclusion{Score: 72},
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
) (VoiceSessionReview, error) {
	reader.reviews.mu.Lock()
	defer reader.reviews.mu.Unlock()
	for _, item := range reader.reviews.bySession {
		if item.ID == reviewID {
			now := time.Unix(2, 0).UTC()
			return VoiceSessionReview{
				ID:                    item.ID,
				SessionID:             item.SessionID,
				SourceTurnID:          item.SourceTurnID,
				Status:                "completed",
				ImplementationVersion: "review-v1",
				SourceTurnVersion:     "conversation-turn:evidence-v1",
				Result: &VoiceReviewResult{
					OverallScore: 80,
					Summary:      "Clear answer.",
					Conclusions: []VoiceReviewConclusion{{
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
	return VoiceSessionReview{}, ErrNotFound
}

func (reader voiceSessionTestReviews) ListReviews(
	_ context.Context,
	_ requestcontext.Actor,
	query VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
	items := make([]VoiceSessionReview, 0, query.Limit)
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
	page := VoiceReviewHistoryPage{Items: items}
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
		page.Next = &VoiceReviewHistoryCursor{
			CreatedAt: last.CreatedAt,
			ReviewID:  last.ID,
		}
	}
	return page, nil
}

type fixedVoiceReviewPageReader struct {
	page VoiceReviewHistoryPage
}

func (reader fixedVoiceReviewPageReader) GetReview(
	context.Context,
	requestcontext.Actor,
	string,
) (VoiceSessionReview, error) {
	return VoiceSessionReview{}, ErrNotFound
}

func (reader fixedVoiceReviewPageReader) ListReviews(
	context.Context,
	requestcontext.Actor,
	VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
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
