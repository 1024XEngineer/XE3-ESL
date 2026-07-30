package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	maxSpeechFeedbackProviderPayload = 64 * 1024
	maxSpeechFeedbackProviderItems   = 8
)

type SpeechFeedbackProvider interface {
	GenerateSpeechFeedback(
		context.Context,
		SpeechFeedbackProviderInput,
	) (SpeechFeedbackProviderResult, error)
}

type SpeechFeedbackProviderInput struct {
	SchemaVersion string               `json:"schema_version"`
	PromptVersion string               `json:"prompt_version"`
	Source        SpeechFeedbackSource `json:"source"`
	EvidenceRefID string               `json:"evidence_ref_id,omitempty"`
	ConfirmedText string               `json:"confirmed_text"`
}

func (input SpeechFeedbackProviderInput) valid() bool {
	return input.SchemaVersion == SpeechFeedbackSchemaVersion &&
		input.PromptVersion == SpeechFeedbackPromptVersion &&
		input.Source.valid() &&
		validSpeechFeedbackText(input.ConfirmedText, 16*1024) &&
		((input.Source.SourceKind ==
			SpeechFeedbackSourceConversationTurn &&
			validSpeechFeedbackIdentifier(input.EvidenceRefID)) ||
			(input.Source.SourceKind ==
				SpeechFeedbackSourceAgentVoiceMessage &&
				input.EvidenceRefID == ""))
}

type SpeechFeedbackProviderResult struct {
	Payload   json.RawMessage
	Provider  string
	Model     string
	RequestID string
}

type TextSpeechFeedbackProvider struct {
	generator ai.TextGenerator
}

func NewSpeechFeedbackTextProvider(
	generator ai.TextGenerator,
) (*TextSpeechFeedbackProvider, error) {
	if generator == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &TextSpeechFeedbackProvider{generator: generator}, nil
}

func (provider *TextSpeechFeedbackProvider) GenerateSpeechFeedback(
	ctx context.Context,
	input SpeechFeedbackProviderInput,
) (SpeechFeedbackProviderResult, error) {
	if provider == nil || provider.generator == nil ||
		ctx == nil || !input.valid() {
		return SpeechFeedbackProviderResult{},
			ErrInvalidSpeechFeedback
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return SpeechFeedbackProviderResult{},
			ErrInvalidSpeechFeedback
	}
	result, err := provider.generator.Generate(ctx, ai.TextRequest{
		Messages: []ai.TextMessage{
			{
				Role:    ai.TextRoleSystem,
				Content: speechFeedbackSystemPrompt,
			},
			{
				Role:    ai.TextRoleUser,
				Content: string(payload),
			},
		},
		ResponseFormat: ai.TextResponseFormatJSON,
	})
	if err != nil {
		return SpeechFeedbackProviderResult{}, err
	}
	if !validSpeechFeedbackIdentifier(result.Provider) ||
		!validSpeechFeedbackIdentifier(result.Model) ||
		!validSpeechFeedbackIdentifier(result.ID) ||
		len(result.Content) == 0 ||
		len(result.Content) > maxSpeechFeedbackProviderPayload {
		return SpeechFeedbackProviderResult{},
			ErrInvalidSpeechFeedback
	}
	return SpeechFeedbackProviderResult{
		Payload:   json.RawMessage(result.Content),
		Provider:  result.Provider,
		Model:     result.Model,
		RequestID: result.ID,
	}, nil
}

const speechFeedbackSystemPrompt = `You return cautious English-learning feedback for one confirmed transcript.
Return one JSON object only: {"items":[...]}.
Each item has exactly: kind, anchor, explanation, and suggested_text when required.
kind is CORRECTION, STRENGTH, IMPROVEMENT, or RECOMMENDED_EXPRESSION.
anchor must copy the complete source-specific anchor identity from the input and include exact UTF-8 byte offsets and original_excerpt.
CORRECTION, IMPROVEMENT, and RECOMMENDED_EXPRESSION require non-empty suggested_text. STRENGTH must omit suggested_text.
Use only the confirmed text. Do not score, grade, infer pronunciation, fluency, confidence, audio quality, intent, or facts not present in the transcript.
Return at most 8 anchored items and no unknown fields.`

type SpeechFeedbackDraftItem struct {
	Kind           SpeechFeedbackItemKind
	Anchor         SpeechFeedbackAnchor
	Explanation    string
	SuggestedText  *string
	RepracticeMode SpeechFeedbackRepracticeMode
}

func (item SpeechFeedbackDraftItem) validFor(
	source SpeechFeedbackSource,
	evidenceRefID string,
	canonicalText string,
) bool {
	if !item.Kind.valid() ||
		!item.Anchor.validFor(source, evidenceRefID, canonicalText) ||
		!validSpeechFeedbackAdviceText(item.Explanation) {
		return false
	}
	switch item.Kind {
	case SpeechFeedbackItemStrength:
		return item.SuggestedText == nil &&
			item.RepracticeMode == SpeechFeedbackRepracticeNone
	default:
		if item.SuggestedText == nil ||
			!validSpeechFeedbackAdviceText(*item.SuggestedText) {
			return false
		}
		switch source.SourceKind {
		case SpeechFeedbackSourceConversationTurn:
			return item.RepracticeMode ==
				SpeechFeedbackRepracticeSameQuestion
		case SpeechFeedbackSourceAgentVoiceMessage:
			return item.RepracticeMode ==
				SpeechFeedbackRepracticeSameThread
		default:
			return false
		}
	}
}

func normalizeSpeechFeedbackProviderResult(
	input SpeechFeedbackProviderInput,
	result SpeechFeedbackProviderResult,
) ([]SpeechFeedbackDraftItem, error) {
	if !input.valid() ||
		len(result.Payload) == 0 ||
		len(result.Payload) > maxSpeechFeedbackProviderPayload ||
		!validSpeechFeedbackIdentifier(result.Provider) ||
		!validSpeechFeedbackIdentifier(result.Model) ||
		!validSpeechFeedbackIdentifier(result.RequestID) {
		return nil, ErrInvalidSpeechFeedback
	}
	type providerItem struct {
		Kind          SpeechFeedbackItemKind `json:"kind"`
		Anchor        SpeechFeedbackAnchor   `json:"anchor"`
		Explanation   string                 `json:"explanation"`
		SuggestedText *string                `json:"suggested_text,omitempty"`
	}
	type providerEnvelope struct {
		Items []providerItem `json:"items"`
	}
	var envelope providerEnvelope
	decoder := json.NewDecoder(bytes.NewReader(result.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.Join(
			ErrInvalidSpeechFeedback,
			fmt.Errorf("decode provider response: %w", err),
		)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, ErrInvalidSpeechFeedback
	}
	if len(envelope.Items) == 0 ||
		len(envelope.Items) > maxSpeechFeedbackProviderItems {
		return nil, ErrInvalidSpeechFeedback
	}
	normalized := make(
		[]SpeechFeedbackDraftItem,
		0,
		len(envelope.Items),
	)
	seen := make(map[string]struct{}, len(envelope.Items))
	for _, generated := range envelope.Items {
		repractice := SpeechFeedbackRepracticeNone
		if generated.Kind != SpeechFeedbackItemStrength {
			switch input.Source.SourceKind {
			case SpeechFeedbackSourceConversationTurn:
				repractice =
					SpeechFeedbackRepracticeSameQuestion
			case SpeechFeedbackSourceAgentVoiceMessage:
				repractice =
					SpeechFeedbackRepracticeSameThread
			default:
				return nil, ErrInvalidSpeechFeedback
			}
		}
		item := SpeechFeedbackDraftItem{
			Kind:           generated.Kind,
			Anchor:         generated.Anchor,
			Explanation:    generated.Explanation,
			SuggestedText:  generated.SuggestedText,
			RepracticeMode: repractice,
		}
		if !item.validFor(
			input.Source,
			input.EvidenceRefID,
			input.ConfirmedText,
		) {
			return nil, ErrInvalidSpeechFeedback
		}
		key := fmt.Sprintf(
			"%s\x00%s\x00%d\x00%d",
			item.Kind,
			item.Anchor.AnchorKind,
			item.Anchor.StartUTF8Byte,
			item.Anchor.EndUTF8Byte,
		)
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidSpeechFeedback
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

var _ SpeechFeedbackProvider = (*TextSpeechFeedbackProvider)(nil)
