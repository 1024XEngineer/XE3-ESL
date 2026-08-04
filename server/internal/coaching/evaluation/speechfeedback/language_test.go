package speechfeedback

import (
	"testing"
)

func TestClassifySpeechFeedbackLanguage(t *testing.T) {
	tests := map[string]speechFeedbackLanguage{
		"Hello":            speechFeedbackLanguageEnglish,
		"I am ready.":      speechFeedbackLanguageEnglish,
		"你好":               speechFeedbackLanguageChinese,
		"你好, I am Olivia.": speechFeedbackLanguageMixed,
		"123?!":            speechFeedbackLanguageUncertain,
		"Привет":           speechFeedbackLanguageUncertain,
	}
	for text, expected := range tests {
		if actual := classifySpeechFeedbackLanguage(text); actual != expected {
			t.Fatalf("%q classified as %s, want %s", text, actual, expected)
		}
	}
}

func TestSpeechFeedbackEnglishReferenceTextProjectsMixedLanguage(t *testing.T) {
	t.Parallel()
	text := "这是补充。 I like AI, 因为 it helps me."
	if actual := speechFeedbackEnglishReferenceText(text); actual !=
		"I like AI, it helps me" {
		t.Fatalf("English reference = %q", actual)
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
