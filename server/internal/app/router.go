package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpobservability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
)

// Module is the narrow boundary used by the application composition root.
// Business modules can add their own dependencies without exposing internals.
type Module interface {
	Name() string
}

// RouteRegistrar is implemented by modules with production HTTP routes.
// Explicit Mock/Test composition roots may keep their own deterministic routes.
type RouteRegistrar interface {
	RegisterRoutes(*gin.Engine)
}

// ReadinessChecker reports whether an external dependency can currently
// accept work. pgxpool.Pool satisfies this interface.
type ReadinessChecker interface {
	Ping(context.Context) error
}

const readinessTimeout = 2 * time.Second

func NewRouter(logger *slog.Logger, modules ...Module) *gin.Engine {
	router, _ := newRouter(logger, nil, nil, modules...)
	return router
}

func NewRouterWithReadiness(
	logger *slog.Logger,
	readiness ReadinessChecker,
	modules ...Module,
) *gin.Engine {
	router, _ := newRouter(logger, readiness, nil, modules...)
	return router
}

// NewRouterWithReadinessAndRoutes mounts infrastructure route registrars
// without advertising them as business modules in the frozen /health contract.
func NewRouterWithReadinessAndRoutes(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	modules ...Module,
) *gin.Engine {
	router, _ := newRouter(logger, readiness, routes, modules...)
	return router
}

// NewObservableRouterWithReadinessAndRoutes returns the public application
// router and a separate handler for the internal metrics listener.
func NewObservableRouterWithReadinessAndRoutes(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	modules ...Module,
) (*gin.Engine, http.Handler) {
	return newRouter(logger, readiness, routes, modules...)
}

// NewObservableRouterWithRegistry mounts HTTP metrics in the same service
// registry already used by provider instrumentation.
func NewObservableRouterWithRegistry(
	logger *slog.Logger,
	readiness ReadinessChecker,
	registry *prometheus.Registry,
	routes []RouteRegistrar,
	modules ...Module,
) (*gin.Engine, http.Handler, error) {
	observer, err := httpobservability.NewWithRegistry(logger, registry)
	if err != nil {
		return nil, nil, err
	}
	router, metrics := buildRouter(
		logger, readiness, routes, observer, modules...,
	)
	return router, metrics, nil
}

func newRouter(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	modules ...Module,
) (*gin.Engine, http.Handler) {
	return buildRouter(
		logger,
		readiness,
		routes,
		httpobservability.New(logger),
		modules...,
	)
}

func buildRouter(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	observer *httpobservability.Observer,
	modules ...Module,
) (*gin.Engine, http.Handler) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(observer.Middleware(), gin.RecoveryWithWriter(io.Discard))
	errorRenderer := httpresponse.NewRenderer(nil)

	moduleNames := make([]string, 0, len(modules))
	for _, module := range modules {
		moduleNames = append(moduleNames, module.Name())
		if registrar, ok := module.(RouteRegistrar); ok {
			registrar.RegisterRoutes(router)
		}
	}
	for _, registrar := range routes {
		registrar.RegisterRoutes(router)
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"modules": moduleNames,
		})
	})

	router.GET("/readyz", func(c *gin.Context) {
		if readiness == nil {
			writeUnavailableReadiness(c)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()

		if err := readiness.Ping(ctx); err != nil {
			writeUnavailableReadiness(c)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ready",
			"checks": gin.H{
				"database": "ready",
			},
		})
	})

	router.NoRoute(func(c *gin.Context) {
		errorRenderer.Write(c, apperror.New(
			apperror.NotFound,
			"resource_not_found",
			"Resource was not found.",
		))
	})

	return router, observer.MetricsHandler()
}

func writeUnavailableReadiness(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status": "unavailable",
		"checks": gin.H{
			"database": "unavailable",
		},
	})
}
