package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type EvaluationSpeechFeedbackGenerator struct {
	generator *textClient
}

func NewEvaluationSpeechFeedbackGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationSpeechFeedbackGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
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
	result, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: protocol.TextResponseFormatJSON,
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
