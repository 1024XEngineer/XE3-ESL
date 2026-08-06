package httpresponse_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
)

func TestRendererAcceptsMatchingCanonicalCodeStatusPairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		category apperror.Category
		code     string
		status   int
	}{
		{apperror.InvalidArgument, "invalid_request", http.StatusBadRequest},
		{
			apperror.PayloadTooLarge,
			"image_too_large",
			http.StatusRequestEntityTooLarge,
		},
		{apperror.Unauthenticated, "invalid_credentials", http.StatusUnauthorized},
		{
			apperror.PermissionDenied,
			"practice_participant_not_authorized",
			http.StatusForbidden,
		},
		{apperror.NotFound, "resource_not_found", http.StatusNotFound},
		{apperror.AlreadyExists, "account_registration_unavailable", http.StatusConflict},
		{apperror.Conflict, "resource_conflict", http.StatusConflict},
		{
			apperror.UnprocessableEntity,
			"evaluation_strategy_not_available",
			http.StatusUnprocessableEntity,
		},
		{apperror.ResourceExhausted, "rate_limited", http.StatusTooManyRequests},
		{apperror.Internal, "internal_error", http.StatusInternalServerError},
	}

	for _, test := range tests {
		test := test
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()
			response := render(
				t,
				httpresponse.NewRenderer(func() string { return "corr_category" }),
				context.Background(),
				apperror.New(
					test.category,
					test.code,
					"Public error.",
					apperror.WithRetryable(true),
				),
			)

			if response.Code != test.status {
				t.Fatalf("expected status %d, got %d", test.status, response.Code)
			}
			payload := decodeResponse(t, response)
			if payload.Error.Code != test.code ||
				payload.Error.Message != "Public error." ||
				!payload.Error.Retryable ||
				payload.Error.CorrelationID != "corr_category" {
				t.Fatalf("unexpected payload: %#v", payload)
			}
		})
	}
}

func TestRendererWritesOnlyCanonicalFields(t *testing.T) {
	t.Parallel()

	sensitiveCause := errors.New("postgres://admin:secret@private.internal/app")
	response := render(
		t,
		httpresponse.NewRenderer(func() string { return "corr_canonical" }),
		context.Background(),
		apperror.New(
			apperror.InvalidArgument,
			"invalid_request",
			"Request validation failed.",
			apperror.WithCause(sensitiveCause),
			apperror.WithDetails(apperror.Detail{
				Field:  "body.email",
				Reason: "Email is invalid.",
			}),
		),
	)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	if strings.Contains(response.Body.String(), sensitiveCause.Error()) {
		t.Fatalf("response leaked cause: %s", response.Body.String())
	}

	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode response document: %v", err)
	}
	if keys := sortedKeys(document); !reflect.DeepEqual(keys, []string{"error"}) {
		t.Fatalf("unexpected top-level fields: %#v", keys)
	}
	errorDocument, ok := document["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is not an object: %#v", document["error"])
	}
	wantKeys := []string{"code", "correlation_id", "details", "message", "retryable"}
	if keys := sortedKeys(errorDocument); !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("unexpected error fields: %#v", keys)
	}
	details, ok := errorDocument["details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("unexpected details: %#v", errorDocument["details"])
	}
	detail, ok := details[0].(map[string]any)
	if !ok {
		t.Fatalf("detail is not an object: %#v", details[0])
	}
	if keys := sortedKeys(detail); !reflect.DeepEqual(keys, []string{"field", "reason"}) {
		t.Fatalf("unexpected detail fields: %#v", keys)
	}
}

func TestRendererOmitsEmptyDetails(t *testing.T) {
	t.Parallel()

	response := render(
		t,
		httpresponse.NewRenderer(func() string { return "corr_no_details" }),
		context.Background(),
		apperror.New(apperror.NotFound, "resource_not_found", "Resource was not found."),
	)

	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode response document: %v", err)
	}
	errorDocument := document["error"].(map[string]any)
	if _, exists := errorDocument["details"]; exists {
		t.Fatalf("empty details must be omitted: %#v", errorDocument)
	}
}

func TestRendererPrefersRequestCorrelationID(t *testing.T) {
	t.Parallel()

	var generatorCalls atomic.Int64
	renderer := httpresponse.NewRenderer(func() string {
		generatorCalls.Add(1)
		return "corr_generated"
	})
	ctx := httpresponse.WithCorrelationID(context.Background(), "corr_existing")
	response := render(
		t,
		renderer,
		ctx,
		apperror.New(apperror.NotFound, "resource_not_found", "Resource was not found."),
	)

	payload := decodeResponse(t, response)
	if payload.Error.CorrelationID != "corr_existing" {
		t.Fatalf("unexpected correlation ID: %q", payload.Error.CorrelationID)
	}
	if generatorCalls.Load() != 0 {
		t.Fatalf("generator called despite existing ID: %d", generatorCalls.Load())
	}

	if correlationID, ok := httpresponse.CorrelationIDFromContext(ctx); !ok ||
		correlationID != "corr_existing" {
		t.Fatalf("context query failed: %q, %v", correlationID, ok)
	}
}

func TestRendererGeneratesNonEmptyCorrelationID(t *testing.T) {
	t.Parallel()

	response := render(
		t,
		httpresponse.NewRenderer(func() string { return "" }),
		context.Background(),
		apperror.New(apperror.NotFound, "resource_not_found", "Resource was not found."),
	)
	payload := decodeResponse(t, response)
	if strings.TrimSpace(payload.Error.CorrelationID) == "" {
		t.Fatal("correlation ID must not be empty")
	}
}

func TestRendererSafelyFallsBackForUnrenderableErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "ordinary error",
			err:  errors.New("raw provider token and stack"),
		},
		{
			name: "nil error",
			err:  nil,
		},
		{
			name: "unknown category",
			err: apperror.New(
				apperror.Category("future"),
				"internal_error",
				"Future error.",
			),
		},
		{
			name: "empty code",
			err:  apperror.New(apperror.Internal, "", "Public error."),
		},
		{
			name: "invalid code",
			err:  apperror.New(apperror.Internal, "Internal Error", "Public error."),
		},
		{
			name: "well formed but undeclared code",
			err: apperror.New(
				apperror.NotFound,
				"resource_not_foud",
				"Resource was not found.",
			),
		},
		{
			name: "canonical code with mismatched category",
			err: apperror.New(
				apperror.NotFound,
				"internal_error",
				"Resource was not found.",
			),
		},
		{
			name: "empty message",
			err:  apperror.New(apperror.Internal, "internal_error", " \t"),
		},
		{
			name: "control in message",
			err:  apperror.New(apperror.Internal, "internal_error", "Public\nsecret"),
		},
		{
			name: "empty detail field",
			err: apperror.New(
				apperror.InvalidArgument,
				"invalid_request",
				"Request validation failed.",
				apperror.WithDetails(apperror.Detail{Reason: "Invalid."}),
			),
		},
		{
			name: "control in detail reason",
			err: apperror.New(
				apperror.InvalidArgument,
				"invalid_request",
				"Request validation failed.",
				apperror.WithDetails(apperror.Detail{
					Field:  "body",
					Reason: "Invalid.\nraw SQL",
				}),
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := render(
				t,
				httpresponse.NewRenderer(func() string { return "corr_fallback" }),
				context.Background(),
				test.err,
			)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusInternalServerError,
					response.Code,
				)
			}
			payload := decodeResponse(t, response)
			if payload.Error.Code != "internal_error" ||
				payload.Error.Message != "Internal server error." ||
				payload.Error.Retryable ||
				payload.Error.CorrelationID != "corr_fallback" ||
				payload.Error.Details != nil {
				t.Fatalf("unexpected safe fallback: %#v", payload)
			}
			if test.err != nil && strings.Contains(response.Body.String(), test.err.Error()) {
				t.Fatalf("response leaked raw error: %s", response.Body.String())
			}
		})
	}
}

func render(
	t *testing.T,
	renderer *httpresponse.Renderer,
	ctx context.Context,
	err error,
) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(response)
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	ginContext.Request = request
	renderer.Write(ginContext, err)
	return response
}

func decodeResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
) httpresponse.ErrorResponse {
	t.Helper()

	var payload httpresponse.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func sortedKeys(document map[string]any) []string {
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	for index := 1; index < len(keys); index++ {
		for current := index; current > 0 && keys[current] < keys[current-1]; current-- {
			keys[current], keys[current-1] = keys[current-1], keys[current]
		}
	}
	return keys
}
