package fieldextractor

import "context"

// MinimumGenerationOutputTokens is the independent lower bound required for a
// complete Resume field document. Provider composition must not replace it
// with the Agent model's output budget.
const MinimumGenerationOutputTokens = 4096

type GenerationRequest struct {
	SystemPrompt        string
	DocumentPayload     string
	MinimumOutputTokens int
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
}
