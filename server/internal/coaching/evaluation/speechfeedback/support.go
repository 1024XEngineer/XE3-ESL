package speechfeedback

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	SpeechFeedbackSchemaVersion      = "speech-feedback/v1"
	maxSpeechFeedbackProviderPayload = 128 * 1024
	maxSpeechFeedbackProviderItems   = 3
)

var ErrInvalidSpeechFeedback = errors.New("evaluation: invalid speech feedback")

type SpeechFeedbackItemKind string

const (
	SpeechFeedbackItemStrength              SpeechFeedbackItemKind = "STRENGTH"
	SpeechFeedbackItemCorrection            SpeechFeedbackItemKind = "CORRECTION"
	SpeechFeedbackItemRecommendedExpression SpeechFeedbackItemKind = "RECOMMENDED_EXPRESSION"
)

var speechFeedbackIdentifierPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`,
)

func validSpeechFeedbackIdentifier(value string) bool {
	return value == strings.TrimSpace(value) &&
		speechFeedbackIdentifierPattern.MatchString(value)
}

func validSpeechFeedbackModel(value string) bool {
	return validSpeechFeedbackIdentifier(value)
}

func validSpeechFeedbackAdviceText(value string) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) &&
		value != "" && len(value) <= 4096 && !strings.ContainsRune(value, '\x00')
}

func equalSpeechFeedbackText(left string, right string) bool {
	normalize := func(value string) string {
		return strings.ToLower(strings.Join(strings.Fields(value), " "))
	}
	return normalize(left) == normalize(right)
}

const speechFeedbackSystemPrompt = `Return one JSON object with an items array for a confirmed English transcript. Each item must have kind, explanation, and suggested_text. Allowed kinds are STRENGTH, CORRECTION, and RECOMMENDED_EXPRESSION. For AGENT_MESSAGE_FEEDBACK, english_text is an oral-language assessment projection from voice ASR: pause-induced punctuation between complete spoken clauses may already be normalized. Judge the spoken grammar, never issue CORRECTION solely for an ASR-authored sentence boundary, and still identify a genuine standalone sentence fragment. If the answer is already strong, return exactly one STRENGTH with suggested_text set to null. Otherwise return one to three non-duplicate CORRECTION or RECOMMENDED_EXPRESSION items and include a genuinely improved suggested_text for each. Do not claim to assess pronunciation or audio. Return JSON only.`
