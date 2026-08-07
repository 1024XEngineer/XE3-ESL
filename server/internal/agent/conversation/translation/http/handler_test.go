package translationhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agenttranslation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/translation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func TestTranslationReturnsOwnedMessageResultWithoutCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &applicationStub{result: agenttranslation.Result{
		MessageID:      "40000000-0000-4000-8000-000000000001",
		TargetLanguage: "zh-CN",
		Content:        "保持回答简洁。",
	}}
	handler, err := NewHandler(application, httpresponse.NewRenderer(func() string {
		return "correlation-id"
	}))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(requestcontext.WithActor(
			c.Request.Context(),
			requestcontext.Actor{UserID: "user", SessionID: "session"},
		))
		c.Next()
	})
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-messages/40000000-0000-4000-8000-000000000001/translation",
		nil,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response = %d headers=%v", response.Code, response.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["message_id"] != application.result.MessageID ||
		body["target_language"] != "zh-CN" ||
		body["translation"] != application.result.Content {
		t.Fatalf("body = %#v", body)
	}
}

type applicationStub struct {
	result agenttranslation.Result
	err    error
}

func (application *applicationStub) Translate(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
) (agenttranslation.Result, error) {
	return application.result, application.err
}
