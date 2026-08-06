package qianwen

import (
	"context"
	"errors"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

// AgentRunGenerator adapts Qianwen's private chat protocol to Agent Run's
// model boundary. Run remains the only owner of Tool and multimodal semantics.
type AgentRunGenerator struct {
	generator *textClient
}

func NewAgentRunGenerator(
	configuration TextConfig,
	apiKey string,
) (*AgentRunGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &AgentRunGenerator{generator: generator}, nil
}

func (generator *AgentRunGenerator) Generate(
	ctx context.Context,
	request agentrun.TextRequest,
) (agentrun.TextResult, error) {
	if generator == nil || generator.generator == nil {
		return agentrun.TextResult{}, agentrun.NewGenerationError(
			agentrun.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: Agent Run generator is required"),
		)
	}
	providerRequest, err := agentRunRequest(request)
	if err != nil {
		return agentrun.TextResult{}, err
	}
	result, err := generator.generator.Generate(ctx, providerRequest)
	if err != nil {
		return agentrun.TextResult{}, mapAgentRunError(err)
	}
	return agentRunResult(result), nil
}

func (generator *AgentRunGenerator) GenerateStream(
	ctx context.Context,
	request agentrun.TextRequest,
	observer agentrun.TextDeltaObserver,
) (agentrun.TextResult, error) {
	if generator == nil || generator.generator == nil || observer == nil {
		return agentrun.TextResult{}, agentrun.NewGenerationError(
			agentrun.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: streaming Agent Run generator is required"),
		)
	}
	providerRequest, err := agentRunRequest(request)
	if err != nil {
		return agentrun.TextResult{}, err
	}
	result, err := generator.generator.GenerateStream(
		ctx,
		providerRequest,
		agentRunDeltaObserver{observer: observer},
	)
	if err != nil {
		return agentrun.TextResult{}, mapAgentRunError(err)
	}
	return agentRunResult(result), nil
}

type agentRunDeltaObserver struct {
	observer agentrun.TextDeltaObserver
}

func (observer agentRunDeltaObserver) OnTextDelta(
	ctx context.Context,
	delta string,
) error {
	return observer.observer.OnTextDelta(ctx, delta)
}

func agentRunRequest(
	request agentrun.TextRequest,
) (protocol.TextRequest, error) {
	if err := agentrun.ValidateTextRequest(request); err != nil {
		return protocol.TextRequest{}, agentrun.NewGenerationError(
			agentrun.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	result := protocol.TextRequest{
		Messages: make([]protocol.TextMessage, 0, len(request.Messages)),
		Tools:    make([]protocol.ToolDefinition, 0, len(request.Tools)),
		ToolChoice: protocol.ToolChoice{
			Mode: protocol.ToolChoiceMode(request.ToolChoice.Mode),
			Name: request.ToolChoice.Name,
		},
		ResponseFormat: protocol.TextResponseFormat(request.ResponseFormat),
	}
	for _, message := range request.Messages {
		mapped := protocol.TextMessage{
			Role:         protocol.TextRole(message.Role),
			Content:      message.Content,
			ContentParts: make([]protocol.ContentPart, 0, len(message.ContentParts)),
			ToolCallID:   message.ToolCallID,
			ToolCalls:    make([]protocol.ToolCall, 0, len(message.ToolCalls)),
		}
		for _, part := range message.ContentParts {
			mapped.ContentParts = append(mapped.ContentParts, protocol.ContentPart{
				Kind:     protocol.ContentPartKind(part.Kind),
				Text:     part.Text,
				ImageURL: part.ImageURL,
			})
		}
		for _, call := range message.ToolCalls {
			mapped.ToolCalls = append(mapped.ToolCalls, protocol.ToolCall{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		result.Messages = append(result.Messages, mapped)
	}
	for _, definition := range request.Tools {
		result.Tools = append(result.Tools, protocol.ToolDefinition{
			Name: definition.Name, Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
	}
	return result, nil
}

func agentRunResult(result protocol.TextResult) agentrun.TextResult {
	mapped := agentrun.TextResult{
		ID: result.ID, Provider: result.Provider, Model: result.Model,
		Content: result.Content, FinishReason: result.FinishReason,
		Usage: agentrun.TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
		ToolCalls: make([]agentrun.ModelToolCall, 0, len(result.ToolCalls)),
	}
	for _, call := range result.ToolCalls {
		mapped.ToolCalls = append(mapped.ToolCalls, agentrun.ModelToolCall{
			ID: call.ID, Name: call.Name, Arguments: call.Arguments,
		})
	}
	return mapped
}

func mapAgentRunError(err error) error {
	var providerError *protocol.GenerationError
	if !errors.As(err, &providerError) {
		return agentrun.NewGenerationError(
			agentrun.ErrorProviderUnavailable, 0, "", "", err,
		)
	}
	return agentrun.NewGenerationError(
		mapAgentRunErrorKind(providerError.Kind),
		providerError.StatusCode,
		providerError.ProviderCode,
		providerError.RequestID,
		err,
	)
}

func mapAgentRunErrorKind(kind protocol.ErrorKind) agentrun.ErrorKind {
	switch kind {
	case protocol.ErrorInvalidRequest:
		return agentrun.ErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return agentrun.ErrorConfiguration
	case protocol.ErrorAuthentication:
		return agentrun.ErrorAuthentication
	case protocol.ErrorAuthorization:
		return agentrun.ErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return agentrun.ErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return agentrun.ErrorRateLimited
	case protocol.ErrorTimeout:
		return agentrun.ErrorTimeout
	case protocol.ErrorInvalidResponse:
		return agentrun.ErrorInvalidResponse
	case protocol.ErrorCancelled:
		return agentrun.ErrorCancelled
	default:
		return agentrun.ErrorProviderUnavailable
	}
}

var (
	_ agentrun.TextGenerator          = (*AgentRunGenerator)(nil)
	_ agentrun.StreamingTextGenerator = (*AgentRunGenerator)(nil)
)
