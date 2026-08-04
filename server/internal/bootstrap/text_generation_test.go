package bootstrap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

func TestNewTextGeneratorRegistersOnlyConfiguredQianwen(t *testing.T) {
	setBootstrapTextGenerationEnvironment(t, "server-only-test-key")

	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	generator, err := NewTextGenerator(configuration)
	if err != nil {
		t.Fatalf("register text generator: %v", err)
	}
	if _, ok := generator.(*qianwen.Generator); !ok {
		t.Fatalf("registered generator has unexpected type %T", generator)
	}
}

func TestNewTextGeneratorRejectsUnregisteredProviderWithoutFallback(
	t *testing.T,
) {
	configuration := config.TextGenerationConfig{Provider: "fake"}
	if generator, err := NewTextGenerator(configuration); err == nil ||
		generator != nil {
		t.Fatalf(
			"unregistered provider returned generator=%T error=%v",
			generator,
			err,
		)
	}
}

func TestNewTextGeneratorDoesNotExposeAPIKeyInStartupError(t *testing.T) {
	const apiKey = "must-never-appear"
	setBootstrapTextGenerationEnvironment(t, apiKey)
	t.Setenv("QIANWEN_BASE_URL", "https://example.com/compatible-mode/v1")

	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	_, err = NewTextGenerator(configuration)
	if err == nil {
		t.Fatal("expected unsafe endpoint to be rejected")
	}
	if formatted := fmt.Sprint(err); strings.Contains(formatted, apiKey) {
		t.Fatalf("startup error exposed API key: %q", formatted)
	}
}

func setBootstrapTextGenerationEnvironment(t *testing.T, apiKey string) {
	t.Helper()
	t.Setenv("TEXT_GENERATION_PROVIDER", config.TextProviderQianwen)
	t.Setenv(
		"QIANWEN_BASE_URL",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
	)
	t.Setenv("QIANWEN_MODEL", "qwen3.5-flash")
	t.Setenv("QIANWEN_TIMEOUT", "")
	t.Setenv("QIANWEN_MAX_OUTPUT_TOKENS", "")
	t.Setenv("AGENT_CONTEXT_MAX_CHARACTERS", "")
	t.Setenv("DASHSCOPE_API_KEY", apiKey)
}
