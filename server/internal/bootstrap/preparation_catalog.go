package bootstrap

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/gin-gonic/gin"
)

// RegisterPreparationCatalog connects Preparation's read-only delivery
// adapter to the application router without adding business logic here.
func RegisterPreparationCatalog(
	router *gin.Engine,
	catalog preparation.CatalogReader,
) {
	preparation.NewCatalogHTTPHandler(catalog).RegisterRoutes(router)
}
