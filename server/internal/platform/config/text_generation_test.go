package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLoadTextGeneration(t *testing.T) {
	setRequiredTextGenerationEnvironment(t)
	t.Setenv("QIANWEN_TIMEOUT", "45s")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "768")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "24000")

	cfg, err := LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	if cfg.Provider != TextProviderQianwen ||
		cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" ||
		cfg.Model != "qwen-test-model" ||
		cfg.SpeechFeedbackModel != "qwen-test-feedback-model" ||
		cfg.Timeout != 45*time.Second ||
		cfg.MaxOutputTokens != 768 ||
		cfg.MaxContextChars != 24000 ||
		cfg.APIKey.Reveal() != "test-secret-value" {
		t.Fatalf("unexpected text generation config: %#v", cfg)
	}
}

func TestLoadTextGenerationUsesSafeOperationalDefaults(t *testing.T) {
	setRequiredTextGenerationEnvironment(t)
	t.Setenv("QIANWEN_TIMEOUT", "")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "")

	cfg, err := LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	if cfg.Timeout != defaultTextTimeout {
		t.Fatalf("timeout = %s, want %s", cfg.Timeout, defaultTextTimeout)
	}
	if cfg.MaxOutputTokens != 8192 {
		t.Fatalf(
			"max output tokens = %d, want %d",
			cfg.MaxOutputTokens,
			8192,
		)
	}
	if cfg.MaxContextChars != defaultAgentContextChars {
		t.Fatalf(
			"context budget = %d, want %d",
			cfg.MaxContextChars,
			defaultAgentContextChars,
		)
	}
}

func TestLoadTextGenerationReadsQiniuConfiguration(t *testing.T) {
	t.Setenv("TEXT_GENERATION_PROVIDER", TextProviderQiniu)
	t.Setenv("QINIU_AI_BASE_URL", "https://api.qnaigc.com/v1")
	t.Setenv("QINIU_AI_MODEL", "moonshotai/kimi-k2.6")
	t.Setenv("QINIU_AI_SPEECH_FEEDBACK_MODEL", "gemini-2.5-flash")
	t.Setenv("QINIU_AI_TIMEOUT", "45s")
	t.Setenv("QINIU_AI_MAX_OUTPUT_TOKENS", "768")
	t.Setenv("QINIU_AI_API_KEY", "qiniu-test-secret")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "24000")

	cfg, err := LoadTextGeneration()
	if err != nil {
		t.Fatalf("load Qiniu text generation config: %v", err)
	}
	if cfg.Provider != TextProviderQiniu ||
		cfg.BaseURL != "https://api.qnaigc.com/v1" ||
		cfg.Model != "moonshotai/kimi-k2.6" ||
		cfg.SpeechFeedbackModel != "gemini-2.5-flash" ||
		cfg.Timeout != 45*time.Second ||
		cfg.MaxOutputTokens != 768 ||
		cfg.MaxContextChars != 24000 ||
		cfg.APIKey.Reveal() != "qiniu-test-secret" {
		t.Fatalf("unexpected Qiniu text generation config: %#v", cfg)
	}
}

func TestLoadTextGenerationRejectsUnsafeQiniuConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing base URL", key: "QINIU_AI_BASE_URL", value: ""},
		{name: "missing model", key: "QINIU_AI_MODEL", value: ""},
		{name: "model with leading whitespace", key: "QINIU_AI_MODEL", value: " moonshotai/kimi-k2.6"},
		{name: "model with repeated separator", key: "QINIU_AI_MODEL", value: "moonshotai//kimi-k2.6"},
		{name: "model with traversal", key: "QINIU_AI_MODEL", value: "moonshotai/../kimi-k2.6"},
		{name: "missing feedback model", key: "QINIU_AI_SPEECH_FEEDBACK_MODEL", value: ""},
		{name: "invalid feedback model", key: "QINIU_AI_SPEECH_FEEDBACK_MODEL", value: "gemini//2.5-flash"},
		{name: "missing API key", key: "QINIU_AI_API_KEY", value: ""},
		{name: "API key whitespace", key: "QINIU_AI_API_KEY", value: "secret value"},
		{name: "invalid timeout", key: "QINIU_AI_TIMEOUT", value: "soon"},
		{name: "invalid output budget", key: "QINIU_AI_MAX_OUTPUT_TOKENS", value: "many"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredQiniuTextGenerationEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadTextGeneration(); err == nil {
				t.Fatal("expected Qiniu configuration error")
			}
		})
	}
}

func TestLoadTextGenerationRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing provider", key: "TEXT_GENERATION_PROVIDER", value: ""},
		{name: "unsupported provider", key: "TEXT_GENERATION_PROVIDER", value: "fake"},
		{name: "missing base URL", key: "QIANWEN_BASE_URL", value: ""},
		{name: "missing model", key: "QIANWEN_MODEL", value: ""},
		{name: "missing speech feedback model", key: "QIANWEN_SPEECH_FEEDBACK_MODEL", value: ""},
		{name: "missing API key", key: "DASHSCOPE_API_KEY", value: ""},
		{name: "API key whitespace", key: "DASHSCOPE_API_KEY", value: "secret value"},
		{name: "invalid timeout", key: "QIANWEN_TIMEOUT", value: "soon"},
		{name: "zero timeout", key: "QIANWEN_TIMEOUT", value: "0s"},
		{name: "excessive timeout", key: "QIANWEN_TIMEOUT", value: "301s"},
		{name: "invalid output budget", key: "QIANWEN_MAX_OUTPUT_TOKENS", value: "many"},
		{name: "zero output budget", key: "QIANWEN_MAX_OUTPUT_TOKENS", value: "0"},
		{name: "small context budget", key: "AGENT_CONTEXT_MAX_CHARACTERS", value: "4999"},
		{name: "invalid context budget", key: "AGENT_CONTEXT_MAX_CHARACTERS", value: "many"},
		{
			name:  "excessive output budget",
			key:   "QIANWEN_MAX_OUTPUT_TOKENS",
			value: "1000001",
		},
		{
			name:  "excessive context budget",
			key:   "AGENT_CONTEXT_MAX_CHARACTERS",
			value: "1000001",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredTextGenerationEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadTextGeneration(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestSecretIsRedactedByCommonFormatters(t *testing.T) {
	secret := Secret{value: "must-never-be-logged"}
	configuration := TextGenerationConfig{
		Provider: TextProviderQianwen,
		APIKey:   secret,
	}
	formatted := []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%+v", configuration),
		fmt.Sprintf("%#v", configuration),
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	formatted = append(formatted, string(encoded))
	encoded, err = json.Marshal(configuration)
	if err != nil {
		t.Fatalf("marshal text generation configuration: %v", err)
	}
	formatted = append(formatted, string(encoded))

	for _, value := range formatted {
		if strings.Contains(value, secret.value) {
			t.Fatalf("formatter exposed secret: %q", value)
		}
	}
}

func setRequiredTextGenerationEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("TEXT_GENERATION_PROVIDER", "qianwen")
	t.Setenv("QIANWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	t.Setenv("QIANWEN_MODEL", "qwen-test-model")
	t.Setenv("QIANWEN_SPEECH_FEEDBACK_MODEL", "qwen-test-feedback-model")
	t.Setenv("QIANWEN_TIMEOUT", "")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "")
	t.Setenv("DASHSCOPE_API_KEY", "test-secret-value")
}

func setRequiredQiniuTextGenerationEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("TEXT_GENERATION_PROVIDER", TextProviderQiniu)
	t.Setenv("QINIU_AI_BASE_URL", "https://api.qnaigc.com/v1")
	t.Setenv("QINIU_AI_MODEL", "moonshotai/kimi-k2.6")
	t.Setenv("QINIU_AI_SPEECH_FEEDBACK_MODEL", "gemini-2.5-flash")
	t.Setenv("QINIU_AI_TIMEOUT", "")
	t.Setenv("QINIU_AI_MAX_OUTPUT_TOKENS", "")
	t.Setenv("QINIU_AI_API_KEY", "qiniu-test-secret")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "")
}
