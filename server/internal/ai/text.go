package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// TextGenerator is the application-facing boundary for one text completion.
// Implementations own provider-specific transport details and must not retry
// calls implicitly.
type TextGenerator interface {
	Generate(context.Context, TextRequest) (TextResult, error)
}

// StreamingTextGenerator emits only user-visible assistant text. Provider
// reasoning and fragmented tool-call arguments are never exposed through the
// observer; they are returned only in the validated final TextResult.
type StreamingTextGenerator interface {
	GenerateStream(context.Context, TextRequest, TextDeltaObserver) (TextResult, error)
}

type TextDeltaObserver interface {
	OnTextDelta(context.Context, string) error
}

type TextDeltaObserverFunc func(context.Context, string) error

func (observe TextDeltaObserverFunc) OnTextDelta(
	ctx context.Context,
	delta string,
) error {
	return observe(ctx, delta)
}

type TextRole string

const (
	TextRoleSystem    TextRole = "system"
	TextRoleUser      TextRole = "user"
	TextRoleAssistant TextRole = "assistant"
	TextRoleTool      TextRole = "tool"
)

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceSpecific ToolChoiceMode = "specific"
)

type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

type TextMessage struct {
	Role       TextRole
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

type TextResponseFormat string

const (
	TextResponseFormatDefault TextResponseFormat = ""
	TextResponseFormatJSON    TextResponseFormat = "json_object"
)

func (format TextResponseFormat) Valid() bool {
	return format == TextResponseFormatDefault ||
		format == TextResponseFormatJSON
}

type TextRequest struct {
	Messages       []TextMessage
	Tools          []ToolDefinition
	ToolChoice     ToolChoice
	ResponseFormat TextResponseFormat
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// TextResult contains only provider-neutral completion metadata needed for
// later run auditing. Hidden reasoning is intentionally not represented.
type TextResult struct {
	ID           string
	Provider     string
	Model        string
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        TokenUsage
}

func ValidateTextRequest(request TextRequest) error {
	if len(request.Messages) == 0 {
		return errors.New("text generation requires at least one message")
	}
	if !request.ResponseFormat.Valid() {
		return errors.New("text generation response format is unsupported")
	}
	toolNames := make(map[string]struct{}, len(request.Tools))
	for index, definition := range request.Tools {
		if err := validateToolDefinition(definition); err != nil {
			return fmt.Errorf("text generation tool %d is invalid: %w", index, err)
		}
		if _, exists := toolNames[definition.Name]; exists {
			return fmt.Errorf("text generation tool %d duplicates name %q", index, definition.Name)
		}
		toolNames[definition.Name] = struct{}{}
	}
	if err := validateToolChoice(request.ToolChoice, toolNames); err != nil {
		return err
	}

	knownCalls := make(map[string]struct{})
	resolvedCalls := make(map[string]struct{})
	for index, message := range request.Messages {
		switch message.Role {
		case TextRoleSystem, TextRoleUser:
			if strings.TrimSpace(message.Content) == "" {
				return fmt.Errorf("text generation message %d has empty content", index)
			}
			if message.ToolCallID != "" || len(message.ToolCalls) != 0 {
				return fmt.Errorf("text generation message %d has invalid tool metadata", index)
			}
		case TextRoleAssistant:
			if message.ToolCallID != "" {
				return fmt.Errorf("text generation message %d has an unexpected tool call ID", index)
			}
			if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
				return fmt.Errorf("text generation message %d has no content or tool calls", index)
			}
			for callIndex, call := range message.ToolCalls {
				if err := ValidateToolCall(call); err != nil {
					return fmt.Errorf(
						"text generation message %d tool call %d is invalid: %w",
						index,
						callIndex,
						err,
					)
				}
				if _, exists := knownCalls[call.ID]; exists {
					return fmt.Errorf(
						"text generation message %d duplicates tool call ID %q",
						index,
						call.ID,
					)
				}
				knownCalls[call.ID] = struct{}{}
			}
		case TextRoleTool:
			if strings.TrimSpace(message.Content) == "" ||
				!validIdentifier(message.ToolCallID) ||
				len(message.ToolCalls) != 0 {
				return fmt.Errorf("text generation message %d has an invalid tool result", index)
			}
			if _, exists := knownCalls[message.ToolCallID]; !exists {
				return fmt.Errorf(
					"text generation message %d references an unknown tool call",
					index,
				)
			}
			if _, exists := resolvedCalls[message.ToolCallID]; exists {
				return fmt.Errorf(
					"text generation message %d duplicates a tool result",
					index,
				)
			}
			resolvedCalls[message.ToolCallID] = struct{}{}
		default:
			return fmt.Errorf("text generation message %d has an unsupported role", index)
		}
	}
	if len(knownCalls) != len(resolvedCalls) {
		return errors.New("text generation has unresolved tool calls")
	}
	finalRole := request.Messages[len(request.Messages)-1].Role
	if finalRole != TextRoleUser && finalRole != TextRoleTool {
		return errors.New("text generation requires the final message to have the user or tool role")
	}
	return nil
}

func validateToolChoice(
	choice ToolChoice,
	toolNames map[string]struct{},
) error {
	switch choice.Mode {
	case "":
		if choice.Name != "" {
			return errors.New("text generation default tool choice must not name a tool")
		}
	case ToolChoiceAuto, ToolChoiceNone:
		if choice.Name != "" {
			return errors.New("text generation tool choice must not name a tool")
		}
	case ToolChoiceRequired:
		if choice.Name != "" || len(toolNames) == 0 {
			return errors.New("text generation required tool choice needs available tools only")
		}
	case ToolChoiceSpecific:
		if !validToolName(choice.Name) {
			return errors.New("text generation specific tool choice requires a valid tool name")
		}
		if _, exists := toolNames[choice.Name]; !exists {
			return errors.New("text generation specific tool choice references an unavailable tool")
		}
	default:
		return errors.New("text generation tool choice mode is unsupported")
	}
	return nil
}

func ValidateToolCall(call ToolCall) error {
	if !validIdentifier(call.ID) || !validToolName(call.Name) {
		return errors.New("tool call requires a valid ID and name")
	}
	var arguments map[string]any
	if len(call.Arguments) == 0 ||
		json.Unmarshal(call.Arguments, &arguments) != nil ||
		arguments == nil {
		return errors.New("tool call arguments must be a JSON object")
	}
	return nil
}

func validateToolDefinition(definition ToolDefinition) error {
	if !validToolName(definition.Name) ||
		strings.TrimSpace(definition.Description) == "" ||
		definition.InputSchema == nil {
		return errors.New("tool definition requires a name, description, and input schema")
	}
	if _, err := json.Marshal(definition.InputSchema); err != nil {
		return errors.New("tool definition input schema must be JSON serializable")
	}
	return nil
}

func validToolName(name string) bool {
	if !validIdentifier(name) {
		return false
	}
	first := name[0]
	last := name[len(name)-1]
	return isASCIIAlphaNumeric(first) &&
		isASCIIAlphaNumeric(last) &&
		!strings.Contains(name, "..")
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if !isASCIIAlphaNumeric(character) &&
			character != '.' &&
			character != '_' &&
			character != '-' &&
			character != ':' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}

type ErrorKind string

const (
	ErrorInvalidRequest      ErrorKind = "invalid_request"
	ErrorConfiguration       ErrorKind = "configuration"
	ErrorAuthentication      ErrorKind = "authentication"
	ErrorAuthorization       ErrorKind = "authorization"
	ErrorQuotaExhausted      ErrorKind = "quota_exhausted"
	ErrorRateLimited         ErrorKind = "rate_limited"
	ErrorTimeout             ErrorKind = "timeout"
	ErrorProviderUnavailable ErrorKind = "provider_unavailable"
	ErrorInvalidResponse     ErrorKind = "invalid_response"
	ErrorCancelled           ErrorKind = "cancelled"
)

func (kind ErrorKind) Retryable() bool {
	switch kind {
	case ErrorRateLimited,
		ErrorTimeout,
		ErrorProviderUnavailable,
		ErrorInvalidResponse,
		// ErrorCancelled currently means caller/transport cancellation. There
		// is no accepted business-level Run cancellation command.
		ErrorCancelled:
		return true
	default:
		return false
	}
}

// GenerationError exposes stable application semantics without carrying the
// provider's free-form message, request body, prompt, or credentials.
type GenerationError struct {
	Kind         ErrorKind
	StatusCode   int
	ProviderCode string
	RequestID    string
	cause        error
}

func NewGenerationError(
	kind ErrorKind,
	statusCode int,
	providerCode string,
	requestID string,
	cause error,
) *GenerationError {
	return &GenerationError{
		Kind:         kind,
		StatusCode:   statusCode,
		ProviderCode: providerCode,
		RequestID:    requestID,
		cause:        cause,
	}
}

func (e *GenerationError) Error() string {
	if e == nil {
		return "text generation failed"
	}
	return "text generation failed: " + string(e.Kind)
}

func (e *GenerationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *GenerationError) Retryable() bool {
	return e != nil && e.Kind.Retryable()
}

// StableCategory lets owning application modules persist provider-neutral
// failure classification through a structural error port. It deliberately
// exposes only the bounded machine category, not provider payloads.
func (e *GenerationError) StableCategory() string {
	if e == nil {
		return ""
	}
	return string(e.Kind)
}
