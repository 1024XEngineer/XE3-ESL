package httpresponse_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
)

func TestEveryPublicCodeHasExplicitStatus(t *testing.T) {
	tests := map[apperror.Code]int{
		apperror.CodeInvalidArgument:    http.StatusBadRequest,
		apperror.CodeUnauthenticated:    http.StatusUnauthorized,
		apperror.CodePermissionDenied:   http.StatusForbidden,
		apperror.CodeNotFound:           http.StatusNotFound,
		apperror.CodeAlreadyExists:      http.StatusConflict,
		apperror.CodeConflict:           http.StatusConflict,
		apperror.CodeFailedPrecondition: http.StatusConflict,
		apperror.CodeResourceExhausted:  http.StatusTooManyRequests,
		apperror.CodeDeadlineExceeded:   http.StatusGatewayTimeout,
		apperror.CodeUnimplemented:      http.StatusNotImplemented,
		apperror.CodeUnavailable:        http.StatusServiceUnavailable,
		apperror.CodeInternal:           http.StatusInternalServerError,
	}

	for code, wantStatus := range tests {
		t.Run(string(code), func(t *testing.T) {
			status, envelope := httpresponse.Render(apperror.New(code, "safe"), "")
			if status != wantStatus {
				t.Fatalf("expected status %d, got %d", wantStatus, status)
			}
			if envelope.Error.Code != code {
				t.Fatalf("expected code %q, got %q", code, envelope.Error.Code)
			}
		})
	}
}

func TestRenderPreservesSafeFieldsAndStructuredDetails(t *testing.T) {
	err := apperror.InvalidArgument(
		"request is invalid",
		apperror.WithReason("request_invalid_json"),
		apperror.WithDetail("field limit must be positive"),
		apperror.WithDetails(map[string]any{
			"field": "limit",
			"min":   float64(1),
		}),
		apperror.WithRetryable(true),
	)

	status, envelope := httpresponse.Render(err, "req_123")
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", status)
	}
	body, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		t.Fatalf("marshal envelope: %v", marshalErr)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	errorBody := decoded["error"].(map[string]any)
	if errorBody["code"] != "invalid_argument" ||
		errorBody["reason"] != "request_invalid_json" ||
		errorBody["message"] != "request is invalid" ||
		errorBody["detail"] != "field limit must be positive" ||
		errorBody["retryable"] != true ||
		errorBody["request_id"] != "req_123" {
		t.Fatalf("unexpected envelope: %#v", errorBody)
	}
	details := errorBody["details"].(map[string]any)
	if details["field"] != "limit" || details["min"] != float64(1) {
		t.Fatalf("details lost structured types: %#v", details)
	}
}

func TestRenderCopiesNestedStructuredDetails(t *testing.T) {
	err := apperror.InvalidArgument(
		"request is invalid",
		apperror.WithDetails(map[string]any{
			"field": map[string]any{"value": "original"},
			"items": []any{map[string]any{"name": "first"}},
		}),
	)

	_, envelope := httpresponse.Render(err, "")
	err.Details["field"].(map[string]any)["value"] = "changed"
	err.Details["items"].([]any)[0].(map[string]any)["name"] = "changed"

	field := envelope.Error.Details["field"].(map[string]any)
	if got := field["value"]; got != "original" {
		t.Fatalf("rendered nested map changed with source error: %#v", envelope.Error.Details)
	}
	items := envelope.Error.Details["items"].([]any)
	if got := items[0].(map[string]any)["name"]; got != "first" {
		t.Fatalf("rendered nested slice changed with source error: %#v", envelope.Error.Details)
	}
}

func TestRenderOmitsEmptyOptionalFields(t *testing.T) {
	_, envelope := httpresponse.Render(apperror.NotFound("missing"), "")
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	text := string(body)
	for _, field := range []string{`"reason"`, `"detail"`, `"details"`, `"request_id"`} {
		if strings.Contains(text, field) {
			t.Fatalf("empty optional field %s was serialized: %s", field, text)
		}
	}
	if !strings.Contains(text, `"retryable":false`) {
		t.Fatalf("retryable must always be serialized: %s", text)
	}
}

func TestCauseNeverAppearsInJSON(t *testing.T) {
	cause := errors.New("postgres password=secret")
	err := apperror.New(
		apperror.CodeUnavailable,
		"service temporarily unavailable",
		apperror.WithCause(cause),
	)

	_, envelope := httpresponse.Render(err, "")
	body, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		t.Fatalf("marshal envelope: %v", marshalErr)
	}
	if strings.Contains(string(body), cause.Error()) || strings.Contains(string(body), "cause") {
		t.Fatalf("cause leaked into response: %s", body)
	}
}

func TestUnknownErrorFallsBackToInternal(t *testing.T) {
	status, envelope := httpresponse.Render(errors.New("sensitive provider failure"), "")

	if status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
	if envelope.Error.Code != apperror.CodeInternal ||
		envelope.Error.Reason != "service_internal_error" ||
		envelope.Error.Retryable {
		t.Fatalf("unexpected internal fallback: %#v", envelope.Error)
	}
	if strings.Contains(envelope.Error.Message, "sensitive") {
		t.Fatalf("unknown error leaked into message: %q", envelope.Error.Message)
	}
}

func TestManuallyConstructedUnknownCodeFallsBackToInternal(t *testing.T) {
	status, envelope := httpresponse.Render(&apperror.Error{
		Code:    "unreviewed_code",
		Message: "must not be exposed",
	}, "")

	if status != http.StatusInternalServerError ||
		envelope.Error.Code != apperror.CodeInternal ||
		envelope.Error.Reason != "service_internal_error" {
		t.Fatalf("unexpected fallback: status=%d body=%#v", status, envelope.Error)
	}
}

func TestInvalidCodeCreatedThroughConstructorUsesSafeFallback(t *testing.T) {
	status, envelope := httpresponse.Render(
		apperror.New("unreviewed_code", "must not be exposed"),
		"",
	)

	if status != http.StatusInternalServerError ||
		envelope.Error.Code != apperror.CodeInternal ||
		envelope.Error.Reason != "service_internal_error" ||
		strings.Contains(envelope.Error.Message, "exposed") {
		t.Fatalf("unexpected fallback: status=%d body=%#v", status, envelope.Error)
	}
}

func TestEmptyMessageUsesSafeDefault(t *testing.T) {
	_, envelope := httpresponse.Render(apperror.NotFound(""), "")
	if envelope.Error.Message != "resource not found" {
		t.Fatalf("unexpected default message: %q", envelope.Error.Message)
	}
}

func TestWriteUsesGinAndInjectsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	httpresponse.Write(context, apperror.NotFound("missing"), "req_write")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"request_id":"req_write"`) {
		t.Fatalf("request id missing from response: %s", recorder.Body.String())
	}
}
