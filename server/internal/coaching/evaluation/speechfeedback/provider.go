package speechfeedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxSpeechFeedbackProviderPayload = 64 * 1024
	maxSpeechFeedbackProviderItems   = 2
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
	generator TextGenerator
}

func NewSpeechFeedbackTextProvider(
	generator TextGenerator,
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
	englishText := speechFeedbackEnglishReferenceText(input.ConfirmedText)
	if !validSpeechFeedbackText(englishText, 16*1024) {
		return SpeechFeedbackProviderResult{}, ErrInvalidSpeechFeedback
	}
	payload, err := json.Marshal(struct {
		SourceKind  SpeechFeedbackSourceKind `json:"source_kind"`
		EnglishText string                   `json:"english_text"`
	}{
		SourceKind:  input.Source.SourceKind,
		EnglishText: englishText,
	})
	if err != nil {
		return SpeechFeedbackProviderResult{},
			ErrInvalidSpeechFeedback
	}
	result, err := provider.generator.Generate(
		ctx,
		TextGenerationRequest{
			SystemPrompt: speechFeedbackSystemPrompt,
			UserPrompt:   string(payload),
		},
	)
	if err != nil {
		return SpeechFeedbackProviderResult{}, err
	}
	if !validSpeechFeedbackIdentifier(result.Provider) ||
		!validSpeechFeedbackModel(result.Model) ||
		!validSpeechFeedbackIdentifier(result.RequestID) ||
		len(result.Content) == 0 ||
		len(result.Content) > maxSpeechFeedbackProviderPayload {
		return SpeechFeedbackProviderResult{},
			ErrInvalidSpeechFeedback
	}
	return SpeechFeedbackProviderResult{
		Payload:   json.RawMessage(result.Content),
		Provider:  result.Provider,
		Model:     result.Model,
		RequestID: result.RequestID,
	}, nil
}

const speechFeedbackSystemPrompt = `You return cautious English-learning feedback for one confirmed transcript.
Return one JSON object only with this exact shape: {"items":[...]}.
The only allowed kinds are CORRECTION, RECOMMENDED_EXPRESSION, and STRENGTH.
The user payload contains english_text that was deterministically extracted from the confirmed transcript. Use only english_text. Never reconstruct, translate, or infer omitted non-English text.
Return CORRECTION if and only if english_text contains a clear grammar or word-choice error. It must contain exactly kind, explanation, and suggested_text. CORRECTION fixes the error and explains it briefly. Never label a merely less-natural expression as CORRECTION.
Return RECOMMENDED_EXPRESSION only when its complete suggested_text meaningfully improves naturalness or completeness, preserves the meaning, adds no new facts, and differs from english_text. It must contain exactly kind, explanation, and suggested_text. Do not rewrite merely to make the output different.
When neither correction nor meaningful improvement is needed, return exactly one STRENGTH with kind and explanation only, using this shape: {"items":[{"kind":"STRENGTH","explanation":"The expression is already natural."}]}.
Never combine STRENGTH with another item.
Do not score, grade, infer pronunciation, fluency, confidence, audio quality, intent, or facts not present in english_text.
Return at most 2 items and no unknown fields. Never omit suggested_text from CORRECTION or RECOMMENDED_EXPRESSION.`

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
		!validSpeechFeedbackModel(result.Model) ||
		!validSpeechFeedbackIdentifier(result.RequestID) {
		return nil, ErrInvalidSpeechFeedback
	}
	type providerItem struct {
		Kind          SpeechFeedbackItemKind `json:"kind"`
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
	anchor, err := speechFeedbackAnchorForInput(input)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(envelope.Items))
	englishText := speechFeedbackEnglishReferenceText(input.ConfirmedText)
	for _, generated := range envelope.Items {
		switch generated.Kind {
		case SpeechFeedbackItemCorrection,
			SpeechFeedbackItemRecommendedExpression:
			if generated.SuggestedText != nil &&
				equalSpeechFeedbackText(*generated.SuggestedText, englishText) {
				return nil, ErrInvalidSpeechFeedback
			}
		case SpeechFeedbackItemStrength:
			if len(envelope.Items) != 1 {
				return nil, ErrInvalidSpeechFeedback
			}
		default:
			return nil, ErrInvalidSpeechFeedback
		}
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
			Anchor:         anchor,
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
		suggestedText := ""
		if item.SuggestedText != nil {
			suggestedText = *item.SuggestedText
		}
		key := fmt.Sprintf(
			"%s\x00%s\x00%s",
			item.Kind,
			item.Explanation,
			suggestedText,
		)
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidSpeechFeedback
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func equalSpeechFeedbackText(left, right string) bool {
	const edgeCharacters = " \t\r\n,;:!?-."
	return strings.EqualFold(
		strings.Join(strings.Fields(strings.Trim(left, edgeCharacters)), " "),
		strings.Join(strings.Fields(strings.Trim(right, edgeCharacters)), " "),
	)
}

func speechFeedbackAnchorForInput(
	input SpeechFeedbackProviderInput,
) (SpeechFeedbackAnchor, error) {
	anchor := SpeechFeedbackAnchor{
		StartUTF8Byte:   0,
		EndUTF8Byte:     len(input.ConfirmedText),
		OriginalExcerpt: input.ConfirmedText,
	}
	switch input.Source.SourceKind {
	case SpeechFeedbackSourceConversationTurn:
		anchor.AnchorKind =
			SpeechFeedbackAnchorConversationTranscript
		anchor.EvidenceRefID = input.EvidenceRefID
		anchor.TurnID = input.Source.TurnID
	case SpeechFeedbackSourceAgentVoiceMessage:
		anchor.AnchorKind = SpeechFeedbackAnchorAgentTranscript
		anchor.TranscriptEvidenceID =
			input.Source.TranscriptEvidenceID
		anchor.MessageID = input.Source.MessageID
	default:
		return SpeechFeedbackAnchor{}, ErrInvalidSpeechFeedback
	}
	if !anchor.validFor(
		input.Source,
		input.EvidenceRefID,
		input.ConfirmedText,
	) {
		return SpeechFeedbackAnchor{}, ErrInvalidSpeechFeedback
	}
	return anchor, nil
}

var _ SpeechFeedbackProvider = (*TextSpeechFeedbackProvider)(nil)
