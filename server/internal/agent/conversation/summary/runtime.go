package summary

import "time"

const workerLeaseDuration = 2 * time.Minute

func NewProcessor(
	repository Repository,
	generator Generator,
	provider string,
	model string,
	maxContextCharacters int,
) (Processor, error) {
	configuration := Configuration{Provider: provider, Model: model}
	service, err := NewGeneratorService(generator, configuration)
	if err != nil {
		return nil, err
	}
	return NewWorker(repository, service, WorkerConfiguration{
		MaxContextCharacters: maxContextCharacters,
		LeaseDuration:        workerLeaseDuration,
		MaxAttempts:          DefaultWorkerMaxAttempts,
		Generation:           configuration,
	})
}
