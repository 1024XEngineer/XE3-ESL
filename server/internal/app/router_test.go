package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
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

type observabilityRoutes struct{}

func (observabilityRoutes) RegisterRoutes(router *gin.Engine) {
	router.GET("/users/:user_id", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/failure", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})
	router.GET("/panic", func(*gin.Context) {
		panic("provider-secret-must-not-appear")
	})
}

func TestHealthIncludesRegisteredModules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := app.NewRouter(logger,
		preparation.New(),
		practice.New(),
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

	wantModules := []string{"preparation", "practice"}
	if body.Status != "ok" || !reflect.DeepEqual(body.Modules, wantModules) {
		t.Fatalf("unexpected health response: %#v", body)
	}
}

func TestHealthDoesNotDependOnDatabaseReadiness(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := app.NewRouterWithReadiness(
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
	router := app.NewRouterWithReadinessAndRoutes(
		logger,
		nil,
		[]app.RouteRegistrar{routedModule{}},
		preparation.New(),
		practice.New(),
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
	wantModules := []string{"preparation", "practice"}
	if !reflect.DeepEqual(healthBody.Modules, wantModules) {
		t.Fatalf("health modules = %#v, want %#v", healthBody.Modules, wantModules)
	}
}

func TestRequestLoggerNeverLogsAuthorization(t *testing.T) {
	const rawToken = "sess_must_not_appear_in_logs"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := app.NewRouter(logger)

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
	router := app.NewRouterWithReadiness(
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
	router := app.NewRouterWithReadiness(
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
	router := app.NewRouter(logger)

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
	router := app.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}

	var body struct {
		Error struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			Retryable     bool   `json:"retryable"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "resource_not_found" {
		t.Fatalf("unexpected error code: %q", body.Error.Code)
	}
	if body.Error.Message != "Resource was not found." {
		t.Fatalf("unexpected error message: %q", body.Error.Message)
	}
	if body.Error.Retryable {
		t.Fatal("route-not-found response must not be retryable")
	}
	if strings.TrimSpace(body.Error.CorrelationID) == "" {
		t.Fatal("route-not-found response must include a correlation ID")
	}
}

func TestRequestIDCorrelatesResponseLogAndError(t *testing.T) {
	const requestID = "client.request_123-abc"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := app.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-ID", requestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != requestID {
		t.Fatalf("response request ID = %q, want %q", got, requestID)
	}
	var body struct {
		Error struct {
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.CorrelationID != requestID {
		t.Fatalf(
			"error correlation ID = %q, want %q",
			body.Error.CorrelationID,
			requestID,
		)
	}

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode request log: %v", err)
	}
	if entry["request_id"] != requestID || entry["route"] != "<unmatched>" {
		t.Fatalf("unexpected request log: %#v", entry)
	}
	if _, exists := entry["path"]; exists {
		t.Fatalf("request log retained raw path field: %#v", entry)
	}
}

func TestUnsafeRequestIDIsReplacedWithoutLeakage(t *testing.T) {
	unsafeRequestID := strings.Repeat("private-user@example.com/", 64)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := app.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Request-ID", unsafeRequestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == unsafeRequestID || !safeRequestID(requestID) {
		t.Fatalf("unsafe replacement request ID: %q", requestID)
	}
	if strings.Contains(output.String(), unsafeRequestID) {
		t.Fatal("request log leaked rejected request ID")
	}
}

func TestMultipleRequestIDsAreRejected(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := app.NewRouter(logger)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header["X-Request-Id"] = []string{"request-one", "request-two"}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "request-one" || requestID == "request-two" ||
		!safeRequestID(requestID) {
		t.Fatalf("ambiguous request ID was accepted: %q", requestID)
	}
}

func TestDynamicPathUsesRouteTemplateInLogAndMetrics(t *testing.T) {
	const privateValue = "private-user-12345"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router, metrics := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		nil,
		[]app.RouteRegistrar{observabilityRoutes{}},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/users/"+privateValue+"?token=private-query",
		nil,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}

	logOutput := output.String()
	metricsOutput := scrapeMetrics(t, metrics)
	for _, output := range []string{logOutput, metricsOutput} {
		if strings.Contains(output, privateValue) ||
			strings.Contains(output, "private-query") {
			t.Fatalf("observability output leaked request data: %s", output)
		}
	}
	if !strings.Contains(logOutput, `"route":"/users/:user_id"`) {
		t.Fatalf("request log missing route template: %s", logOutput)
	}
	if !strings.Contains(
		metricsOutput,
		`speakup_http_server_requests_total{method="GET",route="/users/:user_id",status_class="2xx"} 1`,
	) {
		t.Fatalf("metrics missing templated request count: %s", metricsOutput)
	}
	if !strings.Contains(
		metricsOutput,
		`speakup_http_server_request_duration_seconds_count{method="GET",route="/users/:user_id"} 1`,
	) {
		t.Fatalf("metrics missing templated duration histogram: %s", metricsOutput)
	}
}

func TestSharedRegistryExportsHTTPAndProviderMetricsFromOneHandler(t *testing.T) {
	registry := prometheus.NewRegistry()
	providerObserver, err := providerobservability.New(registry)
	if err != nil {
		t.Fatalf("provider observability: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, metrics, err := app.NewObservableRouterWithRegistry(
		logger,
		nil,
		registry,
		nil,
	)
	if err != nil {
		t.Fatalf("NewObservableRouterWithRegistry: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	providerObserver.Record(providerobservability.Observation{
		Provider:   providerobservability.ProviderQianwen,
		Capability: providerobservability.CapabilityTextGeneration,
		ErrorKind:  providerobservability.ErrorNone,
	})
	output := scrapeMetrics(t, metrics)
	for _, name := range []string{
		"speakup_http_server_requests_total",
		"speakup_provider_calls_total",
	} {
		if !strings.Contains(output, name) {
			t.Fatalf("shared metrics endpoint missing %q: %s", name, output)
		}
	}
}

func TestFiveHundredsAndFailedReadinessAreMeasured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, metrics := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		readinessChecker(func(context.Context) error {
			return errors.New("database credentials must not appear")
		}),
		[]app.RouteRegistrar{observabilityRoutes{}},
	)

	for _, path := range []string{"/failure", "/readyz", "/panic"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodGet, path, nil),
		)
		if response.Code < http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 5xx", path, response.Code)
		}
	}

	metricsOutput := scrapeMetrics(t, metrics)
	for _, route := range []string{"/failure", "/readyz", "/panic"} {
		want := fmt.Sprintf(
			`speakup_http_server_requests_total{method="GET",route=%q,status_class="5xx"} 1`,
			route,
		)
		if !strings.Contains(metricsOutput, want) {
			t.Fatalf("metrics missing %s 5xx count: %s", route, metricsOutput)
		}
	}
}

func TestPanicValueIsNotLogged(t *testing.T) {
	const sensitivePanic = "provider-secret-must-not-appear"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router, _ := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		nil,
		[]app.RouteRegistrar{observabilityRoutes{}},
	)

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/panic", nil),
	)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			response.Code,
		)
	}
	if strings.Contains(output.String(), sensitivePanic) {
		t.Fatalf("request log leaked panic value: %s", output.String())
	}
}

func TestMetricsEndpointIsSeparateFromPublicRouter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, metrics := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		nil,
		nil,
	)

	publicResponse := httptest.NewRecorder()
	router.ServeHTTP(
		publicResponse,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if publicResponse.Code != http.StatusNotFound {
		t.Fatalf("public /metrics status = %d, want 404", publicResponse.Code)
	}

	metricsResponse := httptest.NewRecorder()
	metrics.ServeHTTP(
		metricsResponse,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("internal /metrics status = %d, want 200", metricsResponse.Code)
	}
	if contentType := metricsResponse.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("metrics content type = %q", contentType)
	}
}

func TestConcurrentRequestsProduceExactMetrics(t *testing.T) {
	const requestCount = 64
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, metrics := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		nil,
		[]app.RouteRegistrar{observabilityRoutes{}},
	)

	statuses := make(chan int, requestCount)
	var requests sync.WaitGroup
	requests.Add(requestCount)
	for index := 0; index < requestCount; index++ {
		go func(index int) {
			defer requests.Done()
			response := httptest.NewRecorder()
			router.ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodGet,
					fmt.Sprintf("/users/user-%d", index),
					nil,
				),
			)
			statuses <- response.Code
		}(index)
	}
	requests.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusNoContent {
			t.Errorf("concurrent request status = %d, want 204", status)
		}
	}

	metricsOutput := scrapeMetrics(t, metrics)
	for _, want := range []string{
		`speakup_http_server_requests_total{method="GET",route="/users/:user_id",status_class="2xx"} 64`,
		`speakup_http_server_request_duration_seconds_count{method="GET",route="/users/:user_id"} 64`,
	} {
		if !strings.Contains(metricsOutput, want) {
			t.Fatalf("concurrent metrics missing %q: %s", want, metricsOutput)
		}
	}
}

func TestUnknownMethodsAndRoutesUseFixedMetricLabels(t *testing.T) {
	const requestCount = 16
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, metrics := app.NewObservableRouterWithReadinessAndRoutes(
		logger,
		nil,
		nil,
	)

	for index := 0; index < requestCount; index++ {
		method := fmt.Sprintf("PRIVATE-METHOD-%d", index)
		path := fmt.Sprintf("/private-user-%d", index)
		response := httptest.NewRecorder()
		router.ServeHTTP(
			response,
			httptest.NewRequest(method, path, nil),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unknown route status = %d, want 404", response.Code)
		}
	}

	metricsOutput := scrapeMetrics(t, metrics)
	want := fmt.Sprintf(
		`speakup_http_server_requests_total{method="UNKNOWN",route="<unmatched>",status_class="4xx"} %d`,
		requestCount,
	)
	if !strings.Contains(metricsOutput, want) {
		t.Fatalf("metrics missing bounded unknown labels: %s", metricsOutput)
	}
	if strings.Contains(metricsOutput, "PRIVATE-METHOD") ||
		strings.Contains(metricsOutput, "private-user") {
		t.Fatalf("metrics retained unbounded input labels: %s", metricsOutput)
	}
}

func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	return response.Body.String()
}

func safeRequestID(requestID string) bool {
	if len(requestID) == 0 || len(requestID) > 64 {
		return false
	}
	for index := 0; index < len(requestID); index++ {
		character := requestID[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			index > 0 && (character == '.' || character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}
