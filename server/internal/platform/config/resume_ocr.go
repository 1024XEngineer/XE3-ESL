package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

const (
	defaultResumeOCRTimeout = 45 * time.Second
	maximumResumeOCRTimeout = 2 * time.Minute
	ResumeOCREndpoint       = "ocr.cn-shanghai.aliyuncs.com"
	ResumeOCRRegion         = "cn-shanghai"
)

var (
	ErrResumeOCREnabledInvalid = errors.New(
		"RESUME_OCR_ENABLED must be 0, 1, false, or true",
	)
	ErrResumeOCRTimeout = errors.New(
		"RESUME_OCR_TIMEOUT must be positive and no greater than 2m",
	)
)

// ResumeOCRConfig contains the non-secret Alibaba Cloud RecognizePdf settings.
// Credentials are supplied by the existing OSS credentials provider.
type ResumeOCRConfig struct {
	Enabled  bool
	Endpoint string
	Region   string
	Timeout  time.Duration
}

// LoadResumeOCR reads the server-only OCR fallback configuration.
func LoadResumeOCR() (ResumeOCRConfig, error) {
	enabled, err := parseResumeOCREnabled(os.Getenv("RESUME_OCR_ENABLED"))
	if err != nil {
		return ResumeOCRConfig{}, err
	}
	configuration := ResumeOCRConfig{
		Enabled:  enabled,
		Endpoint: ResumeOCREndpoint,
		Region:   ResumeOCRRegion,
		Timeout:  defaultResumeOCRTimeout,
	}
	if raw := strings.TrimSpace(os.Getenv("RESUME_OCR_TIMEOUT")); raw != "" {
		timeout, parseErr := time.ParseDuration(raw)
		if parseErr != nil || timeout <= 0 || timeout > maximumResumeOCRTimeout {
			return ResumeOCRConfig{}, ErrResumeOCRTimeout
		}
		configuration.Timeout = timeout
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
