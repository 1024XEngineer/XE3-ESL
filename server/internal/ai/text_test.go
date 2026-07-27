package ai

import (
	"context"
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
