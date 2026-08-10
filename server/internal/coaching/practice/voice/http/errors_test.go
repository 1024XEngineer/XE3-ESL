package voicehttp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/gin-gonic/gin"
)

func TestRepositoryErrorRemainsInternal(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := Handler{errors: httpresponse.NewRenderer(
		func() string { return "test-correlation" },
	)}
	handler.write(context, mapError(practicevoice.ErrRepository))
	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("Practice Voice repository response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderErrorUsesPracticeVoiceContract(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      practicevoice.ProviderErrorKind
		code      string
		retryable bool
	}{
		{
			name: "quota",
			kind: practicevoice.ProviderErrorQuotaExhausted,
			code: "quota_exhausted",
		},
		{
			name:      "timeout",
			kind:      practicevoice.ProviderErrorTimeout,
			code:      "provider_unavailable",
			retryable: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			handler := Handler{errors: httpresponse.NewRenderer(
				func() string { return "test-correlation" },
			)}
			handler.write(context, mapError(practicevoice.NewProviderError(
				practicevoice.ProviderOperationTranscription,
				test.kind,
				"provider-request",
				nil,
			)))

			body := recorder.Body.String()
			if recorder.Code != http.StatusServiceUnavailable ||
				!strings.Contains(body, `"code":"`+test.code+`"`) ||
				!strings.Contains(body, `"retryable":`+
					strconv.FormatBool(test.retryable)) {
				t.Fatalf("provider response = %d %s", recorder.Code, body)
			}
			if test.retryable && recorder.Header().Get("Retry-After") != "1" {
				t.Fatalf("Retry-After = %q", recorder.Header().Get("Retry-After"))
			}
		})
	}
}

func TestActivationErrorReportsDiscardedEmptySession(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	handler := Handler{errors: httpresponse.NewRenderer(
		func() string { return "test-correlation" },
	)}
	handler.write(context, mapError(practicevoice.NewActivationError(
		practicevoice.NewProviderError(
			practicevoice.ProviderOperationQuestionGeneration,
			practicevoice.ProviderErrorUnavailable,
			"provider-request",
			nil,
		),
	)))

	body := recorder.Body.String()
	if recorder.Code != http.StatusServiceUnavailable ||
		!strings.Contains(body, `"code":"practice_activation_failed"`) ||
		!strings.Contains(body, `"retryable":true`) {
		t.Fatalf("activation response = %d %s", recorder.Code, body)
	}
}
