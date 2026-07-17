package apperror_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
)

func TestPublicCodesAreValid(t *testing.T) {
	codes := []apperror.Code{
		apperror.CodeInvalidArgument,
		apperror.CodeUnauthenticated,
		apperror.CodePermissionDenied,
		apperror.CodeNotFound,
		apperror.CodeAlreadyExists,
		apperror.CodeConflict,
		apperror.CodeFailedPrecondition,
		apperror.CodeResourceExhausted,
		apperror.CodeDeadlineExceeded,
		apperror.CodeUnimplemented,
		apperror.CodeUnavailable,
		apperror.CodeInternal,
	}

	for _, code := range codes {
		if !apperror.IsValidCode(code) {
			t.Errorf("expected %q to be valid", code)
		}
	}
	if apperror.IsValidCode("review_analysis_not_found") {
		t.Fatal("business reason must not be accepted as a public code")
	}
}

func TestNewNormalizesInvalidCode(t *testing.T) {
	cause := errors.New("internal diagnostic")
	err := apperror.New(
		"custom_failure",
		"unsafe classification",
		apperror.WithReason("unreviewed_reason"),
		apperror.WithDetail("unsafe detail"),
		apperror.WithDetails(map[string]any{"secret": true}),
		apperror.WithRetryable(true),
		apperror.WithCause(cause),
	)

	if err.Code != apperror.CodeInternal {
		t.Fatalf("expected internal code, got %q", err.Code)
	}
	if err.Message != "" || err.Reason != "" || err.Detail != "" || err.Details != nil || err.Retryable {
		t.Fatalf("invalid code retained public fields: %#v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("invalid code normalization discarded the internal cause")
	}
	if apperror.CodeOf(err) != apperror.CodeInternal {
		t.Fatalf("unexpected CodeOf result: %q", apperror.CodeOf(err))
	}
}

func TestErrorChainAndSafeString(t *testing.T) {
	cause := errors.New("database password=secret")
	appErr := apperror.NotFound(
		"analysis not found",
		apperror.WithReason("review_analysis_not_found"),
		apperror.WithCause(cause),
	)
	wrapped := fmt.Errorf("delivery mapping: %w", appErr)

	if strings.Contains(appErr.Error(), cause.Error()) {
		t.Fatal("Error() leaked the internal cause")
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("errors.Is did not traverse the application error")
	}

	var extracted *apperror.Error
	if !errors.As(wrapped, &extracted) || extracted != appErr {
		t.Fatal("errors.As did not extract the application error")
	}
	if got, ok := apperror.As(wrapped); !ok || got != appErr {
		t.Fatal("apperror.As did not extract the application error")
	}
}

func TestDefaultsAndQueries(t *testing.T) {
	retryable := apperror.New(
		apperror.CodeResourceExhausted,
		"quota exhausted",
		apperror.WithReason("rate_limit_exceeded"),
	)
	if !apperror.IsRetryable(retryable) {
		t.Fatal("resource_exhausted should be retryable by default")
	}
	if apperror.ReasonOf(retryable) != "rate_limit_exceeded" {
		t.Fatalf("unexpected reason: %q", apperror.ReasonOf(retryable))
	}

	overridden := apperror.New(
		apperror.CodeResourceExhausted,
		"hard quota exhausted",
		apperror.WithRetryable(false),
	)
	if apperror.IsRetryable(overridden) {
		t.Fatal("WithRetryable did not override the default")
	}
	if apperror.IsRetryable(errors.New("unknown")) {
		t.Fatal("unknown errors must not be retryable")
	}
	if apperror.CodeOf(errors.New("unknown")) != apperror.CodeInternal {
		t.Fatal("unknown errors must fall back to internal")
	}
	if apperror.ReasonOf(errors.New("unknown")) != "" {
		t.Fatal("unknown errors must not expose a reason")
	}
}

func TestInternalIsNotRetryableByDefault(t *testing.T) {
	if apperror.Internal().Retryable {
		t.Fatal("internal must not be retryable by default")
	}
}

func TestWithDetailsCopiesInputMap(t *testing.T) {
	input := map[string]any{"field": "original"}
	err := apperror.InvalidArgument("invalid input", apperror.WithDetails(input))
	input["field"] = "changed"
	input["extra"] = true

	if got := err.Details["field"]; got != "original" {
		t.Fatalf("details changed with caller map: %#v", err.Details)
	}
	if _, exists := err.Details["extra"]; exists {
		t.Fatalf("details unexpectedly contains caller mutation: %#v", err.Details)
	}
}

func TestEmptyOptionalValuesAreAllowed(t *testing.T) {
	err := apperror.New(
		apperror.CodeInvalidArgument,
		"",
		apperror.WithReason(""),
		apperror.WithDetail(""),
		apperror.WithDetails(nil),
	)

	if err.Reason != "" || err.Detail != "" || err.Details != nil {
		t.Fatalf("unexpected optional values: %#v", err)
	}
}
