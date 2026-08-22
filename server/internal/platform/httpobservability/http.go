// Package httpobservability provides bounded HTTP request correlation,
// logging, and metrics for the service boundary.
package httpobservability

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
)

const (
	RequestIDHeader   = "X-Request-ID"
	maxRequestIDBytes = 64
	unknownMethod     = "UNKNOWN"
	unknownRoute      = "<unmatched>"
)

// Observer owns one service instance's HTTP metrics and middleware.
type Observer struct {
	logger          *slog.Logger
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	metricsHandler  http.Handler
}

// New creates an Observer backed by a private Prometheus registry.
func New(logger *slog.Logger) *Observer {
	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "speakup",
			Subsystem: "http_server",
			Name:      "requests_total",
			Help:      "Total HTTP requests handled by method, route, and status class.",
		},
		[]string{"method", "route", "status_class"},
	)
	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "speakup",
			Subsystem: "http_server",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds by method and route.",
		},
		[]string{"method", "route"},
	)
	registry := prometheus.NewRegistry()
	registry.MustRegister(requests, requestDuration)

	return &Observer{
		logger:          logger,
		requests:        requests,
		requestDuration: requestDuration,
		metricsHandler:  metricsMux(registry),
	}
}

// Middleware correlates, logs, and measures each request without retaining
// raw paths or other request content.
func (observer *Observer) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		requestID := acceptedRequestID(
			c.Request.Header.Values(RequestIDHeader),
		)
		c.Header(RequestIDHeader, requestID)
		c.Request = c.Request.WithContext(
			httpresponse.WithCorrelationID(c.Request.Context(), requestID),
		)

		c.Next()

		method := boundedMethod(c.Request.Method)
		route := c.FullPath()
		if route == "" {
			route = unknownRoute
		}
		status := c.Writer.Status()
		duration := time.Since(startedAt)

		observer.requests.WithLabelValues(
			method,
			route,
			statusClass(status),
		).Inc()
		observer.requestDuration.WithLabelValues(method, route).
			Observe(duration.Seconds())
		observer.logger.InfoContext(c.Request.Context(), "http request",
			slog.String("method", method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Duration("duration", duration),
			slog.String("request_id", requestID),
		)
	}
}

// MetricsHandler serves only the internal metrics endpoint.
func (observer *Observer) MetricsHandler() http.Handler {
	return observer.metricsHandler
}

func metricsMux(registry *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return mux
}

func acceptedRequestID(candidates []string) string {
	if len(candidates) == 1 && validRequestID(candidates[0]) {
		return candidates[0]
	}
	return "req_" + rand.Text()
}

func validRequestID(candidate string) bool {
	if len(candidate) == 0 || len(candidate) > maxRequestIDBytes {
		return false
	}
	for index := 0; index < len(candidate); index++ {
		character := candidate[index]
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

func boundedMethod(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace:
		return method
	default:
		return unknownMethod
	}
}

func statusClass(status int) string {
	switch status / 100 {
	case 1:
		return "1xx"
	case 2:
		return "2xx"
	case 3:
		return "3xx"
	case 4:
		return "4xx"
	case 5:
		return "5xx"
	default:
		return "unknown"
	}
}
