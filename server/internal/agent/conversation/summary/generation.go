package summary

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

// Generator owns Summary's strict JSON generation boundary.
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
