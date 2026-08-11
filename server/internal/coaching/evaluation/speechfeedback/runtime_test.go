package speechfeedback

import (
	"errors"
	"testing"
	"time"
)

func TestNewWorkerConfigurationOwnsProductionPolicy(t *testing.T) {
	t.Parallel()

	configuration, err := NewWorkerConfiguration(
		"qianwen",
		"qwen-plus",
		30*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Valid() ||
		configuration.MaxAttempts != workerMaxAttempts ||
		configuration.RetryDelay != workerRetryDelay ||
		configuration.StrategyRef != SpeechFeedbackStrategyRef ||
		configuration.PipelineVersion != SpeechFeedbackPipelineVersion ||
		configuration.PromptVersion != SpeechFeedbackPromptVersion {
		t.Fatalf("worker configuration = %#v", configuration)
	}
}

func TestNewWorkerConfigurationRejectsInvalidDeploymentInput(t *testing.T) {
	t.Parallel()

	_, err := NewWorkerConfiguration("qianwen", "qwen-plus", 0)
	if !errors.Is(err, ErrInvalidSpeechFeedback) {
		t.Fatalf("NewWorkerConfiguration error = %v", err)
	}
}

func TestNewWorkerConfigurationAcceptsMaximumProviderBudget(t *testing.T) {
	t.Parallel()

	_, err := NewWorkerConfiguration(
		"qianwen",
		"qwen-plus",
		11*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewWorkerConfiguration: %v", err)
	}
}
