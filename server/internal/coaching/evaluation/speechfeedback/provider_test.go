package speechfeedback

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTextSpeechFeedbackProviderProjectsMixedInputBeforeGeneration(
	t *testing.T,
) {
	t.Parallel()
	generator := &capturingSpeechFeedbackTextGenerator{
		result: TextGenerationResult{
			RequestID: "request-mixed-projection",
			Content: `{"items":[{"kind":"RECOMMENDED_EXPRESSION",` +
				`"explanation":"Use a natural introduction.",` +
				`"suggested_text":"Hello, my name is Nai Long."}]}`,
			Provider: "qianwen",
			Model:    "moonshotai/kimi-k2.6",
		},
	}
	provider, err := NewSpeechFeedbackTextProvider(generator)
	if err != nil {
		t.Fatal(err)
	}
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
			ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
			MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
			TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
			CandidateVersion:     1,
		},
		ConfirmedText: "你好，Hello, my name is Nai Long. Can you help me?",
	}
	if _, err := provider.GenerateSpeechFeedback(context.Background(), input); err != nil {
		t.Fatalf("generate mixed feedback: %v", err)
	}
	if strings.Contains(generator.request.UserPrompt, "你好") {
		t.Fatalf("provider received Chinese text: %s", generator.request.UserPrompt)
	}
	var prompt struct {
		SourceKind  SpeechFeedbackSourceKind `json:"source_kind"`
		EnglishText string                   `json:"english_text"`
	}
	if err := json.Unmarshal([]byte(generator.request.UserPrompt), &prompt); err != nil {
		t.Fatalf("decode provider prompt: %v", err)
	}
	if prompt.SourceKind != SpeechFeedbackSourceAgentVoiceMessage ||
		prompt.EnglishText !=
			"Hello, my name is Nai Long. Can you help me" {
		t.Fatalf("provider prompt = %#v", prompt)
	}
}

type capturingSpeechFeedbackTextGenerator struct {
	request TextGenerationRequest
	result  TextGenerationResult
}

func (generator *capturingSpeechFeedbackTextGenerator) Generate(
	_ context.Context,
	request TextGenerationRequest,
) (TextGenerationResult, error) {
	generator.request = request
	return generator.result, nil
}

func TestNormalizeSpeechFeedbackProviderResultBuildsAgentAnchor(
	t *testing.T,
) {
	t.Parallel()
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
			ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
			MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
			TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
			CandidateVersion:     1,
		},
		ConfirmedText: "I work this project.",
	}
	payload := map[string]any{
		"items": []any{map[string]any{
			"kind":           "CORRECTION",
			"explanation":    "Use a preposition with this verb.",
			"suggested_text": "I work on this project.",
		}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	items, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload:   encoded,
			Provider:  "qianwen",
			Model:     "moonshotai/kimi-k2.6",
			RequestID: "request-1",
		},
	)
	if err != nil {
		t.Fatalf("normalize valid provider result: %v", err)
	}
	if len(items) != 1 ||
		items[0].RepracticeMode !=
			SpeechFeedbackRepracticeSameThread ||
		items[0].Anchor.AnchorKind !=
			SpeechFeedbackAnchorAgentTranscript ||
		items[0].Anchor.TranscriptEvidenceID !=
			input.Source.TranscriptEvidenceID ||
		items[0].Anchor.MessageID != input.Source.MessageID ||
		items[0].Anchor.StartUTF8Byte != 0 ||
		items[0].Anchor.EndUTF8Byte != len(input.ConfirmedText) ||
		items[0].Anchor.OriginalExcerpt != input.ConfirmedText {
		t.Fatalf("normalized items = %#v", items)
	}

	payload["score"] = 92
	encoded, _ = json.Marshal(payload)
	if _, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload:   encoded,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	); err == nil {
		t.Fatal("provider score field was accepted")
	}
}

func TestNormalizeSpeechFeedbackProviderResultRejectsProviderAnchor(
	t *testing.T,
) {
	t.Parallel()
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:         SpeechFeedbackSourceConversationTurn,
			PracticeSessionID:  "practice-1",
			TurnID:             "turn-1",
			InputRevision:      1,
			EvidenceSnapshotID: "evaluation_snapshot_1",
		},
		EvidenceRefID: "evaluation_evidence_1",
		ConfirmedText: "你好 English",
	}
	encoded := json.RawMessage(`{
		"items": [{
			"kind": "IMPROVEMENT",
			"anchor": {
				"anchor_kind": "CONVERSATION_TRANSCRIPT",
				"evidence_ref_id": "evaluation_evidence_1",
				"turn_id": "turn-1",
				"start_utf8_byte": 0,
				"end_utf8_byte": 14,
				"original_excerpt": "你好 English"
			},
			"explanation": "Add more context.",
			"suggested_text": "你好, could you help me?"
		}]
	}`)
	if _, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload:   encoded,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	); err == nil {
		t.Fatal("provider-controlled anchor was accepted")
	}
}

func TestNormalizeSpeechFeedbackProviderResultBuildsConversationAnchor(
	t *testing.T,
) {
	t.Parallel()
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:         SpeechFeedbackSourceConversationTurn,
			PracticeSessionID:  "practice-1",
			TurnID:             "turn-1",
			InputRevision:      1,
			EvidenceSnapshotID: "evaluation_snapshot_1",
		},
		EvidenceRefID: "evaluation_evidence_1",
		ConfirmedText: "I work on this project.",
	}
	encoded := json.RawMessage(`{
		"items": [{
			"kind": "STRENGTH",
			"explanation": "The answer starts directly."
		}]
	}`)
	items, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload:   encoded,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	)
	if err != nil {
		t.Fatalf("normalize provider result: %v", err)
	}
	if len(items) != 1 ||
		items[0].Anchor.AnchorKind !=
			SpeechFeedbackAnchorConversationTranscript ||
		items[0].Anchor.EvidenceRefID != input.EvidenceRefID ||
		items[0].Anchor.TurnID != input.Source.TurnID ||
		items[0].Anchor.StartUTF8Byte != 0 ||
		items[0].Anchor.EndUTF8Byte != len(input.ConfirmedText) ||
		items[0].Anchor.OriginalExcerpt != input.ConfirmedText {
		t.Fatalf("normalized items = %#v", items)
	}
}

func TestNormalizeSpeechFeedbackProviderResultRejectsUnchangedSuggestion(
	t *testing.T,
) {
	t.Parallel()
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:         SpeechFeedbackSourceConversationTurn,
			PracticeSessionID:  "practice-1",
			TurnID:             "turn-1",
			InputRevision:      1,
			EvidenceSnapshotID: "evaluation_snapshot_1",
		},
		EvidenceRefID: "evaluation_evidence_1",
		ConfirmedText: "This expression is already natural.",
	}
	if _, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload: json.RawMessage(`{
				"items": [{
					"kind": "RECOMMENDED_EXPRESSION",
					"explanation": "This is already natural.",
					"suggested_text": " this expression is already natural. "
				}]
			}`),
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	); err == nil {
		t.Fatal("unchanged recommendation was accepted")
	}
}

func TestNormalizeSpeechFeedbackProviderResultCapsItemsAtEight(
	t *testing.T,
) {
	t.Parallel()
	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: SpeechFeedbackPromptVersion,
		Source: SpeechFeedbackSource{
			SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
			ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
			MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
			TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
			CandidateVersion:     1,
		},
		ConfirmedText: "one two three four five six seven eight nine",
	}
	items := make([]any, 9)
	for index := range items {
		items[index] = map[string]any{
			"kind":        "STRENGTH",
			"explanation": "Clear word choice " + string(rune('A'+index)) + ".",
		}
	}
	encoded, _ := json.Marshal(map[string]any{"items": items})
	if _, err := normalizeSpeechFeedbackProviderResult(
		input,
		SpeechFeedbackProviderResult{
			Payload:   encoded,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	); err == nil {
		t.Fatal("nine provider items were accepted")
	}
}
