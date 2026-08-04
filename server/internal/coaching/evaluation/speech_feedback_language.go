package evaluation

import (
	"strings"
	"unicode"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
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

func speechFeedbackISECategory(text string) xfyun.EvaluationCategory {
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
		return xfyun.CategoryReadWord
	}
	return xfyun.CategoryReadSentence
}
