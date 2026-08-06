package ielts

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

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
	bank, err := handler.bank.QuestionBank()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":           "internal_error",
				"message":        "Internal server error.",
				"retryable":      false,
				"correlation_id": newCorrelationID(),
			},
		})
		return
	}
	c.JSON(http.StatusOK, bank)
}

func newCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_ielts_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}
