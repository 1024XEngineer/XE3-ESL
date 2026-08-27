package qianwen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

const (
	providerName            = "qianwen"
	qiniuProviderName       = "qiniu"
	qiniuKimiK26Model       = "moonshotai/kimi-k2.6"
	chatCompletionsPath     = "/chat/completions"
	compatibleBasePath      = "/compatible-mode/v1"
	qiniuCompatibleBasePath = "/v1"
	maxResponseBytes        = 2 << 20
	maxErrorResponseBytes   = 64 << 10
	maxTimeout              = 5 * time.Minute
	maxOutputTokens         = 1_000_000
	maxProviderIdentifier   = 128
	maxProviderToolName     = 64
	maxStreamEventBytes     = 1 << 20
	maxStreamBytes          = 8 << 20
	maxStreamToolArgsBytes  = 1 << 20
	authorizationHeaderName = "Authorization"
)

type TextConfig struct {
	// Provider is empty for the native Qianwen adapter. The Qiniu wrapper sets
	// it explicitly so both providers can share the audited OpenAI-compatible
	// wire implementation without changing the business ports.
	Provider        string
	BaseURL         string
	Model           string
	Timeout         time.Duration
	MaxOutputTokens int
	Observer        providerobservability.Recorder
}

type textClient struct {
	provider        string
	providerLabel   string
	endpoint        string
	model           string
	timeout         time.Duration
	maxOutputTokens int
	apiKey          providerSecret
	client          httpDoer
	observer        providerobservability.Recorder
}

func (generator *textClient) String() string {
	if generator == nil {
		return "QianwenGenerator(<nil>)"
	}
	return fmt.Sprintf(
		"%sGenerator(model=%q, timeout=%s, max_output_tokens=%d, api_key=[REDACTED])",
		generator.providerLabel,
		generator.model,
		generator.timeout,
		generator.maxOutputTokens,
	)
}

func (generator *textClient) GoString() string {
	return generator.String()
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func newTextClient(config TextConfig, apiKey string) (*textClient, error) {
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newWithClient(config, apiKey, client)
}

func newWithClient(config TextConfig, apiKey string, client httpDoer) (*textClient, error) {
	settings, err := textProviderSettingsFor(config.Provider)
	if err != nil {
		return nil, err
	}
	baseURL, err := normalizeTextBaseURL(config.BaseURL, settings)
	if err != nil {
		return nil, err
	}
	model, err := normalizeModel(config.Model, settings)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeTextAPIKey(apiKey, settings)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf("Qianwen timeout must be greater than zero and at most %s", maxTimeout)
	}
	if config.MaxOutputTokens <= 0 || config.MaxOutputTokens > maxOutputTokens {
		return nil, fmt.Errorf(
			"Qianwen output budget must be between 1 and %d tokens",
			maxOutputTokens,
		)
	}
	if client == nil {
		return nil, errors.New("Qianwen HTTP client is required")
	}

	return &textClient{
		provider:        settings.name,
		providerLabel:   settings.label,
		endpoint:        baseURL + chatCompletionsPath,
		model:           model,
		timeout:         config.Timeout,
		maxOutputTokens: config.MaxOutputTokens,
		apiKey:          newProviderSecret(apiKey),
		client:          client,
		observer:        config.Observer,
	}, nil
}

func (generator *textClient) Generate(
	ctx context.Context,
	request protocol.TextRequest,
) (callResult protocol.TextResult, callErr error) {
	if ctx == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("text generation context is required"),
		)
	}
	if err := protocol.ValidateTextRequest(request); err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	internalToProvider, providerToInternal, err := toolNameMappings(request)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}

	payload, err := generator.providerRequest(request, internalToProvider)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}

	callContext, cancel := context.WithTimeout(ctx, generator.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		callContext,
		http.MethodPost,
		generator.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorConfiguration,
			0,
			"",
			"",
			err,
		)
	}
	httpRequest.Header.Set(
		authorizationHeaderName,
		"Bearer "+generator.apiKey.reveal(),
	)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	startedAt := time.Now()
	observedUsage := protocol.TokenUsage{}
	defer func() {
		recordTextCall(
			generator.observer,
			generator.provider,
			startedAt,
			observedUsage,
			callErr,
		)
	}()
	response, err := generator.client.Do(httpRequest)
	if err != nil {
		return protocol.TextResult{}, transportError(callContext, err)
	}
	if response == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			0,
			"",
			"",
			errors.New("Qianwen returned a nil HTTP response"),
		)
	}
	if response.Body == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen returned an HTTP response without a body"),
		)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return protocol.TextResult{}, decodeStatusError(response)
	}

	responseBody, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	var completion chatCompletionResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("decode Qianwen response"),
		)
	}
	observedUsage = completion.reportedUsage()
	result, err := completion.result(generator.provider, providerToInternal)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	if result.Model != generator.model {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen response model does not match the requested model"),
		)
	}
	if result.Usage.OutputTokens > generator.maxOutputTokens {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen response exceeded the configured output budget"),
		)
	}
	return result, nil
}

// GenerateStream implements the provider-neutral streaming boundary. It
// validates the complete Qwen stream before returning the canonical result and
// emits only visible assistant content. Tool-call fragments remain internal.
func (generator *textClient) GenerateStream(
	ctx context.Context,
	request protocol.TextRequest,
	observer protocol.TextDeltaObserver,
) (callResult protocol.TextResult, callErr error) {
	if ctx == nil || observer == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "",
			errors.New("streaming text generation context and observer are required"),
		)
	}
	if err := protocol.ValidateTextRequest(request); err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	internalToProvider, providerToInternal, err := toolNameMappings(request)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	payload, err := generator.providerRequest(request, internalToProvider)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	payload.Stream = true
	payload.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	callContext, cancel := context.WithTimeout(ctx, generator.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		callContext, http.MethodPost, generator.endpoint, bytes.NewReader(body),
	)
	if err != nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorConfiguration, 0, "", "", err,
		)
	}
	httpRequest.Header.Set(authorizationHeaderName, "Bearer "+generator.apiKey.reveal())
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	startedAt := time.Now()
	observedUsage := protocol.TokenUsage{}
	defer func() {
		recordTextCall(
			generator.observer,
			generator.provider,
			startedAt,
			observedUsage,
			callErr,
		)
	}()
	response, err := generator.client.Do(httpRequest)
	if err != nil {
		return protocol.TextResult{}, transportError(callContext, err)
	}
	if response == nil || response.Body == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse, 0, "", "",
			errors.New("Qianwen returned an invalid streaming HTTP response"),
		)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return protocol.TextResult{}, decodeStatusError(response)
	}
	if mediaType := strings.ToLower(strings.TrimSpace(
		strings.Split(response.Header.Get("Content-Type"), ";")[0],
	)); mediaType != "text/event-stream" {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse, response.StatusCode, "",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen streaming response has an invalid content type"),
		)
	}
	result, err := decodeCompletionStream(
		callContext,
		response.Body,
		generator.provider,
		providerToInternal,
		observer,
		&observedUsage,
	)
	if err != nil {
		var generationError *protocol.GenerationError
		if errors.As(err, &generationError) {
			return protocol.TextResult{}, err
		}
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse, response.StatusCode, "",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")), err,
		)
	}
	if result.Model != generator.model {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse, response.StatusCode, "", "",
			errors.New("Qianwen response model does not match the requested model"),
		)
	}
	if result.Usage.OutputTokens > generator.maxOutputTokens {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse, response.StatusCode, "", "",
			errors.New("Qianwen response exceeded the configured output budget"),
		)
	}
	return result, nil
}

func (generator *textClient) providerRequest(
	request protocol.TextRequest,
	internalToProvider map[string]string,
) (chatCompletionRequest, error) {
	maxTokens := generator.maxOutputTokens
	payload := chatCompletionRequest{
		Model:     generator.model,
		Messages:  make([]chatMessage, 0, len(request.Messages)),
		Tools:     make([]chatTool, 0, len(request.Tools)),
		Stream:    false,
		MaxTokens: &maxTokens,
	}
	switch generator.provider {
	case providerName:
		disabled := false
		payload.EnableThinking = &disabled
	case qiniuProviderName:
		// Qiniu fronts heterogeneous upstream APIs. Kimi uses Qiniu's generic
		// thinking control, Qwen uses enable_thinking, and other upstreams omit both.
		if generator.model == qiniuKimiK26Model {
			payload.Thinking = &chatThinking{Type: "disabled"}
		} else if strings.HasPrefix(strings.ToLower(generator.model), "qwen") {
			disabled := false
			payload.EnableThinking = &disabled
		}
	}
	if request.ResponseFormat == protocol.TextResponseFormatJSON {
		payload.ResponseFormat = &chatResponseFormat{
			Type: string(protocol.TextResponseFormatJSON),
		}
	} else if request.ResponseFormat == protocol.TextResponseFormatJSONSchema {
		payload.ResponseFormat = &chatResponseFormat{
			Type: string(protocol.TextResponseFormatJSONSchema),
			JSONSchema: &chatJSONSchema{
				Name:   request.ResponseSchema.Name,
				Strict: request.ResponseSchema.Strict,
				Schema: request.ResponseSchema.Schema,
			},
		}
		// DashScope's strict structured-output contract owns the completion
		// shape and explicitly recommends omitting the legacy token field.
		payload.MaxTokens = nil
	}
	toolChoice, err := providerToolChoice(request.ToolChoice, internalToProvider)
	if err != nil {
		return chatCompletionRequest{}, err
	}
	payload.ToolChoice = toolChoice
	for _, message := range request.Messages {
		providerMessage := chatMessage{
			Role:       string(message.Role),
			ToolCallID: message.ToolCallID,
			ToolCalls:  make([]chatToolCall, 0, len(message.ToolCalls)),
		}
		if len(message.ContentParts) != 0 {
			providerMessage.Content = providerContentParts(message.ContentParts)
		} else if message.Content != "" {
			providerMessage.Content = message.Content
		}
		for index, call := range message.ToolCalls {
			providerMessage.ToolCalls = append(providerMessage.ToolCalls, chatToolCall{
				ID: call.ID, Type: "function", Index: index,
				Function: chatFunctionCall{
					Name: internalToProvider[call.Name], Arguments: string(call.Arguments),
				},
			})
		}
		payload.Messages = append(payload.Messages, providerMessage)
	}
	for _, definition := range request.Tools {
		payload.Tools = append(payload.Tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        internalToProvider[definition.Name],
				Description: definition.Description,
				Parameters:  definition.InputSchema,
			},
		})
	}
	return payload, nil
}

type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Tools          []chatTool          `json:"tools,omitempty"`
	ToolChoice     any                 `json:"tool_choice,omitempty"`
	Stream         bool                `json:"stream"`
	StreamOptions  *chatStreamOptions  `json:"stream_options,omitempty"`
	EnableThinking *bool               `json:"enable_thinking,omitempty"`
	Thinking       *chatThinking       `json:"thinking,omitempty"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	// The current compatibility overview lists max_completion_tokens as
	// silently ignored. The endpoint-specific Chat API still honors the
	// deprecated max_tokens field, so it remains the enforceable budget.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

type chatThinking struct {
	Type string `json:"type"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *chatJSONSchema `json:"json_schema,omitempty"`
}

type chatJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Index    int              `json:"index"`
	Function chatFunctionCall `json:"function"`
}

type chatSpecificToolChoice struct {
	Type     string                         `json:"type"`
	Function chatSpecificToolChoiceFunction `json:"function"`
}

type chatSpecificToolChoiceFunction struct {
	Name string `json:"name"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func providerContentParts(parts []protocol.ContentPart) []chatContentPart {
	result := make([]chatContentPart, 0, len(parts))
	for _, part := range parts {
		providerPart := chatContentPart{Type: string(part.Kind)}
		switch part.Kind {
		case protocol.ContentPartText:
			providerPart.Text = part.Text
		case protocol.ContentPartImageURL:
			providerPart.ImageURL = &chatImageURL{URL: part.ImageURL}
		}
		result = append(result, providerPart)
	}
	return result
}

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage"`
}

type chatCompletionChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason *string `json:"finish_reason"`
		Delta        struct {
			Role      string         `json:"role"`
			Content   *string        `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage"`
}

type streamMode uint8

const (
	streamModeUnknown streamMode = iota
	streamModeText
	streamModeTools
	streamModeMixed
)

type streamToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func decodeCompletionStream(
	ctx context.Context,
	body io.Reader,
	provider string,
	providerToInternal map[string]string,
	observer protocol.TextDeltaObserver,
	reportedUsage *protocol.TokenUsage,
) (protocol.TextResult, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, maxStreamBytes+1))
	scanner.Buffer(make([]byte, 16<<10), maxStreamEventBytes)
	mode := streamModeUnknown
	var completionID, model, finishReason string
	var content strings.Builder
	var pendingWhitespace string
	var totalBytes int
	var usage *struct {
		prompt     int
		completion int
		total      int
	}
	var tools []streamToolCall
	sawDone := false
	sawUsage := false
	for scanner.Scan() {
		line := scanner.Text()
		totalBytes += len(line) + 1
		if totalBytes > maxStreamBytes {
			return protocol.TextResult{}, errors.New("Qianwen stream exceeds the response limit")
		}
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			return protocol.TextResult{}, errors.New("Qianwen stream contains an unsupported SSE field")
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		if data == "" {
			return protocol.TextResult{}, errors.New("Qianwen stream contains empty event data")
		}
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return protocol.TextResult{}, errors.New("decode Qianwen stream event")
		}
		chunkID := sanitizeIdentifier(chunk.ID)
		chunkModel, _ := normalizeReturnedModel(chunk.Model)
		if completionID == "" {
			completionID = chunkID
			model = chunkModel
		} else if chunkID != completionID || chunkModel != model {
			return protocol.TextResult{}, errors.New("Qianwen stream changed completion identity")
		}
		if completionID == "" || model == "" {
			return protocol.TextResult{}, errors.New("Qianwen stream has invalid completion identity")
		}
		usageInChunk := chunk.Usage != nil
		if usageInChunk {
			if sawUsage || len(chunk.Choices) > 1 ||
				chunk.Usage.PromptTokens == nil ||
				chunk.Usage.CompletionTokens == nil ||
				chunk.Usage.TotalTokens == nil {
				return protocol.TextResult{}, errors.New("Qianwen stream has invalid final usage")
			}
			usage = &struct {
				prompt     int
				completion int
				total      int
			}{
				prompt:     *chunk.Usage.PromptTokens,
				completion: *chunk.Usage.CompletionTokens,
				total:      *chunk.Usage.TotalTokens,
			}
			if usage.prompt < 0 || usage.completion < 0 || usage.total < 0 ||
				usage.total-usage.prompt != usage.completion {
				return protocol.TextResult{}, errors.New("Qianwen stream has invalid token usage")
			}
			if reportedUsage != nil {
				*reportedUsage = protocol.TokenUsage{
					InputTokens:  usage.prompt,
					OutputTokens: usage.completion,
					TotalTokens:  usage.total,
				}
			}
			sawUsage = true
			if len(chunk.Choices) == 0 {
				continue
			}
		}
		// Official OpenAI-compatible examples consume chunks by state: choice
		// chunks carry deltas, while the final empty-choice chunk carries usage.
		// Provider metadata-only chunks do not change the completion.
		if len(chunk.Choices) == 0 {
			continue
		}
		if (sawUsage && !usageInChunk) || len(chunk.Choices) != 1 {
			return protocol.TextResult{}, errors.New("Qianwen stream must contain exactly one choice")
		}
		choice := chunk.Choices[0]
		if usageInChunk && choice.FinishReason == nil {
			return protocol.TextResult{}, errors.New("Qianwen stream has invalid final usage")
		}
		if choice.Delta.Role != "" &&
			choice.Delta.Role != string(protocol.TextRoleAssistant) {
			return protocol.TextResult{}, errors.New("Qianwen stream has an invalid delta role")
		}
		hasText := choice.Delta.Content != nil && *choice.Delta.Content != ""
		hasTools := len(choice.Delta.ToolCalls) != 0
		if hasText {
			if mode == streamModeTools {
				mode = streamModeMixed
			} else if mode == streamModeUnknown {
				mode = streamModeText
			}
			visible, pending := normalizedVisibleDelta(
				content.Len() > 0,
				pendingWhitespace+*choice.Delta.Content,
			)
			pendingWhitespace = pending
			if visible != "" {
				content.WriteString(visible)
				if err := observer.OnTextDelta(ctx, visible); err != nil {
					return protocol.TextResult{}, protocol.NewGenerationError(
						protocol.ErrorCancelled, 0, "", "", err,
					)
				}
			}
		}
		if hasTools {
			if mode == streamModeText {
				mode = streamModeMixed
			} else if mode == streamModeUnknown {
				mode = streamModeTools
			}
			for _, fragment := range choice.Delta.ToolCalls {
				if fragment.Index < 0 || fragment.Index > len(tools) {
					return protocol.TextResult{}, errors.New("Qianwen stream has a non-contiguous tool index")
				}
				if fragment.Index == len(tools) {
					tools = append(tools, streamToolCall{})
				}
				call := &tools[fragment.Index]
				if fragment.ID != "" {
					if call.id != "" && call.id != fragment.ID {
						return protocol.TextResult{}, errors.New("Qianwen stream changed tool call ID")
					}
					call.id = fragment.ID
				}
				if fragment.Type != "" && fragment.Type != "function" {
					return protocol.TextResult{}, errors.New("Qianwen stream has an invalid tool type")
				}
				if fragment.Function.Name != "" {
					if call.name != "" && call.name != fragment.Function.Name {
						return protocol.TextResult{}, errors.New("Qianwen stream changed tool name")
					}
					call.name = fragment.Function.Name
				}
				if call.arguments.Len()+len(fragment.Function.Arguments) >
					maxStreamToolArgsBytes {
					return protocol.TextResult{}, errors.New("Qianwen tool arguments exceed the stream limit")
				}
				call.arguments.WriteString(fragment.Function.Arguments)
			}
		}
		if choice.FinishReason != nil {
			if finishReason != "" && finishReason != *choice.FinishReason {
				return protocol.TextResult{}, errors.New("Qianwen stream duplicated finish reason")
			}
			finishReason = *choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return protocol.TextResult{}, err
	}
	if !sawDone || !sawUsage || finishReason == "" {
		return protocol.TextResult{}, errors.New("Qianwen stream ended before completion")
	}
	if provider == qiniuProviderName &&
		(mode == streamModeTools || mode == streamModeMixed) &&
		finishReason == "stop" && len(tools) > 0 {
		finishReason = "tool_calls"
	}
	result := protocol.TextResult{
		ID: completionID, Provider: provider, Model: model,
		Content: content.String(), FinishReason: finishReason,
		Usage: protocol.TokenUsage{
			InputTokens: usage.prompt, OutputTokens: usage.completion,
			TotalTokens: usage.total,
		},
	}
	switch mode {
	case streamModeText:
		if result.Content == "" || (finishReason != "stop" && finishReason != "length") {
			return protocol.TextResult{}, errors.New("Qianwen text stream has an invalid completion")
		}
	case streamModeTools, streamModeMixed:
		if finishReason != "tool_calls" || len(tools) == 0 {
			return protocol.TextResult{}, errors.New("Qianwen tool stream has an invalid completion")
		}
		result.ToolCalls = make([]protocol.ToolCall, 0, len(tools))
		for _, streamed := range tools {
			internalName, exists := providerToInternal[streamed.name]
			if !exists {
				return protocol.TextResult{}, errors.New("Qianwen stream selected an unknown tool")
			}
			call := protocol.ToolCall{
				ID: streamed.id, Name: internalName,
				Arguments: json.RawMessage(streamed.arguments.String()),
			}
			if err := protocol.ValidateToolCall(call); err != nil {
				return protocol.TextResult{}, errors.New("Qianwen stream contains an invalid tool call")
			}
			result.ToolCalls = append(result.ToolCalls, call)
		}
	default:
		return protocol.TextResult{}, errors.New("Qianwen stream has no substantive delta")
	}
	return result, nil
}

func normalizedVisibleDelta(started bool, value string) (string, string) {
	if !started {
		value = strings.TrimLeftFunc(value, unicode.IsSpace)
	}
	visible := strings.TrimRightFunc(value, unicode.IsSpace)
	return visible, value[len(visible):]
}

func (response chatCompletionResponse) result(
	provider string,
	providerToInternal map[string]string,
) (protocol.TextResult, error) {
	id := sanitizeIdentifier(response.ID)
	if id == "" {
		return protocol.TextResult{}, errors.New("Qianwen response has no valid completion ID")
	}
	model, err := normalizeReturnedModel(response.Model)
	if err != nil {
		return protocol.TextResult{}, errors.New("Qianwen response has no valid model")
	}
	if len(response.Choices) != 1 {
		return protocol.TextResult{}, errors.New("Qianwen response must contain exactly one choice")
	}
	choice := response.Choices[0]
	if choice.Message.Role != string(protocol.TextRoleAssistant) {
		return protocol.TextResult{}, errors.New("Qianwen response choice has an invalid role")
	}
	content := strings.TrimSpace(choice.Message.Content)
	finishReason := choice.FinishReason
	if len(choice.Message.ToolCalls) > 0 && finishReason == "stop" {
		finishReason = "tool_calls"
	}
	switch finishReason {
	case "stop", "length":
		if content == "" {
			return protocol.TextResult{}, errors.New("Qianwen response choice has no visible content")
		}
		if len(choice.Message.ToolCalls) != 0 {
			return protocol.TextResult{}, errors.New("Qianwen text response contains unexpected tool calls")
		}
	case "tool_calls":
		if len(choice.Message.ToolCalls) == 0 {
			return protocol.TextResult{}, errors.New("Qianwen tool response contains no tool calls")
		}
	default:
		return protocol.TextResult{}, errors.New("Qianwen response has an unsupported finish reason")
	}
	var toolCalls []protocol.ToolCall
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls = make([]protocol.ToolCall, 0, len(choice.Message.ToolCalls))
	}
	seenCallIDs := make(map[string]struct{}, len(choice.Message.ToolCalls))
	for _, providerCall := range choice.Message.ToolCalls {
		if providerCall.Type != "function" ||
			sanitizeIdentifier(providerCall.ID) != providerCall.ID {
			return protocol.TextResult{}, errors.New("Qianwen response contains an invalid tool call")
		}
		if _, exists := seenCallIDs[providerCall.ID]; exists {
			return protocol.TextResult{}, errors.New("Qianwen response contains duplicate tool call IDs")
		}
		internalName, exists := providerToInternal[providerCall.Function.Name]
		if !exists {
			return protocol.TextResult{}, errors.New("Qianwen response selected an unknown tool")
		}
		call := protocol.ToolCall{
			ID:        providerCall.ID,
			Name:      internalName,
			Arguments: json.RawMessage(providerCall.Function.Arguments),
		}
		if err := protocol.ValidateToolCall(call); err != nil {
			return protocol.TextResult{}, errors.New("Qianwen response contains invalid tool arguments")
		}
		seenCallIDs[providerCall.ID] = struct{}{}
		toolCalls = append(toolCalls, call)
	}
	if response.Usage == nil ||
		response.Usage.PromptTokens == nil ||
		response.Usage.CompletionTokens == nil ||
		response.Usage.TotalTokens == nil ||
		*response.Usage.PromptTokens < 0 ||
		*response.Usage.CompletionTokens < 0 ||
		*response.Usage.TotalTokens < 0 ||
		*response.Usage.TotalTokens < *response.Usage.PromptTokens ||
		*response.Usage.TotalTokens-*response.Usage.PromptTokens !=
			*response.Usage.CompletionTokens {
		return protocol.TextResult{}, errors.New("Qianwen response has invalid token usage")
	}

	return protocol.TextResult{
		ID:           id,
		Provider:     provider,
		Model:        model,
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: protocol.TokenUsage{
			InputTokens:  *response.Usage.PromptTokens,
			OutputTokens: *response.Usage.CompletionTokens,
			TotalTokens:  *response.Usage.TotalTokens,
		},
	}, nil
}

func (response chatCompletionResponse) reportedUsage() protocol.TokenUsage {
	if response.Usage == nil || response.Usage.PromptTokens == nil ||
		response.Usage.CompletionTokens == nil || response.Usage.TotalTokens == nil ||
		*response.Usage.PromptTokens < 0 || *response.Usage.CompletionTokens < 0 ||
		*response.Usage.TotalTokens < 0 ||
		*response.Usage.TotalTokens-*response.Usage.PromptTokens !=
			*response.Usage.CompletionTokens {
		return protocol.TokenUsage{}
	}
	return protocol.TokenUsage{
		InputTokens:  *response.Usage.PromptTokens,
		OutputTokens: *response.Usage.CompletionTokens,
		TotalTokens:  *response.Usage.TotalTokens,
	}
}

func toolNameMappings(request protocol.TextRequest) (map[string]string, map[string]string, error) {
	internalToProvider := make(map[string]string)
	providerToInternal := make(map[string]string)
	providerOwners := make(map[string]string)
	add := func(internalName string, selectable bool) error {
		if _, exists := internalToProvider[internalName]; exists {
			if selectable {
				providerToInternal[internalToProvider[internalName]] = internalName
			}
			return nil
		}
		providerName := strings.NewReplacer(".", "_", ":", "_").Replace(internalName)
		if len(providerName) == 0 || len(providerName) > maxProviderToolName {
			return errors.New("Qianwen tool name exceeds the provider limit")
		}
		if existing, exists := providerOwners[providerName]; exists &&
			existing != internalName {
			return errors.New("Qianwen tool names collide after provider normalization")
		}
		internalToProvider[internalName] = providerName
		providerOwners[providerName] = internalName
		if selectable {
			providerToInternal[providerName] = internalName
		}
		return nil
	}
	for _, definition := range request.Tools {
		if err := add(definition.Name, true); err != nil {
			return nil, nil, err
		}
	}
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if err := add(call.Name, false); err != nil {
				return nil, nil, err
			}
		}
	}
	return internalToProvider, providerToInternal, nil
}

func providerToolChoice(
	choice protocol.ToolChoice,
	internalToProvider map[string]string,
) (any, error) {
	switch choice.Mode {
	case "":
		return nil, nil
	case protocol.ToolChoiceAuto:
		return "auto", nil
	case protocol.ToolChoiceNone:
		return "none", nil
	case protocol.ToolChoiceRequired:
		return "required", nil
	case protocol.ToolChoiceSpecific:
		providerName, exists := internalToProvider[choice.Name]
		if !exists {
			return nil, errors.New("Qianwen specific tool choice is unavailable")
		}
		return chatSpecificToolChoice{
			Type: "function",
			Function: chatSpecificToolChoiceFunction{
				Name: providerName,
			},
		}, nil
	default:
		return nil, errors.New("Qianwen tool choice mode is unsupported")
	}
}

type errorEnvelope struct {
	Code      json.RawMessage `json:"code"`
	RequestID string          `json:"request_id"`
	Error     *struct {
		Code json.RawMessage `json:"code"`
		Type string          `json:"type"`
	} `json:"error"`
}

func decodeStatusError(response *http.Response) error {
	body, readErr := readBounded(response.Body, maxErrorResponseBytes)
	code := ""
	requestID := sanitizeIdentifier(response.Header.Get("X-Request-Id"))
	if readErr == nil {
		var envelope errorEnvelope
		if json.Unmarshal(body, &envelope) == nil {
			code = rawIdentifier(envelope.Code)
			if envelope.Error != nil {
				if nestedCode := rawIdentifier(envelope.Error.Code); nestedCode != "" {
					code = nestedCode
				} else if nestedType := sanitizeIdentifier(envelope.Error.Type); nestedType != "" {
					code = nestedType
				}
			}
			if bodyRequestID := sanitizeIdentifier(envelope.RequestID); bodyRequestID != "" {
				requestID = bodyRequestID
			}
		}
	}

	return protocol.NewGenerationError(
		classifyStatus(response.StatusCode, code),
		response.StatusCode,
		code,
		requestID,
		readErr,
	)
}

func classifyStatus(statusCode int, providerCode string) protocol.ErrorKind {
	normalizedCode := strings.ToLower(providerCode)
	if statusCode == http.StatusPaymentRequired ||
		strings.Contains(normalizedCode, "quota_exceeded") {
		return protocol.ErrorQuotaExhausted
	}
	if strings.Contains(normalizedCode, "allocationquota.freetieronly") {
		return protocol.ErrorQuotaExhausted
	}
	if strings.Contains(normalizedCode, "arrearage") ||
		strings.Contains(normalizedCode, "billoverdue") ||
		strings.Contains(normalizedCode, "commoditynotpurchased") {
		return protocol.ErrorAuthorization
	}
	if statusCode == http.StatusTooManyRequests {
		return protocol.ErrorRateLimited
	}
	if statusCode == http.StatusRequestTimeout ||
		strings.Contains(normalizedCode, "timeout") {
		return protocol.ErrorTimeout
	}
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return protocol.ErrorInvalidRequest
	case http.StatusUnauthorized:
		return protocol.ErrorAuthentication
	case http.StatusForbidden:
		return protocol.ErrorAuthorization
	case http.StatusNotFound:
		return protocol.ErrorConfiguration
	}
	if statusCode >= http.StatusInternalServerError {
		return protocol.ErrorProviderUnavailable
	}
	if statusCode >= http.StatusMultipleChoices && statusCode < http.StatusBadRequest {
		return protocol.ErrorConfiguration
	}
	if statusCode >= http.StatusBadRequest {
		return protocol.ErrorInvalidRequest
	}
	return protocol.ErrorInvalidResponse
}

func transportError(ctx context.Context, cause error) error {
	kind := protocol.ErrorProviderUnavailable
	var safeCause error
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		kind = protocol.ErrorCancelled
		safeCause = context.Canceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded),
		errors.Is(cause, context.DeadlineExceeded):
		kind = protocol.ErrorTimeout
		safeCause = context.DeadlineExceeded
	}
	return protocol.NewGenerationError(kind, 0, "", "", safeCause)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, errors.New("read Qianwen response")
	}
	if int64(len(body)) > limit {
		return nil, errors.New("Qianwen response exceeds the size limit")
	}
	return body, nil
}

type textProviderSettings struct {
	name     string
	label    string
	basePath string
}

func textProviderSettingsFor(provider string) (textProviderSettings, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", providerName:
		return textProviderSettings{
			name: providerName, label: "Qianwen", basePath: compatibleBasePath,
		}, nil
	case qiniuProviderName:
		return textProviderSettings{
			name: qiniuProviderName, label: "Qiniu", basePath: qiniuCompatibleBasePath,
		}, nil
	default:
		return textProviderSettings{}, errors.New("text provider is not supported")
	}
}

func normalizeTextBaseURL(raw string, settings textProviderSettings) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s base URL is invalid", settings.label)
	}
	if parsed.Scheme != "https" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Port() != "" {
		return "", fmt.Errorf(
			"%s base URL must be a credential-free HTTPS URL",
			settings.label,
		)
	}
	host := strings.ToLower(parsed.Hostname())
	if !isOfficialTextHost(host, settings) {
		return "", fmt.Errorf(
			"%s base URL must use an official provider endpoint",
			settings.label,
		)
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != settings.basePath {
		return "", fmt.Errorf(
			"%s base URL path must be %s",
			settings.label,
			settings.basePath,
		)
	}
	parsed.Path = settings.basePath
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isOfficialTextHost(host string, settings textProviderSettings) bool {
	if settings.name == qiniuProviderName {
		return host == "api.qnaigc.com"
	}
	switch host {
	case "dashscope.aliyuncs.com",
		"dashscope-intl.aliyuncs.com",
		"dashscope-us.aliyuncs.com":
		return true
	default:
		return strings.HasSuffix(host, ".maas.aliyuncs.com") &&
			!strings.HasPrefix(host, "token-plan.")
	}
}

func normalizeModel(raw string, settings textProviderSettings) (string, error) {
	model, err := normalizeReturnedModel(raw)
	if err != nil {
		return "", fmt.Errorf(
			"%s model is required and contains only supported characters",
			settings.label,
		)
	}
	if settings.name == providerName &&
		!strings.HasPrefix(strings.ToLower(model), "qwen") {
		return "", errors.New("Qianwen adapter only accepts Qwen model IDs")
	}
	return model, nil
}

func normalizeReturnedModel(raw string) (string, error) {
	if !modelid.Valid(raw) {
		return "", errors.New("provider model is invalid")
	}
	return raw, nil
}

func normalizeTextAPIKey(raw string, settings textProviderSettings) (string, error) {
	apiKey := strings.TrimSpace(raw)
	if apiKey == "" {
		return "", fmt.Errorf("%s API key is required", settings.label)
	}
	for _, value := range apiKey {
		if unicode.IsSpace(value) || unicode.IsControl(value) {
			return "", fmt.Errorf(
				"%s API key contains whitespace or control characters",
				settings.label,
			)
		}
	}
	return apiKey, nil
}

// These Qianwen-only helpers remain shared by the speech adapters. Text
// generation uses the provider-aware variants above.
func normalizeBaseURL(raw string) (string, error) {
	settings, _ := textProviderSettingsFor(providerName)
	return normalizeTextBaseURL(raw, settings)
}

func normalizeAPIKey(raw string) (string, error) {
	settings, _ := textProviderSettingsFor(providerName)
	return normalizeTextAPIKey(raw, settings)
}

func isOfficialHost(host string) bool {
	settings, _ := textProviderSettingsFor(providerName)
	return isOfficialTextHost(host, settings)
}

func rawIdentifier(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return sanitizeIdentifier(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return sanitizeIdentifier(number.String())
	}
	return ""
}

func sanitizeIdentifier(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > maxProviderIdentifier {
		return ""
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._:-", character)) {
			return ""
		}
	}
	return value
}

var _ protocol.TextGenerator = (*textClient)(nil)
