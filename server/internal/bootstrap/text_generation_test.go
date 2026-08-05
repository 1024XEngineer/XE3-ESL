package bootstrap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestNewAgentAndPreparationGeneratorsRegisterConfiguredQianwen(
	t *testing.T,
) {
	setBootstrapTextGenerationEnvironment(t, "server-only-test-key")

	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	providers, err := NewAgentModelProviders(configuration)
	if err != nil {
		t.Fatalf("register Agent model providers: %v", err)
	}
	if providers.Run == nil || providers.Memory == nil || providers.Summary == nil {
		t.Fatalf("Agent model providers are incomplete: %#v", providers)
	}
	jobTargetGenerator, err := NewPreparationJobTargetGenerator(configuration)
	if err != nil {
		t.Fatalf("register Preparation model provider: %v", err)
	}
	if jobTargetGenerator == nil {
		t.Fatal("Preparation model provider is nil")
	}
	resumeGenerator, err := NewResumeFieldGenerator(configuration)
	if err != nil {
		t.Fatalf("register Resume model provider: %v", err)
	}
	if resumeGenerator == nil {
		t.Fatal("Resume model provider is nil")
	}
}

func TestNewResumeFieldGeneratorRejectsInsufficientBudget(t *testing.T) {
	setBootstrapTextGenerationEnvironment(t, "server-only-test-key")

	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	configuration.MaxOutputTokens = 1
	generator, err := NewResumeFieldGenerator(configuration)
	if err == nil || generator != nil {
		t.Fatalf(
			"insufficient Resume budget returned generator=%T error=%v",
			generator,
			err,
		)
	}
}

func TestExplicitModelPortsRejectUnregisteredProviderWithoutFallback(
	t *testing.T,
) {
	configuration := config.TextGenerationConfig{Provider: "fake"}
	if providers, err := NewAgentModelProviders(configuration); err == nil ||
		providers.Run != nil || providers.Memory != nil || providers.Summary != nil {
		t.Fatalf(
			"unregistered provider returned Agent ports=%#v error=%v",
			providers,
			err,
		)
	}
	if generator, err := NewPreparationJobTargetGenerator(configuration); err == nil || generator != nil {
		t.Fatalf(
			"unregistered provider returned Preparation port=%T error=%v",
			generator,
			err,
		)
	}
	if generator, err := NewEvaluationScoringGenerator(configuration); err == nil || generator != nil {
		t.Fatalf(
			"unregistered provider returned Evaluation scoring port=%T error=%v",
			generator,
			err,
		)
	}
	if generator, err := NewEvaluationSpeechFeedbackGenerator(configuration); err == nil || generator != nil {
		t.Fatalf(
			"unregistered provider returned Evaluation feedback port=%T error=%v",
			generator,
			err,
		)
	}
	if generator, err := NewResumeFieldGenerator(configuration); err == nil || generator != nil {
		t.Fatalf(
			"unregistered provider returned Resume port=%T error=%v",
			generator,
			err,
		)
	}
}

func TestNewAgentModelProvidersDoesNotExposeAPIKeyInStartupError(t *testing.T) {
	const apiKey = "must-never-appear"
	setBootstrapTextGenerationEnvironment(t, apiKey)
	t.Setenv("QIANWEN_BASE_URL", "https://example.com/compatible-mode/v1")

	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	_, err = NewAgentModelProviders(configuration)
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
