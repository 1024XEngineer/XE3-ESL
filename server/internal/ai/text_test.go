package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateTextRequest(t *testing.T) {
	t.Parallel()

	valid := TextRequest{Messages: []TextMessage{
		{Role: TextRoleSystem, Content: "You are a coach."},
		{Role: TextRoleAssistant, Content: "How can I help?"},
		{Role: TextRoleUser, Content: "Help me prepare."},
	}}
	if err := ValidateTextRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := map[string]TextRequest{
		"no messages": {},
		"unknown role": {Messages: []TextMessage{
			{Role: "tool", Content: "not supported"},
			{Role: TextRoleUser, Content: "hello"},
		}},
		"blank content": {Messages: []TextMessage{
			{Role: TextRoleUser, Content: " \n\t"},
		}},
		"final assistant": {Messages: []TextMessage{
			{Role: TextRoleUser, Content: "hello"},
			{Role: TextRoleAssistant, Content: "hi"},
		}},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTextRequest(request); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateTextRequestSupportsToolRoundTrip(t *testing.T) {
	t.Parallel()

	request := TextRequest{
		Messages: []TextMessage{
			{Role: TextRoleUser, Content: "Find my last review."},
			{
				Role: TextRoleAssistant,
				ToolCalls: []ToolCall{{
					ID:        "call-1",
					Name:      "review.search.v1",
					Arguments: json.RawMessage(`{"limit":1}`),
				}},
			},
			{
				Role:       TextRoleTool,
				Content:    `{"reviews":[]}`,
				ToolCallID: "call-1",
			},
		},
		Tools: []ToolDefinition{{
			Name:        "review.search.v1",
			Description: "Search the current user's review summaries.",
			InputSchema: map[string]any{"type": "object"},
		}},
	}

	if err := ValidateTextRequest(request); err != nil {
		t.Fatalf("valid tool round trip rejected: %v", err)
	}
}

func TestValidateTextRequestRejectsInvalidToolData(t *testing.T) {
	t.Parallel()

	tests := map[string]TextRequest{
		"duplicate definitions": {
			Messages: []TextMessage{{Role: TextRoleUser, Content: "hello"}},
			Tools: []ToolDefinition{
				validToolDefinition(),
				validToolDefinition(),
			},
		},
		"invalid definition name": {
			Messages: []TextMessage{{Role: TextRoleUser, Content: "hello"}},
			Tools: []ToolDefinition{{
				Name:        "review search",
				Description: "Search reviews.",
				InputSchema: map[string]any{},
			}},
		},
		"assistant without output": {
			Messages: []TextMessage{
				{Role: TextRoleUser, Content: "hello"},
				{Role: TextRoleAssistant},
			},
		},
		"invalid arguments": {
			Messages: []TextMessage{
				{Role: TextRoleUser, Content: "hello"},
				{
					Role: TextRoleAssistant,
					ToolCalls: []ToolCall{{
						ID:        "call-1",
						Name:      "review.search.v1",
						Arguments: json.RawMessage(`[]`),
					}},
				},
				{Role: TextRoleTool, Content: `{}`, ToolCallID: "call-1"},
			},
		},
		"unknown tool result": {
			Messages: []TextMessage{
				{Role: TextRoleUser, Content: "hello"},
				{Role: TextRoleTool, Content: `{}`, ToolCallID: "call-missing"},
			},
		},
		"unresolved tool call": {
			Messages: []TextMessage{
				{Role: TextRoleUser, Content: "hello"},
				{
					Role: TextRoleAssistant,
					ToolCalls: []ToolCall{{
						ID:        "call-1",
						Name:      "review.search.v1",
						Arguments: json.RawMessage(`{}`),
					}},
				},
				{Role: TextRoleUser, Content: "continue"},
			},
		},
		"duplicate tool result": {
			Messages: []TextMessage{
				{Role: TextRoleUser, Content: "hello"},
				{
					Role: TextRoleAssistant,
					ToolCalls: []ToolCall{{
						ID:        "call-1",
						Name:      "review.search.v1",
						Arguments: json.RawMessage(`{}`),
					}},
				},
				{Role: TextRoleTool, Content: `{}`, ToolCallID: "call-1"},
				{Role: TextRoleTool, Content: `{}`, ToolCallID: "call-1"},
			},
		},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTextRequest(request); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func validToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "review.search.v1",
		Description: "Search reviews.",
		InputSchema: map[string]any{"type": "object"},
	}
}

func TestGenerationErrorHasStableSafeSemantics(t *testing.T) {
	t.Parallel()

	cause := context.DeadlineExceeded
	err := NewGenerationError(
		ErrorTimeout,
		0,
		"RequestTimeOut",
		"request-safe-id",
		cause,
	)
	if got := err.Error(); got != "text generation failed: timeout" {
		t.Fatalf("unexpected safe error string: %q", got)
	}
	if !err.Retryable() {
		t.Fatal("timeout must be retryable")
	}
	if !errors.Is(err, cause) {
		t.Fatal("generation error must retain the machine-readable cause")
	}
}

func TestErrorKindRetryability(t *testing.T) {
	t.Parallel()

	tests := map[ErrorKind]bool{
		ErrorInvalidRequest:      false,
		ErrorConfiguration:       false,
		ErrorAuthentication:      false,
		ErrorAuthorization:       false,
		ErrorQuotaExhausted:      false,
		ErrorRateLimited:         true,
		ErrorTimeout:             true,
		ErrorProviderUnavailable: true,
		ErrorInvalidResponse:     true,
		ErrorCancelled:           true,
	}
	for kind, expected := range tests {
		if got := kind.Retryable(); got != expected {
			t.Errorf("%s retryable = %v, want %v", kind, got, expected)
		}
	}
}
