package evaluation

import (
	"strings"
	"unicode"
)

type speechFeedbackLanguage string

const (
	speechFeedbackLanguageEnglish   speechFeedbackLanguage = "ENGLISH"
	speechFeedbackLanguageChinese   speechFeedbackLanguage = "CHINESE"
	speechFeedbackLanguageUncertain speechFeedbackLanguage = "UNCERTAIN"
)

func classifySpeechFeedbackLanguage(text string) speechFeedbackLanguage {
	hasLatinLetter := false
	for _, character := range strings.TrimSpace(text) {
		switch {
		case unicode.Is(unicode.Han, character):
			return speechFeedbackLanguageChinese
		case unicode.IsLetter(character):
			if !unicode.Is(unicode.Latin, character) {
				return speechFeedbackLanguageUncertain
			}
			hasLatinLetter = true
		}
	}
	if hasLatinLetter {
		return speechFeedbackLanguageEnglish
	}
	return speechFeedbackLanguageUncertain
}

func speechFeedbackAcousticCategory(text string) AcousticAssessmentCategory {
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
