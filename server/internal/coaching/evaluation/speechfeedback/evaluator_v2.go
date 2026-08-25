package speechfeedback

import (
	"bytes"
	"context"
	"encoding/json"
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
		PipelineVersion: "speech-evaluation/v2",
		PromptVersion:   "speech-feedback/v2",
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
	projection := projectSpeechFeedbackEnglishText(snapshot.Transcript)
	if kind == evaluation.KindAgentMessageFeedback {
		projection = projectSpeechFeedbackOralReference(projection)
	}
	englishText := projection.text
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
	items, err := compactFeedbackItemsWithProjection(
		generated,
		snapshot,
		englishText,
		projection,
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
	Kind             SpeechFeedbackItemKind `json:"kind"`
	Explanation      string                 `json:"explanation"`
	SourceText       json.RawMessage        `json:"source_text"`
	SourceOccurrence json.RawMessage        `json:"source_occurrence"`
	SuggestedText    json.RawMessage        `json:"suggested_text"`
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
	return compactFeedbackItemsWithProjection(
		generated,
		snapshot,
		englishText,
		projectSpeechFeedbackEnglishText(snapshot.Transcript),
		repracticeMode,
	)
}

func compactFeedbackItemsWithProjection(
	generated TextGenerationResult,
	snapshot evaluation.SpeechInputSnapshot,
	englishText string,
	projection speechFeedbackEnglishProjection,
	repracticeMode string,
) ([]evaluation.FeedbackItemDraft, error) {
	if !validSpeechFeedbackIdentifier(generated.Provider) ||
		!validSpeechFeedbackModel(generated.Model) ||
		!validSpeechFeedbackIdentifier(generated.RequestID) ||
		len(generated.Content) == 0 ||
		len(generated.Content) > maxSpeechFeedbackProviderPayload {
		return nil, compactProviderError(compactNormalizeReasonResponseMetadataInvalid)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(generated.Content))
	decoder.DisallowUnknownFields()
	var envelope compactProviderEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, compactProviderError(compactNormalizeReasonResponseJSONInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, compactProviderError(compactNormalizeReasonResponseJSONInvalid)
	}
	if len(envelope.Items) == 0 || len(envelope.Items) > maxSpeechFeedbackProviderItems {
		return nil, compactProviderError(compactNormalizeReasonItemCountInvalid)
	}
	for _, generatedItem := range envelope.Items {
		if generatedItem.Kind == SpeechFeedbackItemStrength {
			sourceText, sourcePresent := compactProviderNullableText(
				generatedItem.SourceText,
			)
			sourceOccurrence, occurrencePresent := compactProviderNullableInteger(
				generatedItem.SourceOccurrence,
			)
			suggestedText, suggestedPresent := compactProviderNullableText(
				generatedItem.SuggestedText,
			)
			if len(envelope.Items) != 1 || !sourcePresent || sourceText != nil ||
				!occurrencePresent || sourceOccurrence != nil ||
				!suggestedPresent || suggestedText != nil {
				return nil, compactProviderError(compactNormalizeReasonStrengthContractInvalid)
			}
			item := evaluation.FeedbackItemDraft{
				Category: string(SpeechFeedbackItemStrength),
				Evidence: evaluation.FeedbackEvidence{
					EvidenceRefID:   snapshot.EvidenceRefID,
					StartUTF8Byte:   0,
					EndUTF8Byte:     len(snapshot.Transcript),
					OriginalExcerpt: snapshot.Transcript,
				},
				Recommendation: safeSpeechFeedbackExplanation(
					generatedItem.Kind,
					generatedItem.Explanation,
					englishText,
				),
				RepracticeMode: "NONE",
			}
			if !item.Valid() {
				return nil, compactProviderError(compactNormalizeReasonNormalizedItemInvalid)
			}
			return []evaluation.FeedbackItemDraft{item}, nil
		}
	}

	items := make([]evaluation.FeedbackItemDraft, 0, len(envelope.Items))
	seen := make(map[string]struct{}, len(envelope.Items))
	for _, generatedItem := range envelope.Items {
		sourceTextValue, sourcePresent := compactProviderNullableText(
			generatedItem.SourceText,
		)
		sourceOccurrence, occurrencePresent := compactProviderNullableInteger(
			generatedItem.SourceOccurrence,
		)
		suggestedTextValue, suggestedPresent := compactProviderNullableText(
			generatedItem.SuggestedText,
		)
		if !sourcePresent || sourceTextValue == nil ||
			!validSpeechFeedbackAdviceText(*sourceTextValue) ||
			!occurrencePresent || sourceOccurrence == nil ||
			*sourceOccurrence < 1 ||
			!suggestedPresent || suggestedTextValue == nil ||
			!validSpeechFeedbackAdviceText(*suggestedTextValue) {
			return nil, compactProviderError(compactNormalizeReasonSuggestionContractInvalid)
		}
		sourceText := strings.TrimSpace(*sourceTextValue)
		suggestedText := strings.TrimSpace(*suggestedTextValue)
		if !speechFeedbackEnglishWordPattern.MatchString(sourceText) ||
			!speechFeedbackEnglishWordPattern.MatchString(suggestedText) {
			return nil, compactProviderError(compactNormalizeReasonSuggestionLanguageInvalid)
		}
		start, end, located := projection.excerptRange(
			sourceText,
			*sourceOccurrence,
		)
		if !located {
			return nil, compactProviderError(compactNormalizeReasonEvidenceInvalid)
		}
		if sameSpeechFeedbackLexicalContent(sourceText, suggestedText) {
			continue
		}
		item := evaluation.FeedbackItemDraft{
			Category: string(generatedItem.Kind),
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID:   snapshot.EvidenceRefID,
				StartUTF8Byte:   start,
				EndUTF8Byte:     end,
				OriginalExcerpt: snapshot.Transcript[start:end],
			},
			Recommendation: safeSpeechFeedbackExplanation(
				generatedItem.Kind,
				generatedItem.Explanation,
				englishText,
			),
			Correction:     suggestedText,
			RepracticeMode: repracticeMode,
		}
		switch generatedItem.Kind {
		case SpeechFeedbackItemCorrection:
			if optionalSpeechFeedbackConnectorSwap(sourceText, suggestedText) {
				item.Category = string(SpeechFeedbackItemRecommendedExpression)
				item.Severity = "LOW"
				item.Evidence.StartUTF8Byte = 0
				item.Evidence.EndUTF8Byte = len(snapshot.Transcript)
				item.Evidence.OriginalExcerpt = snapshot.Transcript
				item.Correction = snapshot.Transcript[:start] + suggestedText +
					snapshot.Transcript[end:]
			} else {
				item.Severity = "MEDIUM"
			}
		case SpeechFeedbackItemRecommendedExpression:
			item.Severity = "LOW"
		default:
			return nil, compactProviderError(compactNormalizeReasonItemKindInvalid)
		}
		if !item.Valid() {
			return nil, compactProviderError(compactNormalizeReasonNormalizedItemInvalid)
		}
		key := item.Category + "\x00" + item.Recommendation + "\x00" + item.Correction
		if _, duplicate := seen[key]; duplicate {
			return nil, compactProviderError(compactNormalizeReasonDuplicateItem)
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if len(items) == 0 {
		item := evaluation.FeedbackItemDraft{
			Category: string(SpeechFeedbackItemStrength),
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID:   snapshot.EvidenceRefID,
				StartUTF8Byte:   0,
				EndUTF8Byte:     len(snapshot.Transcript),
				OriginalExcerpt: snapshot.Transcript,
			},
			Recommendation: "原表达没有可定位的语言错误，无需修改。",
			RepracticeMode: "NONE",
		}
		if !item.Valid() {
			return nil, compactProviderError(compactNormalizeReasonNormalizedItemInvalid)
		}
		items = append(items, item)
	}
	return items, nil
}

func compactProviderNullableText(value json.RawMessage) (*string, bool) {
	if len(value) == 0 {
		return nil, false
	}
	if bytes.Equal(value, []byte("null")) {
		return nil, true
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, false
	}
	return &decoded, true
}

func compactProviderNullableInteger(value json.RawMessage) (*int, bool) {
	if len(value) == 0 {
		return nil, false
	}
	if bytes.Equal(value, []byte("null")) {
		return nil, true
	}
	var decoded int
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, false
	}
	return &decoded, true
}

func speechFeedbackExcerptStart(
	transcript string,
	excerpt string,
	occurrence int,
) int {
	searchStart := 0
	for current := 1; current <= occurrence; current++ {
		relative := strings.Index(transcript[searchStart:], excerpt)
		if relative < 0 {
			return -1
		}
		absolute := searchStart + relative
		if current == occurrence {
			return absolute
		}
		searchStart = absolute + len(excerpt)
	}
	return -1
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

type compactNormalizeReason string

const (
	compactNormalizeReasonResponseMetadataInvalid   compactNormalizeReason = "response_metadata_invalid"
	compactNormalizeReasonResponseJSONInvalid       compactNormalizeReason = "response_json_invalid"
	compactNormalizeReasonItemCountInvalid          compactNormalizeReason = "item_count_invalid"
	compactNormalizeReasonStrengthContractInvalid   compactNormalizeReason = "strength_contract_invalid"
	compactNormalizeReasonSuggestionContractInvalid compactNormalizeReason = "suggestion_contract_invalid"
	compactNormalizeReasonSuggestionLanguageInvalid compactNormalizeReason = "suggestion_language_invalid"
	compactNormalizeReasonEvidenceInvalid           compactNormalizeReason = "evidence_invalid"
	compactNormalizeReasonItemKindInvalid           compactNormalizeReason = "item_kind_invalid"
	compactNormalizeReasonNormalizedItemInvalid     compactNormalizeReason = "normalized_item_invalid"
	compactNormalizeReasonDuplicateItem             compactNormalizeReason = "duplicate_item"
)

func (reason compactNormalizeReason) valid() bool {
	switch reason {
	case compactNormalizeReasonResponseMetadataInvalid,
		compactNormalizeReasonResponseJSONInvalid,
		compactNormalizeReasonItemCountInvalid,
		compactNormalizeReasonStrengthContractInvalid,
		compactNormalizeReasonSuggestionContractInvalid,
		compactNormalizeReasonSuggestionLanguageInvalid,
		compactNormalizeReasonEvidenceInvalid,
		compactNormalizeReasonItemKindInvalid,
		compactNormalizeReasonNormalizedItemInvalid,
		compactNormalizeReasonDuplicateItem:
		return true
	default:
		return false
	}
}

type compactProviderFailure struct{ reason compactNormalizeReason }

func (compactProviderFailure) Error() string {
	return "evaluation: speech feedback provider response invalid"
}
func (failure compactProviderFailure) StableCategory() string   { return "PROVIDER_RESPONSE_INVALID" }
func (failure compactProviderFailure) Retryable() bool          { return false }
func (failure compactProviderFailure) AutomaticRetryable() bool { return true }
func (failure compactProviderFailure) EvaluationNormalizeReason() string {
	return string(failure.reason)
}

func compactProviderError(reason compactNormalizeReason) error {
	return compactProviderFailure{reason: reason}
}

var _ evaluation.SpeechEvaluators = (*CompactEvaluator)(nil)
var _ GenerationFailure = compactProviderFailure{}
