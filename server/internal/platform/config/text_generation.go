package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	TextProviderQianwen = "qianwen"

	defaultTextTimeout         = 60 * time.Second
	defaultTextMaxOutputTokens = 8192
	defaultAgentContextChars   = 12000
	maximumTextTimeout         = 5 * time.Minute
	maximumTextOutputTokens    = 1_000_000
	maximumAgentContextChars   = 1_000_000
	minimumAgentContextChars   = 5000
)

type TextGenerationConfig struct {
	Provider            string
	BaseURL             string
	Model               string
	SpeechFeedbackModel string
	Timeout             time.Duration
	MaxOutputTokens     int
	MaxContextChars     int
	APIKey              Secret
}

// Secret deliberately redacts itself in common string and JSON formatting.
// Reveal must only be used when constructing the outbound provider request.
type Secret struct {
	value string
}

func (secret Secret) Reveal() string {
	return secret.value
}

func (Secret) String() string {
	return "[REDACTED]"
}

func (Secret) GoString() string {
	return "config.Secret([REDACTED])"
}

func (Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}

// LoadTextGeneration validates the server-only provider configuration before
// production provider registration and AgentRun assembly.
func LoadTextGeneration() (TextGenerationConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("TEXT_GENERATION_PROVIDER")))
	if provider == "" {
		return TextGenerationConfig{}, errors.New("TEXT_GENERATION_PROVIDER is required")
	}
	if provider != TextProviderQianwen {
		return TextGenerationConfig{}, errors.New("TEXT_GENERATION_PROVIDER is not supported")
	}

	baseURL := strings.TrimSpace(os.Getenv("QIANWEN_BASE_URL"))
	if baseURL == "" {
		return TextGenerationConfig{}, errors.New("QIANWEN_BASE_URL is required")
	}
	model := strings.TrimSpace(os.Getenv("QIANWEN_MODEL"))
	if model == "" {
		return TextGenerationConfig{}, errors.New("QIANWEN_MODEL is required")
	}
	speechFeedbackModel := strings.TrimSpace(
		os.Getenv("QIANWEN_SPEECH_FEEDBACK_MODEL"),
	)
	if speechFeedbackModel == "" {
		return TextGenerationConfig{}, errors.New(
			"QIANWEN_SPEECH_FEEDBACK_MODEL is required",
		)
	}
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		return TextGenerationConfig{}, errors.New("DASHSCOPE_API_KEY is required")
	}
	if strings.IndexFunc(apiKey, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return TextGenerationConfig{}, errors.New("DASHSCOPE_API_KEY contains whitespace or control characters")
	}

	timeout, err := durationOrDefault("QIANWEN_TIMEOUT", defaultTextTimeout)
	if err != nil {
		return TextGenerationConfig{}, err
	}
	if timeout <= 0 || timeout > maximumTextTimeout {
		return TextGenerationConfig{}, fmt.Errorf(
			"QIANWEN_TIMEOUT must be greater than zero and at most %s",
			maximumTextTimeout,
		)
	}

	maxOutputTokens, err := positiveIntOrDefault(
		"QIANWEN_MAX_OUTPUT_TOKENS",
		defaultTextMaxOutputTokens,
	)
	if err != nil {
		return TextGenerationConfig{}, err
	}
	if maxOutputTokens > maximumTextOutputTokens {
		return TextGenerationConfig{}, fmt.Errorf(
			"QIANWEN_MAX_OUTPUT_TOKENS must be at most %d",
			maximumTextOutputTokens,
		)
	}
	maxContextChars, err := positiveIntOrDefault(
		"AGENT_CONTEXT_MAX_CHARACTERS",
		defaultAgentContextChars,
	)
	if err != nil {
		return TextGenerationConfig{}, err
	}
	if maxContextChars < minimumAgentContextChars ||
		maxContextChars > maximumAgentContextChars {
		return TextGenerationConfig{}, fmt.Errorf(
			"AGENT_CONTEXT_MAX_CHARACTERS must be between %d and %d",
			minimumAgentContextChars,
			maximumAgentContextChars,
		)
	}

	return TextGenerationConfig{
		Provider:            provider,
		BaseURL:             baseURL,
		Model:               model,
		SpeechFeedbackModel: speechFeedbackModel,
		Timeout:             timeout,
		MaxOutputTokens:     maxOutputTokens,
		MaxContextChars:     maxContextChars,
		APIKey:              Secret{value: apiKey},
	}, nil
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid Go duration", name)
	}
	return value, nil
}

func positiveIntOrDefault(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
