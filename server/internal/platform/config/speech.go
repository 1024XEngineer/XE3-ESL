package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	SpeechProviderQianwen = "qianwen"

	qianwenRealtimeASRModel = "qwen-audio-3.0-asr-flash-streaming"
	qianwenRecordedASRModel = "qwen-audio-3.0-asr-flash"

	defaultASRTimeout         = 150 * time.Second
	minimumRealtimeASRTimeout = 150 * time.Second
	defaultTTSTimeout         = 60 * time.Second
	maximumSpeechTimeout      = 5 * time.Minute
)

type SpeechRecognitionConfig struct {
	Provider        string
	BaseURL         string
	Model           string
	Timeout         time.Duration
	RecordedModel   string
	RecordedTimeout time.Duration
	APIKey          Secret
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
	provider, err := requiredQianwenProvider("SPEECH_RECOGNITION_PROVIDER")
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	baseURL, err := requiredEnvironment("QIANWEN_ASR_BASE_URL")
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	model, err := requiredQianwenASRModel(
		"QIANWEN_ASR_MODEL",
		qianwenRealtimeASRModel,
	)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	timeout, err := speechTimeoutOrDefault("QIANWEN_ASR_TIMEOUT", defaultASRTimeout)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	if timeout < minimumRealtimeASRTimeout {
		return SpeechRecognitionConfig{}, fmt.Errorf(
			"QIANWEN_ASR_TIMEOUT must be at least %s for realtime recognition",
			minimumRealtimeASRTimeout,
		)
	}
	recordedModel, err := requiredQianwenASRModel(
		"QIANWEN_ASR_RECORDED_MODEL",
		qianwenRecordedASRModel,
	)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	recordedTimeout, err := speechTimeoutOrDefault(
		"QIANWEN_ASR_RECORDED_TIMEOUT",
		defaultASRTimeout,
	)
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	apiKey, err := loadDashScopeAPIKey()
	if err != nil {
		return SpeechRecognitionConfig{}, err
	}
	return SpeechRecognitionConfig{
		Provider:        provider,
		BaseURL:         baseURL,
		Model:           model,
		Timeout:         timeout,
		RecordedModel:   recordedModel,
		RecordedTimeout: recordedTimeout,
		APIKey:          apiKey,
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
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if provider == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if provider != SpeechProviderQianwen {
		return "", fmt.Errorf("%s is not supported", name)
	}
	return provider, nil
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func requiredQianwenASRModel(name string, expected string) (string, error) {
	model, err := requiredEnvironment(name)
	if err != nil {
		return "", err
	}
	model = strings.ToLower(model)
	if model != expected {
		return "", fmt.Errorf("%s must be %s", name, expected)
	}
	return model, nil
}

func loadDashScopeAPIKey() (Secret, error) {
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		return Secret{}, errors.New("DASHSCOPE_API_KEY is required")
	}
	if strings.IndexFunc(apiKey, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return Secret{}, errors.New("DASHSCOPE_API_KEY contains whitespace or control characters")
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
