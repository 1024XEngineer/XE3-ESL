package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadResumeOCRDefaultsToDisabled(t *testing.T) {
	t.Setenv("RESUME_OCR_ENABLED", "")
	t.Setenv("RESUME_OCR_TIMEOUT", "")
	t.Setenv("PADDLEOCR_ACCESS_TOKEN", "")
	t.Setenv("PADDLEOCR_BASE_URL", "")

	configuration, err := LoadResumeOCR()
	if err != nil {
		t.Fatalf("LoadResumeOCR: %v", err)
	}
	if configuration.Enabled || configuration.AccessToken != "" ||
		configuration.BaseURL != ResumeOCRBaseURL ||
		configuration.Model != ResumeOCRModel ||
		configuration.Timeout != 2*time.Minute {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestLoadResumeOCREnabledAndTimeout(t *testing.T) {
	t.Setenv("RESUME_OCR_ENABLED", "true")
	t.Setenv("RESUME_OCR_TIMEOUT", "75s")
	t.Setenv("PADDLEOCR_ACCESS_TOKEN", "test-token")
	t.Setenv("PADDLEOCR_BASE_URL", "https://paddleocr.example")

	configuration, err := LoadResumeOCR()
	if err != nil {
		t.Fatalf("LoadResumeOCR: %v", err)
	}
	if !configuration.Enabled || configuration.AccessToken != "test-token" ||
		configuration.BaseURL != "https://paddleocr.example" ||
		configuration.Timeout != 75*time.Second {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestLoadResumeOCRRejectsInvalidValues(t *testing.T) {
	for name, test := range map[string]struct {
		enabled string
		timeout string
		token   string
		want    error
	}{
		"enabled": {enabled: "yes", token: "test-token", want: ErrResumeOCREnabledInvalid},
		"timeout": {enabled: "true", timeout: "11m", token: "test-token", want: ErrResumeOCRTimeout},
		"token":   {enabled: "true", want: ErrResumeOCRAccessTokenRequired},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("RESUME_OCR_ENABLED", test.enabled)
			t.Setenv("RESUME_OCR_TIMEOUT", test.timeout)
			t.Setenv("PADDLEOCR_ACCESS_TOKEN", test.token)
			_, err := LoadResumeOCR()
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
