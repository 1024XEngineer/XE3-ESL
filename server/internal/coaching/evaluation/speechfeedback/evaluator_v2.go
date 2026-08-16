package speechfeedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

type CompactEvaluator struct {
	generator TextGenerator
}

func Lineage(provider string, model string) (evaluation.ConfigLineage, error) {
	lineage := evaluation.ConfigLineage{
		SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
		StrategyRef:     "speech-feedback/v1",
		PipelineVersion: "speech-evaluation/v1",
		PromptVersion:   "speech-feedback/v1",
		ResultSchema:    SpeechFeedbackSchemaVersion,
		Provider:        provider,
		Model:           model,
	}
	if !lineage.Valid() {
		return evaluation.ConfigLineage{}, evaluation.ErrInvalidRequest
	}
	return lineage, nil
}

func NewCompactEvaluator(generator TextGenerator) (*CompactEvaluator, error) {
	if generator == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &CompactEvaluator{generator: generator}, nil
}

func (evaluator *CompactEvaluator) EvaluatePracticeTurn(
	ctx context.Context,
	snapshot evaluation.SpeechInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, []evaluation.FeedbackItemDraft, error) {
	return evaluator.evaluate(
		ctx,
		evaluation.KindPracticeTurnFeedback,
		snapshot,
		lineage,
		"SAME_QUESTION",
	)
}

func (evaluator *CompactEvaluator) EvaluateAgentMessage(
	ctx context.Context,
	snapshot evaluation.SpeechInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, []evaluation.FeedbackItemDraft, error) {
	return evaluator.evaluate(
		ctx,
		evaluation.KindAgentMessageFeedback,
		snapshot,
		lineage,
		"NONE",
	)
}

func (evaluator *CompactEvaluator) evaluate(
	ctx context.Context,
	kind evaluation.Kind,
	snapshot evaluation.SpeechInputSnapshot,
	lineage evaluation.ConfigLineage,
	repracticeMode string,
) (json.RawMessage, []evaluation.FeedbackItemDraft, error) {
	if evaluator == nil || evaluator.generator == nil || ctx == nil ||
		!snapshot.Valid(kind) || !lineage.Valid() || snapshot.Acoustic == nil {
		return nil, nil, ErrInvalidSpeechFeedback
	}
	if !speechFeedbackHasAssessableEnglish(snapshot.Transcript) {
		result := evaluation.SpeechResult{
			SchemaVersion:      SpeechFeedbackSchemaVersion,
			ScoreabilityStatus: "INSUFFICIENT",
			Summary:            "The confirmed transcript does not contain enough assessable English.",
			ReasonCodes:        []string{"TEXT_NOT_ASSESSABLE"},
			Acoustic:           *snapshot.Acoustic,
		}
		return encodeCompactSpeechResult(result, nil)
	}
	englishText := speechFeedbackEnglishReferenceText(snapshot.Transcript)
	payload, err := json.Marshal(struct {
		Kind        evaluation.Kind `json:"kind"`
		EnglishText string          `json:"english_text"`
	}{Kind: kind, EnglishText: englishText})
	if err != nil {
		return nil, nil, ErrInvalidSpeechFeedback
	}
	generated, err := evaluator.generator.Generate(ctx, TextGenerationRequest{
		SystemPrompt: speechFeedbackSystemPrompt,
		UserPrompt:   string(payload),
	})
	if err != nil {
		return nil, nil, err
	}
	items, err := compactFeedbackItems(
		generated,
		snapshot,
		englishText,
		repracticeMode,
	)
	if err != nil {
		return nil, nil, err
	}
	result := evaluation.SpeechResult{
		SchemaVersion:      SpeechFeedbackSchemaVersion,
		ScoreabilityStatus: "PROVISIONAL",
		Summary:            "Feedback is ready for this confirmed transcript.",
		ReasonCodes:        []string{},
		Acoustic:           *snapshot.Acoustic,
	}
	return encodeCompactSpeechResult(result, items)
}

type compactProviderItem struct {
	Kind          SpeechFeedbackItemKind `json:"kind"`
	Explanation   string                 `json:"explanation"`
	SuggestedText *string                `json:"suggested_text,omitempty"`
}

type compactProviderEnvelope struct {
	Items []compactProviderItem `json:"items"`
}

func compactFeedbackItems(
	generated TextGenerationResult,
	snapshot evaluation.SpeechInputSnapshot,
	englishText string,
	repracticeMode string,
) ([]evaluation.FeedbackItemDraft, error) {
	if !validSpeechFeedbackIdentifier(generated.Provider) ||
		!validSpeechFeedbackModel(generated.Model) ||
		!validSpeechFeedbackIdentifier(generated.RequestID) ||
		len(generated.Content) == 0 ||
		len(generated.Content) > maxSpeechFeedbackProviderPayload {
		return nil, compactProviderError("provider response metadata is invalid")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(generated.Content))
	decoder.DisallowUnknownFields()
	var envelope compactProviderEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, compactProviderError(fmt.Sprintf("decode provider response: %v", err))
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, compactProviderError("provider response has trailing JSON")
	}
	if len(envelope.Items) == 0 || len(envelope.Items) > maxSpeechFeedbackProviderItems {
		return nil, compactProviderError("provider item count is invalid")
	}
	items := make([]evaluation.FeedbackItemDraft, len(envelope.Items))
	seen := make(map[string]struct{}, len(envelope.Items))
	for index, generatedItem := range envelope.Items {
		if !validSpeechFeedbackAdviceText(generatedItem.Explanation) {
			return nil, compactProviderError("provider explanation is invalid")
		}
		item := evaluation.FeedbackItemDraft{
			Category: string(generatedItem.Kind),
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID:   snapshot.EvidenceRefID,
				StartUTF8Byte:   0,
				EndUTF8Byte:     len(snapshot.Transcript),
				OriginalExcerpt: snapshot.Transcript,
			},
			Recommendation: generatedItem.Explanation,
		}
		switch generatedItem.Kind {
		case SpeechFeedbackItemCorrection:
			item.Severity = "MEDIUM"
			item.RepracticeMode = repracticeMode
		case SpeechFeedbackItemRecommendedExpression:
			item.Severity = "LOW"
			item.RepracticeMode = repracticeMode
		case SpeechFeedbackItemStrength:
			if len(envelope.Items) != 1 || generatedItem.SuggestedText != nil {
				return nil, compactProviderError("STRENGTH must be the only item")
			}
			item.RepracticeMode = "NONE"
		default:
			return nil, compactProviderError("provider item kind is invalid")
		}
		if generatedItem.Kind != SpeechFeedbackItemStrength {
			if generatedItem.SuggestedText == nil ||
				!validSpeechFeedbackAdviceText(*generatedItem.SuggestedText) ||
				equalSpeechFeedbackText(*generatedItem.SuggestedText, englishText) {
				return nil, compactProviderError("provider correction is invalid")
			}
			item.Correction = strings.TrimSpace(*generatedItem.SuggestedText)
		}
		if !item.Valid() {
			return nil, compactProviderError("normalized feedback item is invalid")
		}
		key := item.Category + "\x00" + item.Recommendation + "\x00" + item.Correction
		if _, duplicate := seen[key]; duplicate {
			return nil, compactProviderError("provider returned duplicate items")
		}
		seen[key] = struct{}{}
		items[index] = item
	}
	return items, nil
}

func encodeCompactSpeechResult(
	result evaluation.SpeechResult,
	items []evaluation.FeedbackItemDraft,
) (json.RawMessage, []evaluation.FeedbackItemDraft, error) {
	if !result.Valid() {
		return nil, nil, ErrInvalidSpeechFeedback
	}
	encoded, _, err := evaluation.EncodeStrict(result)
	if err != nil {
		return nil, nil, err
	}
	return encoded, items, nil
}

type compactProviderFailure struct{ message string }

func (failure compactProviderFailure) Error() string          { return failure.message }
func (failure compactProviderFailure) StableCategory() string { return "PROVIDER_RESPONSE_INVALID" }
func (failure compactProviderFailure) Retryable() bool        { return false }

func compactProviderError(message string) error {
	return compactProviderFailure{message: message}
}

var _ evaluation.SpeechEvaluators = (*CompactEvaluator)(nil)
var _ GenerationFailure = compactProviderFailure{}
