package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadResumeOCRDefaultsToDisabled(t *testing.T) {
	t.Setenv("RESUME_OCR_ENABLED", "")
	t.Setenv("RESUME_OCR_TIMEOUT", "")

	configuration, err := LoadResumeOCR()
	if err != nil {
		t.Fatalf("LoadResumeOCR: %v", err)
	}
	if configuration.Enabled || configuration.Endpoint != ResumeOCREndpoint ||
		configuration.Region != ResumeOCRRegion ||
		configuration.Timeout != 45*time.Second {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestLoadResumeOCREnabledAndTimeout(t *testing.T) {
	t.Setenv("RESUME_OCR_ENABLED", "true")
	t.Setenv("RESUME_OCR_TIMEOUT", "75s")

	configuration, err := LoadResumeOCR()
	if err != nil {
		t.Fatalf("LoadResumeOCR: %v", err)
	}
	if !configuration.Enabled || configuration.Timeout != 75*time.Second {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestLoadResumeOCRRejectsInvalidValues(t *testing.T) {
	for name, test := range map[string]struct {
		enabled string
		timeout string
		want    error
	}{
		"enabled": {enabled: "yes", want: ErrResumeOCREnabledInvalid},
		"timeout": {enabled: "true", timeout: "3m", want: ErrResumeOCRTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("RESUME_OCR_ENABLED", test.enabled)
			t.Setenv("RESUME_OCR_TIMEOUT", test.timeout)
			_, err := LoadResumeOCR()
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
