package preparation

import "context"

type JobTargetGenerationRequest struct {
	SystemInstruction string
	UserMaterial      string
}

type JobTargetGenerationResult struct {
	Content string
}

// JobTargetGenerator is Preparation's data-only model boundary. The provider
// receives no tools, repositories, identity, or resource access.
type JobTargetGenerator interface {
	GenerateJobTarget(
		context.Context,
		JobTargetGenerationRequest,
	) (JobTargetGenerationResult, error)
}

type JobTargetGenerationFailure interface {
	error
	StableCategory() string
}
