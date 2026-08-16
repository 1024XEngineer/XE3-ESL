package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const maxRequestBody = 128 * 1024

var preparationErrorRenderer = httpresponse.NewRenderer(
	newPreparationCorrelationID,
)

func actorFromContext(c *gin.Context) (requestcontext.Actor, bool) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		c.Header("WWW-Authenticate", "Bearer")
		writeHTTPError(c, http.StatusUnauthorized, "authentication_required", false)
	}
	return actor, ok
}

func clientRequestID(c *gin.Context) (string, bool) {
	value := c.GetHeader("Idempotency-Key")
	return value, preparation.ValidIdempotencyKey(value)
}

func setPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func decodeJSONObject(c *gin.Context, target any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" || c.Request.Body == nil || c.Request.ContentLength > maxRequestBody {
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBody))
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

func writeHTTPError(c *gin.Context, status int, code string, retryable bool) {
	category := apperror.Internal
	switch status {
	case http.StatusBadRequest:
		category = apperror.InvalidArgument
	case http.StatusUnauthorized:
		category = apperror.Unauthenticated
	case http.StatusNotFound:
		category = apperror.NotFound
	case http.StatusConflict:
		category = apperror.Conflict
	case http.StatusServiceUnavailable:
		category = apperror.Unavailable
	}
	message := map[string]string{
		"authentication_required":  "Authentication is required.",
		"invalid_request":          "The request is invalid.",
		"resource_not_found":       "The requested resource was not found.",
		"practice_plan_not_found":  "The practice plan was not found.",
		"idempotency_key_conflict": "The idempotency key was already used for a different request.",
		"resource_conflict":        "The resource changed or cannot complete this operation.",
		"provider_unavailable":     "The required provider is temporarily unavailable.",
		"internal_error":           "Internal server error.",
	}[code]
	if message == "" {
		message = "Internal server error."
	}
	options := []apperror.Option{}
	if retryable {
		options = append(options, apperror.WithRetryable(true))
	}
	preparationErrorRenderer.Write(c, apperror.New(category, code, message, options...))
}
