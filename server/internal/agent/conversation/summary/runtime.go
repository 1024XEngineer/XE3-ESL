package summary

import "time"

const (
	summaryPolicyVersion = "summary-policy-v1"
	summaryPromptVersion = "summary-prompt-v1"
	workerLeaseDuration  = 2 * time.Minute
)

// NewProcessor builds the production summary service and worker with policy
// owned by Conversation Summary rather than startup wiring.
func NewProcessor(
	repository interface {
		Repository
		JobRepository
	},
	generator Generator,
	provider string,
	model string,
) (Processor, error) {
	configuration := Configuration{
		PolicyVersion: summaryPolicyVersion,
		PromptVersion: summaryPromptVersion,
		Provider:      provider,
		Model:         model,
	}
	service, err := NewService(repository, generator, configuration)
	if err != nil {
		return nil, err
	}
	return NewWorker(
		repository,
		repository,
		service,
		WorkerConfiguration{
			TriggerPolicyVersion: TriggerPolicyV2,
			TriggerMessages:      DefaultTriggerMessages,
			RetainRecentMessages: DefaultRetainedMessages,
			LeaseDuration:        workerLeaseDuration,
			MaxAttempts:          DefaultWorkerMaxAttempts,
			Summary:              configuration,
		},
	)
}
