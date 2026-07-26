package preparation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const maxProfileHTTPRequestBody = 128 * 1024

// ProfileHTTPApplication is the transport-owned boundary implemented by
// PersistenceService. The handler never reaches into a Repository directly.
type ProfileHTTPApplication interface {
	CreateProfile(
		context.Context,
		requestcontext.Actor,
		string,
		CreateProfileRequest,
	) (Profile, bool, error)
	CreateSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		CreateSnapshotRequest,
	) (Snapshot, bool, error)
}

// ProfileHTTPHandler exposes the authenticated Preparation write surface.
type ProfileHTTPHandler struct {
	application ProfileHTTPApplication
}

func NewProfileHTTPHandler(
	application ProfileHTTPApplication,
) (*ProfileHTTPHandler, error) {
	if application == nil {
		return nil, errors.New("preparation: profile HTTP application is required")
	}
	return &ProfileHTTPHandler{application: application}, nil
}

func (h *ProfileHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/preparation-profiles", h.createProfile)
	routes.POST(
		"/v1/preparation-profiles/:preparation_profile_id/snapshots",
		h.createSnapshot,
	)
}

func (h *ProfileHTTPHandler) createProfile(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeProfileAuthenticationRequired(c)
		return
	}
	idempotencyKey, ok := profileIdempotencyKey(c)
	if !ok {
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var request CreateProfileRequest
	if !decodeProfileJSONObject(c, &request) {
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	profile, _, err := h.application.CreateProfile(
		c.Request.Context(),
		actor,
		idempotencyKey,
		request,
	)
	if err != nil {
		writeProfileServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, profile)
}

func (h *ProfileHTTPHandler) createSnapshot(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeProfileAuthenticationRequired(c)
		return
	}
	idempotencyKey, ok := profileIdempotencyKey(c)
	if !ok {
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	profileID := c.Param("preparation_profile_id")
	if !utf8.ValidString(profileID) || !validResourceIdentifier(profileID) {
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var request CreateSnapshotRequest
	if !decodeProfileJSONObject(c, &request) {
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	snapshot, _, err := h.application.CreateSnapshot(
		c.Request.Context(),
		actor,
		profileID,
		idempotencyKey,
		request,
	)
	if err != nil {
		writeProfileServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, snapshot)
}

func setProfilePrivateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func decodeProfileJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil ||
		c.Request.ContentLength > maxProfileHTTPRequestBody ||
		!validProfileJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxProfileHTTPRequestBody,
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

func writeProfileServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrProfileInvalid):
		writeProfileHTTPError(c, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrProfileNotFound):
		writeProfileHTTPError(
			c,
			http.StatusNotFound,
			"preparation_profile_not_found",
		)
	case errors.Is(err, ErrProfileIdempotencyConflict):
		writeProfileHTTPError(
			c,
			http.StatusConflict,
			"idempotency_key_conflict",
		)
	case errors.Is(err, ErrProfileConflict),
		errors.Is(err, ErrProfileDeletionGeneration):
		writeProfileHTTPError(
			c,
			http.StatusConflict,
			"preparation_version_conflict",
		)
	default:
		writeProfileHTTPError(c, http.StatusInternalServerError, "internal_error")
	}
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
			"correlation_id": newCatalogCorrelationID(),
		},
	})
}

var _ ProfileHTTPApplication = (*PersistenceService)(nil)
