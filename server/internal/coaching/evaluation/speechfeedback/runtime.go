package speechfeedback

import "time"

const (
	workerMaxAttempts = 3
	workerRetryDelay  = 2 * time.Second
)

// NewWorkerConfiguration owns the SpeechFeedback processing policy while
// accepting only deployment-specific model and lease inputs.
func NewWorkerConfiguration(
	provider string,
	model string,
	leaseDuration time.Duration,
) (SpeechFeedbackWorkerConfiguration, error) {
	configuration := SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     workerMaxAttempts,
		LeaseDuration:   leaseDuration,
		RetryDelay:      workerRetryDelay,
		StrategyRef:     SpeechFeedbackStrategyRef,
		PipelineVersion: SpeechFeedbackPipelineVersion,
		PromptVersion:   SpeechFeedbackPromptVersion,
		Provider:        provider,
		Model:           model,
	}
	if !configuration.Valid() {
		return SpeechFeedbackWorkerConfiguration{}, ErrInvalidSpeechFeedback
	}
	return configuration, nil
}
