package voice

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSessionStartReturnsQuestionFromFrozenSession(t *testing.T) {
	session := sessionFixture()
	candidate := roundCandidate()
	orchestrator, err := NewRoundOrchestrator(
		&roundConversation{candidate: candidate, turn: roundTurn(candidate)},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	application, err := NewSessionApplication(
		sessionPortStub{session: session},
		questionPortStub{},
		checkpointPortStub{},
		orchestrator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Start(
		context.Background(),
		roundActor(),
		session.ID,
		"start-1",
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Session.ID != session.ID || state.Question == nil ||
		state.Question.SessionID != session.ID {
		t.Fatalf("Session state = %#v", state)
	}
}

type sessionPortStub struct{ session Session }

func (p sessionPortStub) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, error) {
	return p.session, nil
}

func (p sessionPortStub) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return p.session, nil
}

type questionPortStub struct{}

func (questionPortStub) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	sequence int,
) (practice.Question, error) {
	return practice.Question{
		ID:                      "question-1",
		SessionID:               session.ID,
		SpeakerParticipantID:    session.FacilitatorParticipantID,
		AddresseeParticipantIDs: []string{session.LearnerParticipantID},
		ObjectiveID:             "objective-1",
		Type:                    "PRIMARY",
		Content:                 "Tell me about yourself.",
		Sequence:                sequence,
	}, nil
}

func (questionPortStub) GetQuestion(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Question, error) {
	return practice.Question{ID: "question-1", Content: "Question"}, nil
}

type checkpointPortStub struct{}

func (checkpointPortStub) LatestTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Turn, bool, error) {
	return practice.Turn{}, false, nil
}

func (checkpointPortStub) ListTurnHistory(
	context.Context,
	requestcontext.Actor,
	string,
) ([]TurnExchange, error) {
	return nil, nil
}

func sessionFixture() Session {
	return Session{
		ID:           "session-1",
		PlanID:       "plan-1",
		SceneID:      "scene-1",
		SceneVersion: 1,
		SceneFamily:  "INTERVIEW",
		SceneModel:   "INTERVIEW_STANDARD",
		Prompt: scene.ScenePrompt{
			PublicSceneBrief: "Interview practice",
			PracticeGoal:     "Answer clearly",
			UserRole:         "Candidate",
			AIRole:           "Interviewer",
			PersonaSummary:   "Professional interviewer",
			FocusAreas:       []string{"clarity"},
			TurnBlueprints:   []string{"Ask for an introduction"},
		},
		SessionVersion:           1,
		TurnLimit:                3,
		MaxFollowUpsPerQuestion:  1,
		Status:                   "in_progress",
		FacilitatorParticipantID: "facilitator-1",
		LearnerParticipantID:     "learner-1",
	}
}
