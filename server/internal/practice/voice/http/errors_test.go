package voicehttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/voice"
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
