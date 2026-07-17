package bootstrap

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
)

// Module is the narrow boundary used by the application composition root.
// Business modules can add their own dependencies without exposing internals.
type Module interface {
	Name() string
}

func NewRouter(logger *slog.Logger, modules ...Module) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), requestLogger(logger))

	moduleNames := make([]string, 0, len(modules))
	for _, module := range modules {
		moduleNames = append(moduleNames, module.Name())
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"modules": moduleNames,
		})
	})

	router.NoRoute(func(c *gin.Context) {
		err := apperror.NotFound(
			"route not found",
			apperror.WithReason("route_not_found"),
		)
		httpresponse.Write(c, err, requestIDFromContext(c))
	})

	return router
}

func requestIDFromContext(c *gin.Context) string {
	requestID, _ := c.Get("request_id")
	value, _ := requestID.(string)
	return value
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
