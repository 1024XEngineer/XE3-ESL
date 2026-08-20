package speechfeedback

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestCompactEvaluatorUsesOralReferenceWithoutChangingEvidence(t *testing.T) {
	generator := &recordingCompactGenerator{
		content: `{"items":[{"kind":"STRENGTH","explanation":"表达完整，无需修改。","source_text":null,"source_occurrence":null,"suggested_text":null}]}`,
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
		content: `{"items":[{"kind":"CORRECTION","explanation":"从句缺少主句。","source_text":"Because the air conditioner is leaking","source_occurrence":1,"suggested_text":"The air conditioner is leaking"}]}`,
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
		items[0].Evidence.OriginalExcerpt != "Because the air conditioner is leaking" {
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
		Content: `{"items":[{"kind":"CORRECTION","explanation":"主语后应使用正确的动词形式。",` +
			`"source_text":"has","source_occurrence":1,"suggested_text":"have"}]}`,
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

func TestCompactFeedbackItemsEnforcesClassificationContract(t *testing.T) {
	t.Parallel()

	snapshot := evaluation.SpeechInputSnapshot{
		Transcript:    "I have a plan, and I can start today.",
		EvidenceRefID: "30000000-0000-4000-8000-000000000001",
	}

	t.Run("correct answer remains no change", func(t *testing.T) {
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"STRENGTH","explanation":"表达自然，无需修改。","source_text":null,"source_occurrence":null,"suggested_text":null}]}`),
			snapshot,
			snapshot.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Category != "STRENGTH" ||
			items[0].Correction != "" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("punctuation and case only correction safely becomes no change", func(t *testing.T) {
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"只有大小写或标点差异，不属于语言错误。","source_text":"I have a plan,","source_occurrence":1,"suggested_text":"i have a plan."}]}`),
			snapshot,
			snapshot.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Category != "STRENGTH" {
			t.Fatalf("items = %#v", items)
		}
	})

	for _, connector := range []struct {
		name       string
		transcript string
		source     string
		suggested  string
		want       string
	}{
		{
			name:       "and to so",
			transcript: "It is leaking, and I need help.",
			source:     "and", suggested: "so",
			want: "It is leaking, so I need help.",
		},
		{
			name:       "so to and",
			transcript: "It is leaking, so I need help.",
			source:     "so", suggested: "and",
			want: "It is leaking, and I need help.",
		},
	} {
		connector := connector
		t.Run(connector.name+" is optional", func(t *testing.T) {
			current := snapshot
			current.Transcript = connector.transcript
			items, err := compactFeedbackItems(
				compactResult(`{"items":[{"kind":"CORRECTION","explanation":"连接方式属于可选表达。","source_text":"`+
					connector.source+`","source_occurrence":1,"suggested_text":"`+connector.suggested+`"}]}`),
				current,
				current.Transcript,
				"SAME_QUESTION",
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 ||
				items[0].Category != "RECOMMENDED_EXPRESSION" ||
				items[0].Correction != connector.want {
				t.Fatalf("items = %#v", items)
			}
		})
	}

	t.Run("real grammar error stays a localized correction", func(t *testing.T) {
		current := snapshot
		current.Transcript = "I has a plan."
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"主语后应使用正确的动词形式。","source_text":"has","source_occurrence":1,"suggested_text":"have"}]}`),
			current,
			current.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Category != "CORRECTION" ||
			items[0].Evidence.OriginalExcerpt != "has" ||
			items[0].Evidence.StartUTF8Byte != 2 ||
			items[0].Correction != "have" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("repeated evidence uses the declared occurrence", func(t *testing.T) {
		current := snapshot
		current.Transcript = "He has a plan, but I has none."
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"第二处动词形式与主语不一致。","source_text":"has","source_occurrence":2,"suggested_text":"have"}]}`),
			current,
			current.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Evidence.StartUTF8Byte != 21 {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("projected source maps back to mixed transcript bytes", func(t *testing.T) {
		current := snapshot
		current.Transcript = "中文 I  has a plan."
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"主语后动词形式不正确。","source_text":"has","source_occurrence":1,"suggested_text":"have"}]}`),
			current,
			speechFeedbackEnglishReferenceText(current.Transcript),
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Evidence.StartUTF8Byte != 10 ||
			items[0].Evidence.EndUTF8Byte != 13 ||
			items[0].Evidence.OriginalExcerpt != "has" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("projected source spans collapsed original whitespace", func(t *testing.T) {
		current := snapshot
		current.Transcript = "I  has a plan."
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"主语后动词形式不正确。","source_text":"I has","source_occurrence":1,"suggested_text":"I have"}]}`),
			current,
			speechFeedbackEnglishReferenceText(current.Transcript),
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Evidence.StartUTF8Byte != 0 ||
			items[0].Evidence.EndUTF8Byte != 6 ||
			items[0].Evidence.OriginalExcerpt != "I  has" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("missing required structured field is rejected", func(t *testing.T) {
		_, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"动词形式需要修改。","source_text":"have","suggested_text":"had"}]}`),
			snapshot,
			snapshot.Transcript,
			"SAME_QUESTION",
		)
		if err == nil {
			t.Fatal("provider item without source_occurrence was accepted")
		}
	})

	t.Run("explanation mentioning absent English words is repaired", func(t *testing.T) {
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"RECOMMENDED_EXPRESSION","explanation":"原输入中的 fix 和 service 可以更自然。","source_text":"I have a plan","source_occurrence":1,"suggested_text":"I already have a plan"}]}`),
			snapshot,
			snapshot.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 ||
			items[0].Category != "RECOMMENDED_EXPRESSION" ||
			items[0].Recommendation != "这是保留原意的可选表达。" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("correction explanation cannot introduce the replacement word", func(t *testing.T) {
		current := snapshot
		current.Transcript = "I has a plan."
		items, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"CORRECTION","explanation":"将 has 改为 have。","source_text":"has","source_occurrence":1,"suggested_text":"have"}]}`),
			current,
			current.Transcript,
			"SAME_QUESTION",
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Category != "CORRECTION" ||
			items[0].Recommendation != "此处存在可定位的语言错误，建议使用右侧表达。" {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("no change conflicts with recommendations", func(t *testing.T) {
		_, err := compactFeedbackItems(
			compactResult(`{"items":[{"kind":"STRENGTH","explanation":"无需修改。","source_text":null,"source_occurrence":null,"suggested_text":null},{"kind":"RECOMMENDED_EXPRESSION","explanation":"这是可选表达。","source_text":"I have a plan","source_occurrence":1,"suggested_text":"I already have a plan"}]}`),
			snapshot,
			snapshot.Transcript,
			"SAME_QUESTION",
		)
		if err == nil {
			t.Fatal("conflicting provider output was accepted")
		}
	})
}

func compactResult(content string) TextGenerationResult {
	return TextGenerationResult{
		RequestID: "request-1",
		Provider:  "qianwen",
		Model:     "qwen-plus",
		Content:   content,
	}
}
