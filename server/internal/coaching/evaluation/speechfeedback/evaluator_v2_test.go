package speechfeedback

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestCompactEvaluatorAssignsSupportedRepracticeModeBySource(t *testing.T) {
	evaluator, err := NewCompactEvaluator(compactGeneratorFake{})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		evaluate func() ([]evaluation.FeedbackItemDraft, error)
		wantMode string
	}{
		{
			name: "practice turn",
			evaluate: func() ([]evaluation.FeedbackItemDraft, error) {
				_, items, err := evaluator.EvaluatePracticeTurn(
					context.Background(),
					evaluation.SpeechInputSnapshot{
						SchemaVersion: evaluation.SpeechInputSchemaVersion,
						Transcript:    "I has a plan.",
						EvidenceRefID: "30000000-0000-4000-8000-000000000001",
						QuestionID:    "40000000-0000-4000-8000-000000000001",
						Acoustic: &evaluation.AcousticCheckpoint{
							Status: evaluation.AcousticNotAssessed,
							Reason: "PRACTICE_TURN_AUDIO_UNAVAILABLE",
						},
					},
					lineage,
				)
				return items, err
			},
			wantMode: "SAME_QUESTION",
		},
		{
			name: "agent message",
			evaluate: func() ([]evaluation.FeedbackItemDraft, error) {
				_, items, err := evaluator.EvaluateAgentMessage(
					context.Background(),
					evaluation.SpeechInputSnapshot{
						SchemaVersion: evaluation.SpeechInputSchemaVersion,
						Transcript:    "I has a plan.",
						EvidenceRefID: "30000000-0000-4000-8000-000000000002",
						Acoustic: &evaluation.AcousticCheckpoint{
							Status: evaluation.AcousticNotAssessed,
							Reason: "AGENT_MESSAGE_ACOUSTICS_NOT_ASSESSED",
						},
					},
					lineage,
				)
				return items, err
			},
			wantMode: "NONE",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := test.evaluate()
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].RepracticeMode != test.wantMode {
				t.Fatalf("feedback items = %#v, want mode %s", items, test.wantMode)
			}
			unsupported := items[0]
			unsupported.RepracticeMode = "SAME_THREAD"
			if unsupported.Valid() {
				t.Fatal("FeedbackItemDraft accepted retired SAME_THREAD mode")
			}
		})
	}
}

type compactGeneratorFake struct{}

func (compactGeneratorFake) Generate(
	context.Context,
	TextGenerationRequest,
) (TextGenerationResult, error) {
	return TextGenerationResult{
		RequestID: "request-1",
		Provider:  "qianwen",
		Model:     "qwen-plus",
		Content: `{"items":[{"kind":"CORRECTION","explanation":"Use the correct verb form.",` +
			`"suggested_text":"I have a plan."}]}`,
	}, nil
}

var _ TextGenerator = compactGeneratorFake{}
