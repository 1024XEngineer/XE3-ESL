package review

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSpeechFeedbackProviderResultRequiresExactAnchors(
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
			"kind": "CORRECTION",
			"anchor": map[string]any{
				"anchor_kind":            "AGENT_TRANSCRIPT",
				"transcript_evidence_id": input.Source.TranscriptEvidenceID,
				"message_id":             input.Source.MessageID,
				"start_utf8_byte":        0,
				"end_utf8_byte":          len(input.ConfirmedText),
				"original_excerpt":       input.ConfirmedText,
			},
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
			SpeechFeedbackRepracticeSameThread {
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

func TestNormalizeSpeechFeedbackProviderResultRejectsUTF8RuneSplit(
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
			EvidenceSnapshotID: "review_snapshot_1",
		},
		EvidenceRefID: "review_evidence_1",
		ConfirmedText: "你好 English",
	}
	encoded := json.RawMessage(`{
		"items": [{
			"kind": "IMPROVEMENT",
			"anchor": {
				"anchor_kind": "CONVERSATION_TRANSCRIPT",
				"evidence_ref_id": "review_evidence_1",
				"turn_id": "turn-1",
				"start_utf8_byte": 1,
				"end_utf8_byte": 6,
				"original_excerpt": "好"
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
		t.Fatal("UTF-8 rune-splitting anchor was accepted")
	}
}

func TestNormalizeSpeechFeedbackProviderResultRejectsDifferentFrozenEvidence(
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
			EvidenceSnapshotID: "review_snapshot_1",
		},
		EvidenceRefID: "review_evidence_1",
		ConfirmedText: "I work on this project.",
	}
	encoded := json.RawMessage(`{
		"items": [{
			"kind": "STRENGTH",
			"anchor": {
				"anchor_kind": "CONVERSATION_TRANSCRIPT",
				"evidence_ref_id": "different_review_evidence",
				"turn_id": "turn-1",
				"start_utf8_byte": 0,
				"end_utf8_byte": 1,
				"original_excerpt": "I"
			},
			"explanation": "The answer starts directly."
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
		t.Fatal("anchor for a different frozen evidence snapshot was accepted")
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
		start := index * 4
		items[index] = map[string]any{
			"kind": "STRENGTH",
			"anchor": map[string]any{
				"anchor_kind":            "AGENT_TRANSCRIPT",
				"transcript_evidence_id": input.Source.TranscriptEvidenceID,
				"message_id":             input.Source.MessageID,
				"start_utf8_byte":        start,
				"end_utf8_byte":          start + 3,
				"original_excerpt":       input.ConfirmedText[start : start+3],
			},
			"explanation": "Clear word choice.",
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
