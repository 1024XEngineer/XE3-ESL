package speechfeedback

import (
	"strings"
	"unicode"
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
	var output strings.Builder
	needsSpace := false
	for _, character := range strings.TrimSpace(text) {
		allowed := unicode.Is(unicode.Latin, character) &&
			unicode.IsLetter(character)
		allowed = allowed || unicode.IsNumber(character) ||
			(character <= unicode.MaxASCII &&
				(unicode.IsPunct(character) || unicode.IsSpace(character)))
		if !allowed {
			needsSpace = output.Len() > 0
			continue
		}
		if needsSpace && !unicode.IsSpace(character) {
			output.WriteByte(' ')
		}
		output.WriteRune(character)
		needsSpace = false
	}
	projected := strings.Join(strings.Fields(output.String()), " ")
	return strings.Trim(projected, " \t\r\n,;:!?-.")
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
