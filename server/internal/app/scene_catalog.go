package app

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/gin-gonic/gin"
)

// RegisterSceneCatalog connects Scene's read-only delivery
// adapter to the application router without adding business logic here.
func RegisterSceneCatalog(
	router *gin.Engine,
	catalog scene.CatalogReader,
) {
	scene.NewCatalogHTTPHandler(catalog).RegisterRoutes(router)
}
