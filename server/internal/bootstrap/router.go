package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
	return newRouter(logger, nil, nil, modules...)
}

func NewRouterWithReadiness(
	logger *slog.Logger,
	readiness ReadinessChecker,
	modules ...Module,
) *gin.Engine {
	return newRouter(logger, readiness, nil, modules...)
}

// NewRouterWithReadinessAndRoutes mounts infrastructure route registrars
// without advertising them as business modules in the frozen /health contract.
func NewRouterWithReadinessAndRoutes(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	modules ...Module,
) *gin.Engine {
	return newRouter(logger, readiness, routes, modules...)
}

func newRouter(
	logger *slog.Logger,
	readiness ReadinessChecker,
	routes []RouteRegistrar,
	modules ...Module,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), requestLogger(logger))

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
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "route_not_found",
				"message": "route not found",
			},
		})
	})

	return router
}

func writeUnavailableReadiness(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status": "unavailable",
		"checks": gin.H{
			"database": "unavailable",
		},
	})
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		logger.InfoContext(c.Request.Context(), "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(startedAt)),
		)
	}
}
