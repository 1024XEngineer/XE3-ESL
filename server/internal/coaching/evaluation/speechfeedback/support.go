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

var speechFeedbackEnglishWordPattern = regexp.MustCompile(
	`[A-Za-z]+(?:['’][A-Za-z]+)*`,
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

func sameSpeechFeedbackLexicalContent(left string, right string) bool {
	leftWords := speechFeedbackEnglishWordPattern.FindAllString(left, -1)
	rightWords := speechFeedbackEnglishWordPattern.FindAllString(right, -1)
	if len(leftWords) == 0 || len(leftWords) != len(rightWords) {
		return false
	}
	for index := range leftWords {
		if !strings.EqualFold(leftWords[index], rightWords[index]) {
			return false
		}
	}
	return true
}

func validSpeechFeedbackExplanation(value string, englishText string) bool {
	if !validSpeechFeedbackAdviceText(value) {
		return false
	}
	inputWords := make(map[string]struct{})
	for _, word := range speechFeedbackEnglishWordPattern.FindAllString(
		englishText,
		-1,
	) {
		inputWords[strings.ToLower(word)] = struct{}{}
	}
	for _, word := range speechFeedbackEnglishWordPattern.FindAllString(value, -1) {
		if _, exists := inputWords[strings.ToLower(word)]; !exists {
			return false
		}
	}
	return true
}

func safeSpeechFeedbackExplanation(
	kind SpeechFeedbackItemKind,
	value string,
	englishText string,
) string {
	if validSpeechFeedbackExplanation(value, englishText) {
		return strings.TrimSpace(value)
	}
	switch kind {
	case SpeechFeedbackItemCorrection:
		return "此处存在可定位的语言错误，建议使用右侧表达。"
	case SpeechFeedbackItemRecommendedExpression:
		return "这是保留原意的可选表达。"
	case SpeechFeedbackItemStrength:
		return "原表达没有可定位的语言错误，无需修改。"
	default:
		return "反馈内容已通过安全校验。"
	}
}

func optionalSpeechFeedbackConnectorSwap(left string, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	return left != right &&
		(left == "and" || left == "so") &&
		(right == "and" || right == "so")
}

const speechFeedbackSystemPrompt = `Return one JSON object with an items array for a confirmed English transcript. Every item must have kind, explanation, source_text, source_occurrence, and suggested_text. Allowed kinds are STRENGTH, CORRECTION, and RECOMMENDED_EXPRESSION. For AGENT_MESSAGE_FEEDBACK, english_text is an oral-language assessment projection from voice ASR: pause-induced punctuation between complete spoken clauses may already be normalized. Never issue CORRECTION solely for an ASR-authored sentence boundary, and still identify a genuine standalone sentence fragment. Use CORRECTION only for a necessary, locatable language error: source_text must be one exact contiguous excerpt copied from english_text, source_occurrence must be its 1-based occurrence, and suggested_text must contain only the replacement for that excerpt. Never use CORRECTION for capitalization, punctuation, personal preference, optional wording, or a valid choice between and and so. Use RECOMMENDED_EXPRESSION only for an optional meaning-preserving alternative; source_text and source_occurrence must still identify exact input evidence. If no change is useful, return exactly one STRENGTH with source_text, source_occurrence, and suggested_text set to null. STRENGTH cannot appear with any other item. Write explanation only in Simplified Chinese without English words; never invent or discuss a word the user did not say. Do not claim to assess pronunciation or audio. Return JSON only.`
