package evaluation

import (
	"encoding/json"
	"testing"
)

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
			Model:     "qwen-plus",
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
