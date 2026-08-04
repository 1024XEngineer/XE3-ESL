package voicehttp

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
)

func TestSessionStateResponseContainsOnlyPracticeRuntimeState(t *testing.T) {
	question := practice.Question{
		ID:                      "question-1",
		SessionID:               "session-1",
		Type:                    "PRIMARY",
		Content:                 "Tell me about yourself.",
		SpeakerParticipantID:    "facilitator-1",
		AddresseeParticipantIDs: []string{"learner-1"},
	}
	turn := practice.Turn{
		ID:                    "turn-1",
		SessionID:             "session-1",
		QuestionID:            question.ID,
		AnswerText:            "I build reliable systems.",
		EffectiveTurns:        1,
		CountsTowardTurnLimit: true,
	}
	response := SessionStateResponse(practicevoice.SessionState{
		Session: practicevoice.Session{
			ID:             "session-1",
			PlanID:         "plan-1",
			SceneID:        "scene-1",
			SceneVersion:   1,
			SessionVersion: 2,
			EffectiveTurns: 1,
			TurnLimit:      3,
		},
		Question: &question,
		Turn:     &turn,
	})
	if response["practice_session_id"] != "session-1" ||
		response["current_question"] == nil ||
		response["current_turn"] == nil {
		t.Fatalf("response = %#v", response)
	}
	if _, leaked := response["review"]; leaked {
		t.Fatalf("Practice response contains Review: %#v", response)
	}
}

func TestConfirmedTurnResponseHasNoReviewCheckpoint(t *testing.T) {
	response := ConfirmedTurnResponse(practice.Turn{
		ID:             "turn-1",
		SessionID:      "session-1",
		QuestionID:     "question-1",
		AnswerText:     "answer",
		EffectiveTurns: 1,
	})
	if _, leaked := response["review_id"]; leaked {
		t.Fatalf("Turn response contains Review checkpoint: %#v", response)
	}
}
