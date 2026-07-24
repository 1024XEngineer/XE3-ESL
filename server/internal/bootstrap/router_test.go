package bootstrap_test

import (
	"bytes"
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
	"github.com/gin-gonic/gin"
)

type readinessChecker func(context.Context) error

func (checker readinessChecker) Ping(ctx context.Context) error {
	return checker(ctx)
}

type routedModule struct{}

func (routedModule) RegisterRoutes(router *gin.Engine) {
	router.GET("/module-route", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
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

func TestModuleCanRegisterProductionRoutes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bootstrap.NewRouterWithReadinessAndRoutes(
		logger,
		nil,
		[]bootstrap.RouteRegistrar{routedModule{}},
		preparation.New(),
		practice.New(),
		conversation.New(),
		review.New(),
	)

	request := httptest.NewRequest(http.MethodGet, "/module-route", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}

	healthRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthResponse := httptest.NewRecorder()
	router.ServeHTTP(healthResponse, healthRequest)
	var healthBody struct {
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal(healthResponse.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	wantModules := []string{"preparation", "practice", "conversation", "review"}
	if !reflect.DeepEqual(healthBody.Modules, wantModules) {
		t.Fatalf("health modules = %#v, want %#v", healthBody.Modules, wantModules)
	}
}

func TestRequestLoggerNeverLogsAuthorization(t *testing.T) {
	const rawToken = "sess_must_not_appear_in_logs"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := bootstrap.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if strings.Contains(output.String(), rawToken) ||
		strings.Contains(output.String(), "Authorization") {
		t.Fatalf("request log leaked credential metadata: %s", output.String())
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
