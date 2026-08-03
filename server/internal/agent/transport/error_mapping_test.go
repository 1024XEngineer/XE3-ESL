package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/voice"
	"github.com/gin-gonic/gin"
)

func TestInvalidStoredContextRemainsAnInternalError(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	(&HTTPHandler{correlationID: func() string { return "test-correlation" }}).
		writeAgentError(context, agentcontext.ErrInvalidContext)

	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf(
			"invalid stored Context response = %d %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
}

func TestPracticeVoiceRepositoryErrorRemainsInternal(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	(&HTTPHandler{correlationID: func() string { return "test-correlation" }}).
		writeVoiceError(context, practicevoice.ErrRepository)

	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf(
			"Practice Voice repository response = %d %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
}
