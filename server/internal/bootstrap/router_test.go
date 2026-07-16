package bootstrap_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/assistant"
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

func TestHealthIncludesRegisteredModules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouter(logger,
		assistant.New(),
		preparation.New(),
		practice.New(),
		conversation.New(),
		review.New(),
	)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var body struct {
		Status  string   `json:"status"`
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantModules := []string{"assistant", "preparation", "practice", "conversation", "review"}
	if body.Status != "ok" || !reflect.DeepEqual(body.Modules, wantModules) {
		t.Fatalf("unexpected health response: %#v", body)
	}
}

func TestUnknownRouteUsesStableErrorShape(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "route_not_found" {
		t.Fatalf("unexpected error code: %q", body.Error.Code)
	}
}
