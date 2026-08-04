package scoring

import "context"

type TextGenerationRequest struct {
	SystemPrompt string
	UserPrompt   string
}

type TextGenerationResult struct {
	RequestID string
	Content   string
	Provider  string
	Model     string
}

type TextGenerator interface {
	Generate(
		context.Context,
		TextGenerationRequest,
	) (TextGenerationResult, error)
}

type GenerationFailure interface {
	error
	StableCategory() string
	Retryable() bool
}
