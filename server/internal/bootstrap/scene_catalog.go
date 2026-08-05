package bootstrap

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
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

// RegisterIELTSQuestionBank connects the dedicated IELTS Speaking content
// boundary without making the generic Scene Catalog own the question bank.
func RegisterIELTSQuestionBank(
	router *gin.Engine,
	bank ielts.QuestionBankReader,
) {
	ielts.NewHTTPHandler(bank).RegisterRoutes(router)
}
