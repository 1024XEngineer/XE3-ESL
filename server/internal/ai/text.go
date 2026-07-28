package ai

import (
	"context"
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

type TextRole string

const (
	TextRoleSystem    TextRole = "system"
	TextRoleUser      TextRole = "user"
	TextRoleAssistant TextRole = "assistant"
)

type TextMessage struct {
	Role    TextRole
	Content string
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
	for index, message := range request.Messages {
		switch message.Role {
		case TextRoleSystem, TextRoleUser, TextRoleAssistant:
		default:
			return fmt.Errorf("text generation message %d has an unsupported role", index)
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("text generation message %d has empty content", index)
		}
	}
	if request.Messages[len(request.Messages)-1].Role != TextRoleUser {
		return errors.New("text generation requires the final message to have the user role")
	}
	return nil
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
