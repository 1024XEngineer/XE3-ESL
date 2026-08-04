package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

type EvaluationScoringGenerator struct {
	generator ai.TextGenerator
}

func NewEvaluationScoringGenerator(
	generator ai.TextGenerator,
) (*EvaluationScoringGenerator, error) {
	if generator == nil {
		return nil, errors.New(
			"qianwen: Evaluation scoring generator is required",
		)
	}
	return &EvaluationScoringGenerator{generator: generator}, nil
}

func (generator *EvaluationScoringGenerator) Generate(
	ctx context.Context,
	request scoring.TextGenerationRequest,
) (scoring.TextGenerationResult, error) {
	if generator == nil || generator.generator == nil || ctx == nil ||
		request.SystemPrompt == "" || request.UserPrompt == "" {
		return scoring.TextGenerationResult{}, errors.New(
			"qianwen: invalid Evaluation scoring request",
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
		return scoring.TextGenerationResult{}, err
	}
	return scoring.TextGenerationResult{
		RequestID: result.ID,
		Content:   result.Content,
		Provider:  result.Provider,
		Model:     result.Model,
	}, nil
}

var _ scoring.TextGenerator = (*EvaluationScoringGenerator)(nil)
