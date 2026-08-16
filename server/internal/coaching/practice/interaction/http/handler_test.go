package voicehttp

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
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
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{
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

func TestSessionStateResponseIncludesFrozenIELTSAssignment(t *testing.T) {
	assignment := &practice.IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   practice.PracticeModePart3,
		Parts: []practice.IELTSPart{{
			Part:           practice.PracticeModePart3,
			SourceID:       "topic-group-1",
			TopicTitle:     "Technology",
			TurnBlueprints: []string{"Part 3 question: Why does it matter?"},
		}},
	}
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{IELTSAssignment: assignment},
	})
	if response["ielts_assignment"] != assignment {
		t.Fatalf("response = %#v", response)
	}
}

func TestSessionStateResponseMarksEndedEarlySessionTerminal(t *testing.T) {
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{
			ID:             "session-1",
			PlanID:         "plan-1",
			SceneID:        "scene-1",
			SceneVersion:   1,
			SessionVersion: 2,
			TurnLimit:      3,
			Status:         string(practice.SessionEndedEarly),
		},
	})

	if response["session_completed"] != true {
		t.Fatalf("session_completed = %#v, want true", response["session_completed"])
	}
}
