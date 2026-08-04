package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

type EvaluationSpeechFeedbackGenerator struct {
	generator ai.TextGenerator
}

func NewEvaluationSpeechFeedbackGenerator(
	generator ai.TextGenerator,
) (*EvaluationSpeechFeedbackGenerator, error) {
	if generator == nil {
		return nil, errors.New(
			"qianwen: Evaluation speech feedback generator is required",
		)
	}
	return &EvaluationSpeechFeedbackGenerator{generator: generator}, nil
}

func (generator *EvaluationSpeechFeedbackGenerator) Generate(
	ctx context.Context,
	request speechfeedback.TextGenerationRequest,
) (speechfeedback.TextGenerationResult, error) {
	if generator == nil || generator.generator == nil || ctx == nil ||
		request.SystemPrompt == "" || request.UserPrompt == "" {
		return speechfeedback.TextGenerationResult{}, errors.New(
			"qianwen: invalid Evaluation speech feedback request",
		)
	}
	result, err := generator.generator.Generate(ctx, ai.TextRequest{
		Messages: []ai.TextMessage{
			{Role: ai.TextRoleSystem, Content: request.SystemPrompt},
			{Role: ai.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: ai.TextResponseFormatJSON,
	})
	if err != nil {
		return speechfeedback.TextGenerationResult{}, err
	}
	return speechfeedback.TextGenerationResult{
		RequestID: result.ID,
		Content:   result.Content,
		Provider:  result.Provider,
		Model:     result.Model,
	}, nil
}

var _ speechfeedback.TextGenerator = (*EvaluationSpeechFeedbackGenerator)(nil)
