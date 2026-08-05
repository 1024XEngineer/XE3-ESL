package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

const (
	defaultResumeOCRTimeout = 2 * time.Minute
	maximumResumeOCRTimeout = 10 * time.Minute
	ResumeOCRBaseURL        = "https://paddleocr.aistudio-app.com"
	ResumeOCRModel          = "PaddleOCR-VL-1.6"
)

var (
	ErrResumeOCREnabledInvalid = errors.New(
		"RESUME_OCR_ENABLED must be 0, 1, false, or true",
	)
	ErrResumeOCRTimeout = errors.New(
		"RESUME_OCR_TIMEOUT must be positive and no greater than 10m",
	)
	ErrResumeOCRAccessTokenRequired = errors.New(
		"PADDLEOCR_ACCESS_TOKEN is required when RESUME_OCR_ENABLED is enabled",
	)
)

// ResumeOCRConfig contains the server-only PaddleOCR document parsing settings.
type ResumeOCRConfig struct {
	Enabled     bool
	AccessToken string
	BaseURL     string
	Model       string
	Timeout     time.Duration
}

// LoadResumeOCR reads the server-only OCR fallback configuration.
func LoadResumeOCR() (ResumeOCRConfig, error) {
	enabled, err := parseResumeOCREnabled(os.Getenv("RESUME_OCR_ENABLED"))
	if err != nil {
		return ResumeOCRConfig{}, err
	}
	configuration := ResumeOCRConfig{
		Enabled:     enabled,
		AccessToken: strings.TrimSpace(os.Getenv("PADDLEOCR_ACCESS_TOKEN")),
		BaseURL:     ResumeOCRBaseURL,
		Model:       ResumeOCRModel,
		Timeout:     defaultResumeOCRTimeout,
	}
	if raw := strings.TrimSpace(os.Getenv("PADDLEOCR_BASE_URL")); raw != "" {
		configuration.BaseURL = raw
	}
	if raw := strings.TrimSpace(os.Getenv("RESUME_OCR_TIMEOUT")); raw != "" {
		timeout, parseErr := time.ParseDuration(raw)
		if parseErr != nil || timeout <= 0 || timeout > maximumResumeOCRTimeout {
			return ResumeOCRConfig{}, ErrResumeOCRTimeout
		}
		configuration.Timeout = timeout
	}
	if configuration.Enabled && configuration.AccessToken == "" {
		return ResumeOCRConfig{}, ErrResumeOCRAccessTokenRequired
	}
	return configuration, nil
}

func parseResumeOCREnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, ErrResumeOCREnabledInvalid
	}
}
