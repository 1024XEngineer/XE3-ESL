package agent

import (
	"context"
	"errors"
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
				"",
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
		ID:                       "session-1",
		PlanID:                   "agent-thread:thread-1",
		ThreadID:                 "thread-1",
		MatterID:                 "matter-1",
		SessionVersion:           effective + 1,
		EffectiveTurns:           effective,
		TurnLimit:                3,
		Completed:                effective == 3,
		InterviewerParticipantID: "participant-interviewer",
		CandidateParticipantID:   "participant-a",
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
