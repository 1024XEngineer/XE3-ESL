package preparation

// This file keeps the shared transport primitives used by the still-migrating
// JobTarget and Plan handlers. It is deleted when those handlers move into
// transport/http; new Profile HTTP code must not depend on it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const maxLegacyPreparationHTTPRequestBody = 128 * 1024

func setProfilePrivateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func decodeProfileJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil ||
		c.Request.ContentLength > maxLegacyPreparationHTTPRequestBody ||
		!validProfileJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxLegacyPreparationHTTPRequestBody,
	)
	raw, err := io.ReadAll(body)
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func validProfileJSONContentType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	for name, value := range parameters {
		if !strings.EqualFold(name, "charset") ||
			!strings.EqualFold(value, "utf-8") {
			return false
		}
	}
	return true
}

func profileIdempotencyKey(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	key := values[0]
	return key, validIdempotencyKey(key) && utf8.ValidString(key)
}

func writeProfileAuthenticationRequired(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	writeProfileHTTPError(c, http.StatusUnauthorized, "authentication_required")
}

func writeProfileHTTPError(c *gin.Context, status int, code string) {
	messages := map[string]string{
		"invalid_request":               "Request validation failed.",
		"authentication_required":       "Authentication is required.",
		"preparation_profile_not_found": "Preparation profile was not found.",
		"preparation_version_conflict":  "Preparation profile version conflicts with this operation.",
		"idempotency_key_conflict":      "Idempotency key conflicts with the original request.",
		"internal_error":                "An internal error occurred.",
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        messages[code],
			"retryable":      false,
			"correlation_id": newPreparationCorrelationID(),
		},
	})
}
