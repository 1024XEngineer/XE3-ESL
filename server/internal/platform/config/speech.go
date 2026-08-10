package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	SpeechProviderQianwen = "qianwen"
	SpeechProviderQiniu   = "qiniu"

	defaultASRTimeout    = 90 * time.Second
	defaultTTSTimeout    = 60 * time.Second
	maximumSpeechTimeout = 5 * time.Minute
)

type SpeechRecognitionConfig struct {
	Provider string
	BaseURL  string
	Model    string
	Timeout  time.Duration
	APIKey   Secret
}

type SpeechSynthesisConfig struct {
	Provider      string
	BaseURL       string
	Model         string
	Voice         string
	LanguageHint  string
	Timeout       time.Duration
	TempDirectory string
	APIKey        Secret
}

func LoadSpeechRecognition() (SpeechRecognitionConfig, error) {
	provider, err := requiredSpeechProvider(
		"SPEECH_RECOGNITION_PROVIDER",
		SpeechProviderQianwen,
		SpeechProviderQiniu,
	)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	prefix := "QIANWEN_ASR"
	if provider == SpeechProviderQiniu {
		prefix = "QINIU_ASR"
	}
	baseURL, err := requiredEnvironment(prefix + "_BASE_URL")
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	model, err := requiredEnvironment(prefix + "_MODEL")
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	timeout, err := speechTimeoutOrDefault(prefix+"_TIMEOUT", defaultASRTimeout)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	apiKey, err := loadSpeechAPIKey(provider)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	return SpeechRecognitionConfig{
		Provider: provider,
		BaseURL:  baseURL,
		Model:    model,
		Timeout:  timeout,
		APIKey:   apiKey,
	}, nil
}

func LoadSpeechSynthesis() (SpeechSynthesisConfig, error) {
	provider, err := requiredQianwenProvider("SPEECH_SYNTHESIS_PROVIDER")
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	baseURL, err := requiredEnvironment("QIANWEN_TTS_BASE_URL")
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	model, err := requiredEnvironment("QIANWEN_TTS_MODEL")
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	voice, err := requiredEnvironment("QIANWEN_TTS_VOICE")
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	languageHint, err := requiredEnvironment("QIANWEN_TTS_LANGUAGE")
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	timeout, err := speechTimeoutOrDefault("QIANWEN_TTS_TIMEOUT", defaultTTSTimeout)
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	apiKey, err := loadDashScopeAPIKey()
	if err != nil {
		return SpeechSynthesisConfig{}, err
	}
	return SpeechSynthesisConfig{
		Provider:      provider,
		BaseURL:       baseURL,
		Model:         model,
		Voice:         voice,
		LanguageHint:  languageHint,
		Timeout:       timeout,
		TempDirectory: strings.TrimSpace(os.Getenv("QIANWEN_TTS_TEMP_DIRECTORY")),
		APIKey:        apiKey,
	}, nil
}

func requiredQianwenProvider(name string) (string, error) {
	return requiredSpeechProvider(name, SpeechProviderQianwen)
}

func requiredSpeechProvider(name string, allowed ...string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if provider == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	for _, candidate := range allowed {
		if provider == candidate {
			return provider, nil
		}
	}
	return "", fmt.Errorf("%s is not supported", name)
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func loadDashScopeAPIKey() (Secret, error) {
	return loadProviderAPIKey("DASHSCOPE_API_KEY")
}

func loadSpeechAPIKey(provider string) (Secret, error) {
	if provider == SpeechProviderQiniu {
		return loadProviderAPIKey("QINIU_AI_API_KEY")
	}
	return loadDashScopeAPIKey()
}

func loadProviderAPIKey(name string) (Secret, error) {
	apiKey := strings.TrimSpace(os.Getenv(name))
	if apiKey == "" {
		return Secret{}, fmt.Errorf("%s is required", name)
	}
	if strings.IndexFunc(apiKey, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return Secret{}, fmt.Errorf(
			"%s contains whitespace or control characters",
			name,
		)
	}
	return Secret{value: apiKey}, nil
}

func speechTimeoutOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	timeout, err := durationOrDefault(name, fallback)
	if err != nil {
		return 0, err
	}
	if timeout <= 0 || timeout > maximumSpeechTimeout {
		return 0, fmt.Errorf(
			"%s must be greater than zero and at most %s",
			name,
			maximumSpeechTimeout,
		)
	}
	return timeout, nil
}
