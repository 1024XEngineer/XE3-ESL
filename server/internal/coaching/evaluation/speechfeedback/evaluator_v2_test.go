package speechfeedback

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestCompactEvaluatorUsesOralReferenceWithoutChangingEvidence(t *testing.T) {
	generator := &recordingCompactGenerator{
		content: `{"items":[{"kind":"STRENGTH","explanation":"The spoken sentence is grammatically complete.","suggested_text":null}]}`,
	}
	evaluator, err := NewCompactEvaluator(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	if lineage.PipelineVersion != "speech-evaluation/v2" ||
		lineage.PromptVersion != "speech-feedback/v2" {
		t.Fatalf("lineage = %#v", lineage)
	}
	const transcript = "I called the lender. Because the air conditioner is leaking. And I need someone to repair it tomorrow."
	_, items, err := evaluator.EvaluateAgentMessage(
		context.Background(),
		evaluation.SpeechInputSnapshot{
			SchemaVersion: evaluation.SpeechInputSchemaVersion,
			Transcript:    transcript,
			EvidenceRefID: "30000000-0000-4000-8000-000000000003",
			Acoustic: &evaluation.AcousticCheckpoint{
				Status: evaluation.AcousticNotAssessed,
				Reason: "AGENT_MESSAGE_ACOUSTICS_NOT_ASSESSED",
			},
		},
		lineage,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Category != string(SpeechFeedbackItemStrength) ||
		items[0].Evidence.OriginalExcerpt != transcript {
		t.Fatalf("feedback items = %#v", items)
	}
	var prompt struct {
		Kind        evaluation.Kind `json:"kind"`
		EnglishText string          `json:"english_text"`
	}
	if err := json.Unmarshal([]byte(generator.request.UserPrompt), &prompt); err != nil {
		t.Fatal(err)
	}
	const want = "I called the lender because the air conditioner is leaking, and I need someone to repair it tomorrow"
	if prompt.Kind != evaluation.KindAgentMessageFeedback || prompt.EnglishText != want {
		t.Fatalf("generation prompt = %#v, want english_text %q", prompt, want)
	}
}

func TestCompactEvaluatorKeepsStandaloneFragmentAssessable(t *testing.T) {
	generator := &recordingCompactGenerator{
		content: `{"items":[{"kind":"CORRECTION","explanation":"Complete the subordinate clause with a main clause.","suggested_text":"I called because the air conditioner is leaking."}]}`,
	}
	evaluator, err := NewCompactEvaluator(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	const fragment = "Because the air conditioner is leaking."
	_, items, err := evaluator.EvaluateAgentMessage(
		context.Background(),
		evaluation.SpeechInputSnapshot{
			SchemaVersion: evaluation.SpeechInputSchemaVersion,
			Transcript:    fragment,
			EvidenceRefID: "30000000-0000-4000-8000-000000000004",
			Acoustic: &evaluation.AcousticCheckpoint{
				Status: evaluation.AcousticNotAssessed,
				Reason: "AGENT_MESSAGE_ACOUSTICS_NOT_ASSESSED",
			},
		},
		lineage,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Category != string(SpeechFeedbackItemCorrection) ||
		items[0].Evidence.OriginalExcerpt != fragment {
		t.Fatalf("feedback items = %#v", items)
	}
	var prompt struct {
		EnglishText string `json:"english_text"`
	}
	if err := json.Unmarshal([]byte(generator.request.UserPrompt), &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.EnglishText != "Because the air conditioner is leaking" {
		t.Fatalf("generation prompt english_text = %q", prompt.EnglishText)
	}
}

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

type recordingCompactGenerator struct {
	request TextGenerationRequest
	content string
}

func (generator *recordingCompactGenerator) Generate(
	_ context.Context,
	request TextGenerationRequest,
) (TextGenerationResult, error) {
	generator.request = request
	return TextGenerationResult{
		RequestID: "request-oral-reference",
		Provider:  "qianwen",
		Model:     "qwen-plus",
		Content:   generator.content,
	}, nil
}

var _ TextGenerator = (*recordingCompactGenerator)(nil)
