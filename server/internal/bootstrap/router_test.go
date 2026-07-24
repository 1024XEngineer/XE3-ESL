package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

type readinessChecker func(context.Context) error

func (checker readinessChecker) Ping(ctx context.Context) error {
	return checker(ctx)
}

func TestHealthIncludesRegisteredModules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouter(logger,
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

	wantModules := []string{"preparation", "practice", "conversation", "review"}
	if body.Status != "ok" || !reflect.DeepEqual(body.Modules, wantModules) {
		t.Fatalf("unexpected health response: %#v", body)
	}
}

func TestHealthDoesNotDependOnDatabaseReadiness(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouterWithReadiness(
		logger,
		readinessChecker(func(context.Context) error {
			return errors.New("database host and credentials must not leak")
		}),
	)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestReadinessReportsReadyDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouterWithReadiness(
		logger,
		readinessChecker(func(context.Context) error { return nil }),
	)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ready" || body.Checks["database"] != "ready" {
		t.Fatalf("unexpected readiness response: %#v", body)
	}
}

func TestReadinessHidesDatabaseError(t *testing.T) {
	const sensitiveError = "postgres://user:password@database.internal/speakup"

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouterWithReadiness(
		logger,
		readinessChecker(func(context.Context) error {
			return errors.New(sensitiveError)
		}),
	)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			response.Code,
		)
	}
	if body := response.Body.String(); body == "" || strings.Contains(body, sensitiveError) {
		t.Fatalf("unexpected readiness response: %q", body)
	}
}

func TestReadinessWithoutCheckerIsUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			response.Code,
		)
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
