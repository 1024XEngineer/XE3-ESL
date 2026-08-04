package memory

import "context"

// GenerationRequest is the complete data Memory permits a model provider to
// receive for one extraction. GenerateJSON makes the required response mode
// part of this Port instead of provider or Bootstrap policy.
type GenerationRequest struct {
	SystemPrompt string
	UserPrompt   string
}

type GenerationResult struct {
	Provider string
	Model    string
	Content  string
}

type Generator interface {
	GenerateJSON(
		context.Context,
		GenerationRequest,
	) (GenerationResult, error)
}

type ProviderFailure interface {
	error
	StableCategory() string
	Retryable() bool
}
