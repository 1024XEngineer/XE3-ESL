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

	cfg, err := LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	if cfg.Provider != TextProviderQianwen ||
		cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" ||
		cfg.Model != "qwen-test-model" ||
		cfg.Timeout != 45*time.Second ||
		cfg.MaxOutputTokens != 768 ||
		cfg.APIKey.Reveal() != "test-secret-value" {
		t.Fatalf("unexpected text generation config: %#v", cfg)
	}
}

func TestLoadTextGenerationUsesSafeOperationalDefaults(t *testing.T) {
	setRequiredTextGenerationEnvironment(t)
	t.Setenv("QIANWEN_TIMEOUT", "")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "")

	cfg, err := LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	if cfg.Timeout != defaultTextTimeout {
		t.Fatalf("timeout = %s, want %s", cfg.Timeout, defaultTextTimeout)
	}
	if cfg.MaxOutputTokens != defaultTextMaxOutputTokens {
		t.Fatalf(
			"max output tokens = %d, want %d",
			cfg.MaxOutputTokens,
			defaultTextMaxOutputTokens,
		)
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
		{name: "missing API key", key: "DASHSCOPE_API_KEY", value: ""},
		{name: "API key whitespace", key: "DASHSCOPE_API_KEY", value: "secret value"},
		{name: "invalid timeout", key: "QIANWEN_TIMEOUT", value: "soon"},
		{name: "zero timeout", key: "QIANWEN_TIMEOUT", value: "0s"},
		{name: "excessive timeout", key: "QIANWEN_TIMEOUT", value: "301s"},
		{name: "invalid output budget", key: "QIANWEN_MAX_OUTPUT_TOKENS", value: "many"},
		{name: "zero output budget", key: "QIANWEN_MAX_OUTPUT_TOKENS", value: "0"},
		{
			name:  "excessive output budget",
			key:   "QIANWEN_MAX_OUTPUT_TOKENS",
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
	formatted := []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
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
	t.Setenv("QIANWEN_TIMEOUT", "")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "")
	t.Setenv("DASHSCOPE_API_KEY", "test-secret-value")
}
