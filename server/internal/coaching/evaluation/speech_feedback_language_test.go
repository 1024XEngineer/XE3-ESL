package evaluation

import (
	"testing"
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

func TestSpeechFeedbackAcousticCategory(t *testing.T) {
	tests := map[string]AcousticAssessmentCategory{
		"Hello":         AcousticCategoryReadWord,
		"Hello!":        AcousticCategoryReadWord,
		"I am ready.":   AcousticCategoryReadSentence,
		"well-prepared": AcousticCategoryReadSentence,
	}
	for text, expected := range tests {
		if actual := speechFeedbackAcousticCategory(text); actual != expected {
			t.Fatalf("%q category = %s, want %s", text, actual, expected)
		}
	}
}
