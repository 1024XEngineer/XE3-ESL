package service

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestFreezeIELTSPreparedAnswersRequiresExactFrozenQuestion(t *testing.T) {
	assignment := &preparation.IELTSAssignmentSnapshot{
		BankID: "bank_1",
		Parts: []preparation.IELTSAssignmentPartSnapshot{{
			Part: scene.PracticeModePart1, SourceID: "topic_1",
			TurnBlueprints: []string{"question one"},
		}},
	}
	request := preparation.IELTSPreparedAnswerRequest{
		BankID: "bank_1", Part: scene.PracticeModePart1, SourceID: "topic_1",
		QuestionPosition: 1, Answer: "I listen to jazz on my commute.", Personalized: true,
	}
	if err := freezeIELTSPreparedAnswers(assignment, []preparation.IELTSPreparedAnswerRequest{request}); err != nil {
		t.Fatalf("freeze prepared answer: %v", err)
	}
	if got := assignment.Parts[0].PreparedAnswers; len(got) != 1 || got[0].Answer != request.Answer {
		t.Fatalf("frozen answers = %#v", got)
	}
	request.QuestionPosition = 2
	if err := freezeIELTSPreparedAnswers(assignment, []preparation.IELTSPreparedAnswerRequest{request}); err == nil {
		t.Fatal("out-of-range prepared answer was accepted")
	}
}
