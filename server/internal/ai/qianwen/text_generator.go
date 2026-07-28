package qianwen

import (
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

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	providerName            = "qianwen"
	chatCompletionsPath     = "/chat/completions"
	compatibleBasePath      = "/compatible-mode/v1"
	maxResponseBytes        = 2 << 20
	maxErrorResponseBytes   = 64 << 10
	maxTimeout              = 5 * time.Minute
	maxOutputTokens         = 1_000_000
	maxProviderIdentifier   = 128
	maxProviderToolName     = 64
	authorizationHeaderName = "Authorization"
)

type Config struct {
	BaseURL         string
	Model           string
	Timeout         time.Duration
	MaxOutputTokens int
}

type Generator struct {
	endpoint        string
	model           string
	timeout         time.Duration
	maxOutputTokens int
	apiKey          providerSecret
	client          httpDoer
}

func (generator *Generator) String() string {
	if generator == nil {
		return "QianwenGenerator(<nil>)"
	}
	return fmt.Sprintf(
		"QianwenGenerator(model=%q, timeout=%s, max_output_tokens=%d, api_key=[REDACTED])",
		generator.model,
		generator.timeout,
		generator.maxOutputTokens,
	)
}

func (generator *Generator) GoString() string {
	return generator.String()
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func New(config Config, apiKey string) (*Generator, error) {
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newWithClient(config, apiKey, client)
}

func newWithClient(config Config, apiKey string, client httpDoer) (*Generator, error) {
	baseURL, err := normalizeBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model, err := normalizeModel(config.Model)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeAPIKey(apiKey)
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

	return &Generator{
		endpoint:        baseURL + chatCompletionsPath,
		model:           model,
		timeout:         config.Timeout,
		maxOutputTokens: config.MaxOutputTokens,
		apiKey:          newProviderSecret(apiKey),
		client:          client,
	}, nil
}

func (generator *Generator) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	if ctx == nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("text generation context is required"),
		)
	}
	if err := ai.ValidateTextRequest(request); err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	internalToProvider, providerToInternal, err := toolNameMappings(request)
	if err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}

	payload := chatCompletionRequest{
		Model:          generator.model,
		Messages:       make([]chatMessage, 0, len(request.Messages)),
		Tools:          make([]chatTool, 0, len(request.Tools)),
		Stream:         false,
		EnableThinking: false,
		MaxTokens:      generator.maxOutputTokens,
	}
	toolChoice, err := providerToolChoice(request.ToolChoice, internalToProvider)
	if err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	payload.ToolChoice = toolChoice
	for _, message := range request.Messages {
		providerMessage := chatMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  make([]chatToolCall, 0, len(message.ToolCalls)),
		}
		for index, call := range message.ToolCalls {
			providerMessage.ToolCalls = append(providerMessage.ToolCalls, chatToolCall{
				ID:    call.ID,
				Type:  "function",
				Index: index,
				Function: chatFunctionCall{
					Name:      internalToProvider[call.Name],
					Arguments: string(call.Arguments),
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
	body, err := json.Marshal(payload)
	if err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidRequest,
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
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorConfiguration,
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

	response, err := generator.client.Do(httpRequest)
	if err != nil {
		return ai.TextResult{}, transportError(callContext, err)
	}
	if response == nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			0,
			"",
			"",
			errors.New("Qianwen returned a nil HTTP response"),
		)
	}
	if response.Body == nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen returned an HTTP response without a body"),
		)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ai.TextResult{}, decodeStatusError(response)
	}

	responseBody, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	var completion chatCompletionResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("decode Qianwen response"),
		)
	}
	result, err := completion.result(providerToInternal)
	if err != nil {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	if result.Model != generator.model {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen response model does not match the requested model"),
		)
	}
	if result.Usage.OutputTokens > generator.maxOutputTokens {
		return ai.TextResult{}, ai.NewGenerationError(
			ai.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("Qianwen response exceeded the configured output budget"),
		)
	}
	return result, nil
}

type chatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Tools          []chatTool    `json:"tools,omitempty"`
	ToolChoice     any           `json:"tool_choice,omitempty"`
	Stream         bool          `json:"stream"`
	EnableThinking bool          `json:"enable_thinking"`
	// The current compatibility overview lists max_completion_tokens as
	// silently ignored. The endpoint-specific Chat API still honors the
	// deprecated max_tokens field, so it remains the enforceable budget.
	MaxTokens int `json:"max_tokens"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
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

func (response chatCompletionResponse) result(
	providerToInternal map[string]string,
) (ai.TextResult, error) {
	id := sanitizeIdentifier(response.ID)
	if id == "" {
		return ai.TextResult{}, errors.New("Qianwen response has no valid completion ID")
	}
	model, err := normalizeModel(response.Model)
	if err != nil {
		return ai.TextResult{}, errors.New("Qianwen response has no valid model")
	}
	if len(response.Choices) != 1 {
		return ai.TextResult{}, errors.New("Qianwen response must contain exactly one choice")
	}
	choice := response.Choices[0]
	if choice.Message.Role != string(ai.TextRoleAssistant) {
		return ai.TextResult{}, errors.New("Qianwen response choice has an invalid role")
	}
	content := strings.TrimSpace(choice.Message.Content)
	finishReason := choice.FinishReason
	if len(choice.Message.ToolCalls) > 0 && finishReason == "stop" {
		finishReason = "tool_calls"
	}
	switch finishReason {
	case "stop", "length":
		if content == "" {
			return ai.TextResult{}, errors.New("Qianwen response choice has no visible content")
		}
		if len(choice.Message.ToolCalls) != 0 {
			return ai.TextResult{}, errors.New("Qianwen text response contains unexpected tool calls")
		}
	case "tool_calls":
		if len(choice.Message.ToolCalls) == 0 {
			return ai.TextResult{}, errors.New("Qianwen tool response contains no tool calls")
		}
	default:
		return ai.TextResult{}, errors.New("Qianwen response has an unsupported finish reason")
	}
	var toolCalls []ai.ToolCall
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls = make([]ai.ToolCall, 0, len(choice.Message.ToolCalls))
	}
	seenCallIDs := make(map[string]struct{}, len(choice.Message.ToolCalls))
	for _, providerCall := range choice.Message.ToolCalls {
		if providerCall.Type != "function" ||
			sanitizeIdentifier(providerCall.ID) != providerCall.ID {
			return ai.TextResult{}, errors.New("Qianwen response contains an invalid tool call")
		}
		if _, exists := seenCallIDs[providerCall.ID]; exists {
			return ai.TextResult{}, errors.New("Qianwen response contains duplicate tool call IDs")
		}
		internalName, exists := providerToInternal[providerCall.Function.Name]
		if !exists {
			return ai.TextResult{}, errors.New("Qianwen response selected an unknown tool")
		}
		call := ai.ToolCall{
			ID:        providerCall.ID,
			Name:      internalName,
			Arguments: json.RawMessage(providerCall.Function.Arguments),
		}
		if err := ai.ValidateToolCall(call); err != nil {
			return ai.TextResult{}, errors.New("Qianwen response contains invalid tool arguments")
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
		return ai.TextResult{}, errors.New("Qianwen response has invalid token usage")
	}

	return ai.TextResult{
		ID:           id,
		Provider:     providerName,
		Model:        model,
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: ai.TokenUsage{
			InputTokens:  *response.Usage.PromptTokens,
			OutputTokens: *response.Usage.CompletionTokens,
			TotalTokens:  *response.Usage.TotalTokens,
		},
	}, nil
}

func toolNameMappings(request ai.TextRequest) (map[string]string, map[string]string, error) {
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
	choice ai.ToolChoice,
	internalToProvider map[string]string,
) (any, error) {
	switch choice.Mode {
	case "":
		return nil, nil
	case ai.ToolChoiceAuto:
		return "auto", nil
	case ai.ToolChoiceNone:
		return "none", nil
	case ai.ToolChoiceRequired:
		return "required", nil
	case ai.ToolChoiceSpecific:
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

	return ai.NewGenerationError(
		classifyStatus(response.StatusCode, code),
		response.StatusCode,
		code,
		requestID,
		readErr,
	)
}

func classifyStatus(statusCode int, providerCode string) ai.ErrorKind {
	normalizedCode := strings.ToLower(providerCode)
	if strings.Contains(normalizedCode, "allocationquota.freetieronly") {
		return ai.ErrorQuotaExhausted
	}
	if strings.Contains(normalizedCode, "arrearage") ||
		strings.Contains(normalizedCode, "billoverdue") ||
		strings.Contains(normalizedCode, "commoditynotpurchased") {
		return ai.ErrorAuthorization
	}
	if statusCode == http.StatusTooManyRequests {
		return ai.ErrorRateLimited
	}
	if statusCode == http.StatusRequestTimeout ||
		strings.Contains(normalizedCode, "timeout") {
		return ai.ErrorTimeout
	}
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ai.ErrorInvalidRequest
	case http.StatusUnauthorized:
		return ai.ErrorAuthentication
	case http.StatusForbidden:
		return ai.ErrorAuthorization
	case http.StatusNotFound:
		return ai.ErrorConfiguration
	}
	if statusCode >= http.StatusInternalServerError {
		return ai.ErrorProviderUnavailable
	}
	if statusCode >= http.StatusMultipleChoices && statusCode < http.StatusBadRequest {
		return ai.ErrorConfiguration
	}
	if statusCode >= http.StatusBadRequest {
		return ai.ErrorInvalidRequest
	}
	return ai.ErrorInvalidResponse
}

func transportError(ctx context.Context, cause error) error {
	kind := ai.ErrorProviderUnavailable
	var safeCause error
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		kind = ai.ErrorCancelled
		safeCause = context.Canceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded),
		errors.Is(cause, context.DeadlineExceeded):
		kind = ai.ErrorTimeout
		safeCause = context.DeadlineExceeded
	}
	return ai.NewGenerationError(kind, 0, "", "", safeCause)
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

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("Qianwen base URL is invalid")
	}
	if parsed.Scheme != "https" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Port() != "" {
		return "", errors.New("Qianwen base URL must be a credential-free HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if !isOfficialHost(host) {
		return "", errors.New("Qianwen base URL must use an official Alibaba Cloud endpoint")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != compatibleBasePath {
		return "", fmt.Errorf("Qianwen base URL path must be %s", compatibleBasePath)
	}
	parsed.Path = compatibleBasePath
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isOfficialHost(host string) bool {
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

func normalizeModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if len(model) == 0 || len(model) > maxProviderIdentifier {
		return "", errors.New("Qianwen model is required and must not exceed 128 characters")
	}
	if !strings.HasPrefix(strings.ToLower(model), "qwen") {
		return "", errors.New("Qianwen adapter only accepts Qwen model IDs")
	}
	for _, value := range model {
		if !((value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') ||
			value == '-' || value == '_' || value == '.') {
			return "", errors.New("Qianwen model contains unsupported characters")
		}
	}
	return model, nil
}

func normalizeAPIKey(raw string) (string, error) {
	apiKey := strings.TrimSpace(raw)
	if apiKey == "" {
		return "", errors.New("Qianwen API key is required")
	}
	for _, value := range apiKey {
		if unicode.IsSpace(value) || unicode.IsControl(value) {
			return "", errors.New("Qianwen API key contains whitespace or control characters")
		}
	}
	return apiKey, nil
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

var _ ai.TextGenerator = (*Generator)(nil)
