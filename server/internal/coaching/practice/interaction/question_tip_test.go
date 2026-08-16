package interaction

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestPreparedIELTSAnswerUsesFrozenQuestionPosition(t *testing.T) {
	assignment := &practice.IELTSAssignment{Parts: []practice.IELTSPart{
		{TurnBlueprints: []string{"one", "two"}, PreparedAnswers: []practice.IELTSPreparedAnswer{{QuestionPosition: 2, Answer: "my frozen answer", Personalized: true}}},
		{TurnBlueprints: []string{"three"}},
	}}

	answer, ok := preparedIELTSAnswer(assignment, 2, "two")
	if !ok || answer != "my frozen answer" {
		t.Fatalf("preparedIELTSAnswer() = %q, %v", answer, ok)
	}
	if _, ok := preparedIELTSAnswer(assignment, 1, "one"); ok {
		t.Fatal("unprepared question unexpectedly returned an answer")
	}
	if _, ok := preparedIELTSAnswer(assignment, 2, "follow-up"); ok {
		t.Fatal("a follow-up at the same sequence reused another question's answer")
	}
}
