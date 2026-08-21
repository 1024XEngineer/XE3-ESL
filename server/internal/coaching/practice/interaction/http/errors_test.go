package voicehttp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
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
	handler.write(context, mapError(practiceinteraction.ErrRepository))
	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("Practice Interaction repository response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderErrorUsesPracticeVoiceContract(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      practiceinteraction.ProviderErrorKind
		code      string
		retryable bool
	}{
		{
			name: "quota",
			kind: practiceinteraction.ProviderErrorQuotaExhausted,
			code: "quota_exhausted",
		},
		{
			name:      "timeout",
			kind:      practiceinteraction.ProviderErrorTimeout,
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
			handler.write(context, mapError(practiceinteraction.NewProviderError(
				practiceinteraction.ProviderOperationTranscription,
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
