package scoring

import "context"

type TextGenerationOutputContract string

const (
	TextGenerationOutputDefault                  TextGenerationOutputContract = ""
	TextGenerationOutputIELTSSpeakingCriterionV3 TextGenerationOutputContract = IELTSSpeakingShadowProviderSchemaVersion
)

type TextGenerationRequest struct {
	SystemPrompt    string
	UserPrompt      string
	OutputContract  TextGenerationOutputContract
	OutputCriterion IELTSCriterion
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
