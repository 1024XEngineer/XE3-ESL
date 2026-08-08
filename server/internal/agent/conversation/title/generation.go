package title

import "context"

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

type GenerationFailure interface {
	error
	StableCategory() string
	Retryable() bool
}
