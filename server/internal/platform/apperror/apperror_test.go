package apperror_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
)

func TestEveryPublicCategoryIsValid(t *testing.T) {
	t.Parallel()

	categories := []apperror.Category{
		apperror.InvalidArgument,
		apperror.PayloadTooLarge,
		apperror.Unauthenticated,
		apperror.PermissionDenied,
		apperror.NotFound,
		apperror.AlreadyExists,
		apperror.Conflict,
		apperror.FailedPrecondition,
		apperror.UnprocessableEntity,
		apperror.ResourceExhausted,
		apperror.DeadlineExceeded,
		apperror.Unimplemented,
		apperror.Unavailable,
		apperror.Internal,
	}

	for _, category := range categories {
		category := category
		t.Run(string(category), func(t *testing.T) {
			t.Parallel()
			if !category.Valid() {
				t.Fatalf("expected category %q to be valid", category)
			}
		})
	}

	if apperror.Category("future_category").Valid() {
		t.Fatal("unexpected unknown category acceptance")
	}
}

func TestNewUsesSafeDefaultsAndQueries(t *testing.T) {
	t.Parallel()

	appError := apperror.New(
		apperror.NotFound,
		"goal_not_found",
		"Goal was not found.",
	)

	if appError.Category() != apperror.NotFound {
		t.Fatalf("unexpected category: %q", appError.Category())
	}
	if appError.Code() != "goal_not_found" {
		t.Fatalf("unexpected code: %q", appError.Code())
	}
	if appError.Message() != "Goal was not found." {
		t.Fatalf("unexpected message: %q", appError.Message())
	}
	if appError.Error() != "Goal was not found." {
		t.Fatalf("unexpected Error result: %q", appError.Error())
	}
	if appError.Retryable() {
		t.Fatal("new errors must be non-retryable by default")
	}
	if appError.Details() != nil {
		t.Fatalf("new errors must have no details by default: %#v", appError.Details())
	}
	if appError.Unwrap() != nil {
		t.Fatalf("new errors must have no cause by default: %v", appError.Unwrap())
	}
	if !apperror.IsCategory(appError, apperror.NotFound) {
		t.Fatal("expected category query to match")
	}
	if apperror.IsCategory(appError, apperror.Internal) {
		t.Fatal("unexpected category query match")
	}
}

func TestOptionsPreserveErrorChain(t *testing.T) {
	t.Parallel()

	cause := errors.New("database DSN must stay internal")
	appError := apperror.New(
		apperror.Unavailable,
		"goal_temporarily_unavailable",
		"Goal is temporarily unavailable.",
		apperror.WithRetryable(true),
		apperror.WithCause(cause),
		apperror.WithDetails(apperror.Detail{
			Field:  "goal_id",
			Reason: "Goal is not currently available.",
		}),
		nil,
	)
	wrapped := fmt.Errorf("application boundary: %w", appError)

	if !errors.Is(wrapped, cause) {
		t.Fatal("errors.Is must reach the internal cause")
	}

	var target *apperror.AppError
	if !errors.As(wrapped, &target) || target != appError {
		t.Fatalf("errors.As did not recover the AppError: %#v", target)
	}

	from, ok := apperror.From(wrapped)
	if !ok || from != appError {
		t.Fatalf("From did not recover the AppError: %#v, %v", from, ok)
	}
	if !appError.Retryable() {
		t.Fatal("expected retryable option to be applied")
	}
	if appError.Error() == cause.Error() {
		t.Fatal("Error must not expose the internal cause")
	}
}

func TestDetailsAreDeepCopiedAtEveryBoundary(t *testing.T) {
	t.Parallel()

	input := []apperror.Detail{{
		Field:  "body.email",
		Reason: "Email is invalid.",
	}}
	option := apperror.WithDetails(input...)
	input[0].Field = "mutated before construction"

	appError := apperror.New(
		apperror.InvalidArgument,
		"invalid_request",
		"Request validation failed.",
		option,
	)
	input[0].Reason = "mutated after construction"

	want := []apperror.Detail{{
		Field:  "body.email",
		Reason: "Email is invalid.",
	}}
	if got := appError.Details(); !reflect.DeepEqual(got, want) {
		t.Fatalf("constructor retained caller-owned detail storage: %#v", got)
	}

	output := appError.Details()
	output[0].Field = "mutated output"
	if got := appError.Details(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Details returned mutable internal storage: %#v", got)
	}
}

func TestNilAppErrorAccessorsAreSafe(t *testing.T) {
	t.Parallel()

	var appError *apperror.AppError
	if appError.Error() != "application error" {
		t.Fatalf("unexpected nil error string: %q", appError.Error())
	}
	if appError.Unwrap() != nil ||
		appError.Category() != "" ||
		appError.Code() != "" ||
		appError.Message() != "" ||
		appError.Retryable() ||
		appError.Details() != nil {
		t.Fatal("nil AppError accessors must return zero values")
	}
}

func TestFromRejectsOrdinaryAndNilErrors(t *testing.T) {
	t.Parallel()

	if appError, ok := apperror.From(errors.New("ordinary")); ok || appError != nil {
		t.Fatalf("ordinary error unexpectedly matched: %#v, %v", appError, ok)
	}
	if appError, ok := apperror.From(nil); ok || appError != nil {
		t.Fatalf("nil error unexpectedly matched: %#v, %v", appError, ok)
	}
}
