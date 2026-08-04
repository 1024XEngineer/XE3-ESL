package evaluation

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
)

func TestClassifySpeechFeedbackLanguage(t *testing.T) {
	tests := map[string]speechFeedbackLanguage{
		"Hello":            speechFeedbackLanguageEnglish,
		"I am ready.":      speechFeedbackLanguageEnglish,
		"你好":               speechFeedbackLanguageChinese,
		"你好, I am Olivia.": speechFeedbackLanguageChinese,
		"123?!":            speechFeedbackLanguageUncertain,
		"Привет":           speechFeedbackLanguageUncertain,
	}
	for text, expected := range tests {
		if actual := classifySpeechFeedbackLanguage(text); actual != expected {
			t.Fatalf("%q classified as %s, want %s", text, actual, expected)
		}
	}
}

func TestSpeechFeedbackISECategory(t *testing.T) {
	tests := map[string]xfyun.EvaluationCategory{
		"Hello":         xfyun.CategoryReadWord,
		"Hello!":        xfyun.CategoryReadWord,
		"I am ready.":   xfyun.CategoryReadSentence,
		"well-prepared": xfyun.CategoryReadSentence,
	}
	for text, expected := range tests {
		if actual := speechFeedbackISECategory(text); actual != expected {
			t.Fatalf("%q category = %s, want %s", text, actual, expected)
		}
	}
}
