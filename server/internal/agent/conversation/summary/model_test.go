package summary

import (
	"strings"
	"testing"
)

func TestContentValidation(t *testing.T) {
	t.Parallel()
	valid := validContent()
	if !valid.Valid() {
		t.Fatal("valid content rejected")
	}
	valid.CurrentIntents = []string{strings.Repeat("界", MaxItemRunes+1)}
	if valid.Valid() {
		t.Fatal("oversized item accepted")
	}
	blank := validContent()
	blank.CurrentIntents = []string{" "}
	if blank.Valid() {
		t.Fatal("blank item accepted")
	}
}

func TestStateValidation(t *testing.T) {
	t.Parallel()
	state := State{
		OwnerID:         "10000000-0000-4000-8000-000000000001",
		ThreadID:        "20000000-0000-4000-8000-000000000001",
		ThroughSequence: 4,
		Content:         validContent(),
	}
	if !state.Valid() {
		t.Fatal("valid state rejected")
	}
	state.ThroughSequence = 0
	if state.Valid() {
		t.Fatal("state without coverage accepted")
	}
}

func validContent() Content {
	return Content{
		CurrentIntents: []string{"Prepare for the interview"},
		Background:     []string{}, Progress: []string{}, Decisions: []string{},
		OpenQuestions: []string{}, NextSteps: []string{},
	}
}
