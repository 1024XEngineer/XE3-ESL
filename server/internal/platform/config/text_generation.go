package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
)

const (
	TextProviderQianwen = "qianwen"
	TextProviderQiniu   = "qiniu"

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
	EvaluationModel     string
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
	if provider != TextProviderQianwen && provider != TextProviderQiniu {
		return TextGenerationConfig{}, errors.New("TEXT_GENERATION_PROVIDER is not supported")
	}

	prefix := "QIANWEN"
	apiKeyName := "DASHSCOPE_API_KEY"
	if provider == TextProviderQiniu {
		prefix = "QINIU_AI"
		apiKeyName = "QINIU_AI_API_KEY"
	}
	baseURLName := prefix + "_BASE_URL"
	modelName := prefix + "_MODEL"
	evaluationModelName := prefix + "_EVALUATION_MODEL"
	speechFeedbackModelName := prefix + "_SPEECH_FEEDBACK_MODEL"
	timeoutName := prefix + "_TIMEOUT"
	maxOutputTokensName := prefix + "_MAX_OUTPUT_TOKENS"

	baseURL := strings.TrimSpace(os.Getenv(baseURLName))
	if baseURL == "" {
		return TextGenerationConfig{}, fmt.Errorf("%s is required", baseURLName)
	}
	model := os.Getenv(modelName)
	if !modelid.Valid(model) {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s must be a valid model ID",
			modelName,
		)
	}
	evaluationModel := os.Getenv(evaluationModelName)
	if !modelid.Valid(evaluationModel) {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s must be a valid model ID",
			evaluationModelName,
		)
	}
	speechFeedbackModel := os.Getenv(speechFeedbackModelName)
	if !modelid.Valid(speechFeedbackModel) {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s must be a valid model ID",
			speechFeedbackModelName,
		)
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyName))
	if apiKey == "" {
		return TextGenerationConfig{}, fmt.Errorf("%s is required", apiKeyName)
	}
	if strings.IndexFunc(apiKey, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s contains whitespace or control characters",
			apiKeyName,
		)
	}

	timeout, err := durationOrDefault(timeoutName, defaultTextTimeout)
	if err != nil {
		return TextGenerationConfig{}, err
	}
	if timeout <= 0 || timeout > maximumTextTimeout {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s must be greater than zero and at most %s",
			timeoutName,
			maximumTextTimeout,
		)
	}

	maxOutputTokens, err := positiveIntOrDefault(
		maxOutputTokensName,
		defaultTextMaxOutputTokens,
	)
	if err != nil {
		return TextGenerationConfig{}, err
	}
	if maxOutputTokens > maximumTextOutputTokens {
		return TextGenerationConfig{}, fmt.Errorf(
			"%s must be at most %d",
			maxOutputTokensName,
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
		EvaluationModel:     evaluationModel,
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
