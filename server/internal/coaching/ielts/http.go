package ielts

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/gin-gonic/gin"
)

var ieltsErrorRenderer = httpresponse.NewRenderer(newCorrelationID)

// HTTPHandler exposes the IELTS Speaking question-bank surface independently
// from the generic Scene Catalog.
type HTTPHandler struct {
	bank QuestionBankReader
}

func NewHTTPHandler(bank QuestionBankReader) *HTTPHandler {
	if bank == nil {
		panic("IELTS question bank reader is required")
	}
	return &HTTPHandler{bank: bank}
}

func (handler *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET(
		"/v1/ielts-speaking/question-bank",
		handler.getQuestionBank,
	)
}

func (handler *HTTPHandler) getQuestionBank(c *gin.Context) {
	bank, err := handler.bank.QuestionBank(c.Request.Context())
	if err != nil {
		writeIELTSError(c, apperror.Internal, "internal_error", "Internal server error.", false)
		return
	}
	c.JSON(http.StatusOK, bank)
}

func writeIELTSError(
	c *gin.Context,
	category apperror.Category,
	code string,
	message string,
	retryable bool,
) {
	options := []apperror.Option{}
	if retryable {
		options = append(options, apperror.WithRetryable(true))
	}
	ieltsErrorRenderer.Write(c, apperror.New(category, code, message, options...))
}

func newCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_ielts_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}
