package title

import "time"

const (
	promptVersion       = "thread-title-prompt-v1"
	workerLeaseDuration = 2 * time.Minute
)

func NewProcessor(
	repository JobRepository,
	generator Generator,
	provider string,
	model string,
) (Processor, error) {
	configuration := Configuration{
		PromptVersion: promptVersion,
		Provider:      provider,
		Model:         model,
	}
	service, err := NewService(generator, configuration)
	if err != nil {
		return nil, err
	}
	return NewWorker(
		repository,
		service,
		WorkerConfiguration{
			LeaseDuration: workerLeaseDuration,
			MaxAttempts:   DefaultWorkerMaxAttempts,
			Generation:    configuration,
		},
	)
}
