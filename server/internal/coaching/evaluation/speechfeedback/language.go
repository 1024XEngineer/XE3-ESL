package speechfeedback

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type speechFeedbackLanguage string

const (
	speechFeedbackLanguageEnglish   speechFeedbackLanguage = "ENGLISH"
	speechFeedbackLanguageMixed     speechFeedbackLanguage = "MIXED"
	speechFeedbackLanguageChinese   speechFeedbackLanguage = "CHINESE"
	speechFeedbackLanguageUncertain speechFeedbackLanguage = "UNCERTAIN"
)

func classifySpeechFeedbackLanguage(text string) speechFeedbackLanguage {
	var hasLatinLetter, hasHanLetter, hasOtherLetter bool
	for _, character := range strings.TrimSpace(text) {
		switch {
		case unicode.Is(unicode.Han, character):
			hasHanLetter = true
		case unicode.IsLetter(character):
			if !unicode.Is(unicode.Latin, character) {
				hasOtherLetter = true
				continue
			}
			hasLatinLetter = true
		}
	}
	if hasOtherLetter {
		return speechFeedbackLanguageUncertain
	}
	if hasLatinLetter && hasHanLetter {
		return speechFeedbackLanguageMixed
	}
	if hasLatinLetter {
		return speechFeedbackLanguageEnglish
	}
	if hasHanLetter {
		return speechFeedbackLanguageChinese
	}
	return speechFeedbackLanguageUncertain
}

func speechFeedbackHasAssessableEnglish(text string) bool {
	language := classifySpeechFeedbackLanguage(text)
	return language == speechFeedbackLanguageEnglish ||
		language == speechFeedbackLanguageMixed
}

// speechFeedbackEnglishReferenceText keeps only the English-addressable part
// of a confirmed mixed-language transcript. The original transcript remains
// the immutable text evidence; this projection is used only as the ISE paper.
func speechFeedbackEnglishReferenceText(text string) string {
	return projectSpeechFeedbackEnglishText(text).text
}

type speechFeedbackEnglishProjection struct {
	text           string
	originalStarts []int
	originalEnds   []int
}

func projectSpeechFeedbackEnglishText(text string) speechFeedbackEnglishProjection {
	var output strings.Builder
	originalStarts := make([]int, 0, len(text))
	originalEnds := make([]int, 0, len(text))
	needsSpace := false
	spaceStart := 0
	appendRune := func(character rune, originalStart int, originalEnd int) {
		output.WriteRune(character)
		for range utf8.RuneLen(character) {
			originalStarts = append(originalStarts, originalStart)
			originalEnds = append(originalEnds, originalEnd)
		}
	}
	for originalStart, character := range text {
		originalEnd := originalStart + utf8.RuneLen(character)
		allowed := unicode.Is(unicode.Latin, character) &&
			unicode.IsLetter(character)
		allowed = allowed || unicode.IsNumber(character) ||
			(character <= unicode.MaxASCII &&
				(unicode.IsPunct(character) || unicode.IsSpace(character)))
		if !allowed || unicode.IsSpace(character) {
			if output.Len() > 0 && !needsSpace {
				spaceStart = originalStart
			}
			needsSpace = output.Len() > 0
			continue
		}
		if needsSpace {
			appendRune(' ', spaceStart, originalStart)
		}
		appendRune(character, originalStart, originalEnd)
		needsSpace = false
	}
	projected := output.String()
	trimmed := strings.Trim(projected, " \t\r\n,;:!?-.")
	if trimmed == "" {
		return speechFeedbackEnglishProjection{}
	}
	start := strings.Index(projected, trimmed)
	end := start + len(trimmed)
	return speechFeedbackEnglishProjection{
		text:           trimmed,
		originalStarts: originalStarts[start:end],
		originalEnds:   originalEnds[start:end],
	}
}

func (projection speechFeedbackEnglishProjection) excerptRange(
	excerpt string,
	occurrence int,
) (int, int, bool) {
	projectedStart := speechFeedbackExcerptStart(
		projection.text,
		excerpt,
		occurrence,
	)
	projectedEnd := projectedStart + len(excerpt)
	if projectedStart < 0 || projectedEnd > len(projection.originalStarts) ||
		projectedStart >= projectedEnd {
		return 0, 0, false
	}
	return projection.originalStarts[projectedStart],
		projection.originalEnds[projectedEnd-1], true
}

func speechFeedbackAcousticCategory(
	text string,
) AcousticAssessmentCategory {
	words := 0
	inWord := false
	for _, character := range strings.TrimSpace(text) {
		isWord := unicode.IsLetter(character) || unicode.IsNumber(character)
		if isWord && !inWord {
			words++
		}
		inWord = isWord
	}
	if words == 1 {
		return AcousticCategoryReadWord
	}
	return AcousticCategoryReadSentence
}
