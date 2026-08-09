package ielts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxAnswerPreparationBody = 32 * 1024

type AnswerPreparationHTTPHandler struct{ service *AnswerPreparationService }

func NewAnswerPreparationHTTPHandler(service *AnswerPreparationService) (*AnswerPreparationHTTPHandler, error) {
	if service == nil {
		return nil, ErrAnswerPreparationInvalid
	}
	return &AnswerPreparationHTTPHandler{service: service}, nil
}

func (handler *AnswerPreparationHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/ielts-speaking/answer-preparations", handler.create)
	routes.GET("/v1/ielts-speaking/answer-preparations/:answer_preparation_id", handler.get)
	routes.PATCH("/v1/ielts-speaking/answer-preparations/:answer_preparation_id", handler.update)
	routes.POST("/v1/ielts-speaking/answer-preparations/:answer_preparation_id/generations", handler.generate)
	routes.DELETE("/v1/ielts-speaking/answer-preparations/:answer_preparation_id", handler.delete)
}

func (handler *AnswerPreparationHTTPHandler) create(c *gin.Context) {
	actor, key, ok := answerWriteContext(c)
	if !ok {
		return
	}
	var request CreateAnswerPreparationRequest
	if !decodeAnswerJSON(c, &request) {
		writeAnswerError(c, ErrAnswerPreparationInvalid)
		return
	}
	value, replayed, err := handler.service.Create(c.Request.Context(), actor, key, request)
	if err != nil {
		writeAnswerError(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, value)
}

func (handler *AnswerPreparationHTTPHandler) get(c *gin.Context) {
	setAnswerPrivateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeAnswerAuthentication(c)
		return
	}
	value, err := handler.service.Get(c.Request.Context(), actor, c.Param("answer_preparation_id"))
	if err != nil {
		writeAnswerError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (handler *AnswerPreparationHTTPHandler) update(c *gin.Context) {
	actor, key, ok := answerWriteContext(c)
	if !ok {
		return
	}
	var request UpdateAnswerPreparationRequest
	if !decodeAnswerJSON(c, &request) {
		writeAnswerError(c, ErrAnswerPreparationInvalid)
		return
	}
	value, _, err := handler.service.Update(c.Request.Context(), actor, c.Param("answer_preparation_id"), key, request)
	if err != nil {
		writeAnswerError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (handler *AnswerPreparationHTTPHandler) generate(c *gin.Context) {
	actor, key, ok := answerWriteContext(c)
	if !ok {
		return
	}
	var request GenerateAnswerPreparationRequest
	if !decodeAnswerJSON(c, &request) {
		writeAnswerError(c, ErrAnswerPreparationInvalid)
		return
	}
	value, _, err := handler.service.Generate(c.Request.Context(), actor, c.Param("answer_preparation_id"), key, request)
	if err != nil {
		if errors.Is(err, ErrAnswerPreparationGeneration) {
			failed, getErr := handler.service.Get(c.Request.Context(), actor, c.Param("answer_preparation_id"))
			if getErr == nil {
				c.JSON(http.StatusBadGateway, failed)
				return
			}
		}
		writeAnswerError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (handler *AnswerPreparationHTTPHandler) delete(c *gin.Context) {
	actor, key, ok := answerWriteContext(c)
	if !ok {
		return
	}
	expectedVersion, err := strconv.Atoi(c.Query("expected_version"))
	if err != nil || expectedVersion < 1 {
		writeAnswerError(c, ErrAnswerPreparationInvalid)
		return
	}
	_, err = handler.service.Delete(c.Request.Context(), actor, c.Param("answer_preparation_id"), key, expectedVersion)
	if err != nil {
		writeAnswerError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func answerWriteContext(c *gin.Context) (requestcontext.Actor, string, bool) {
	setAnswerPrivateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeAnswerAuthentication(c)
		return requestcontext.Actor{}, "", false
	}
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 || !validIdempotencyKey(values[0]) {
		writeAnswerError(c, ErrAnswerPreparationInvalid)
		return requestcontext.Actor{}, "", false
	}
	return actor, values[0], true
}

func decodeAnswerJSON(c *gin.Context, target any) bool {
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	if charset, exists := params["charset"]; exists && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxAnswerPreparationBody)
	raw, err := io.ReadAll(body)
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func writeAnswerError(c *gin.Context, err error) {
	status, code, retryable := http.StatusInternalServerError, "internal_error", false
	switch {
	case errors.Is(err, ErrAnswerPreparationInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrAnswerPreparationNotFound):
		status, code = http.StatusNotFound, "answer_preparation_not_found"
	case errors.Is(err, ErrAnswerPreparationConflict):
		status, code = http.StatusConflict, "answer_preparation_version_conflict"
	case errors.Is(err, ErrAnswerPreparationIdempotencyConflict):
		status, code = http.StatusConflict, "idempotency_key_conflict"
	case errors.Is(err, ErrAnswerPreparationGeneration):
		status, code, retryable = http.StatusBadGateway, "answer_preparation_generation_failed", true
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": answerErrorMessage(code), "retryable": retryable, "correlation_id": newCorrelationID()}})
}

func answerErrorMessage(code string) string {
	switch code {
	case "invalid_request":
		return "Request is invalid."
	case "answer_preparation_not_found":
		return "Answer preparation was not found."
	case "answer_preparation_version_conflict":
		return "Answer preparation version conflicts with this operation."
	case "idempotency_key_conflict":
		return "Idempotency key conflicts with the original request."
	case "answer_preparation_generation_failed":
		return "Answer preparation generation failed and can be retried."
	default:
		return "Internal server error."
	}
}

func writeAnswerAuthentication(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "authentication_required", "message": "Authentication is required.", "retryable": false, "correlation_id": newCorrelationID()}})
}
func setAnswerPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}
