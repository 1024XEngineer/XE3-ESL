package app

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	"github.com/gin-gonic/gin"
)

// RegisterIELTSQuestionBank connects the IELTS content boundary to the
// application router without making the generic Scene Catalog own it.
func RegisterIELTSQuestionBank(
	router *gin.Engine,
	bank ielts.QuestionBankReader,
) {
	ielts.NewHTTPHandler(bank).RegisterRoutes(router)
}
