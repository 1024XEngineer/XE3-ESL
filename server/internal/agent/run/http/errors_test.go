package runhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/gin-gonic/gin"
)

func TestInvalidStoredContextRemainsAnInternalError(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := Handler{errors: httpresponse.NewRenderer(
		func() string { return "test-correlation" },
	)}
	handler.write(context, mapError(agentcontext.ErrInvalidContext))
	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("invalid stored Context response = %d %s", recorder.Code, recorder.Body.String())
	}
}
