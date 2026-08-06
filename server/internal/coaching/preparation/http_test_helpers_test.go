package preparation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// These helpers remain shared by the JobTarget and Plan transport tests until
// those handlers complete their move into transport/http.
func serveProfileHTTPRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	contentType string,
	idempotencyKey string,
	actor *requestcontext.Actor,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if actor != nil {
		request = request.WithContext(
			requestcontext.WithActor(request.Context(), *actor),
		)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertProfileHTTPError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body)
	}
	assertProfileHTTPNoStore(t, response)
	var body struct {
		Error struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			Retryable     bool   `json:"retryable"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != code || body.Error.Message == "" ||
		body.Error.Retryable || body.Error.CorrelationID == "" {
		t.Fatalf("error response = %#v", body)
	}
}

func assertProfileHTTPNoStore(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %#v", response.Header())
	}
}

func profileHTTPActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "session-1",
	}
}
