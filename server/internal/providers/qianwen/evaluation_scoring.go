package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type EvaluationScoringGenerator struct {
	generator *textClient
}

func NewEvaluationScoringGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationScoringGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
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
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: protocol.TextResponseFormatJSON,
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
