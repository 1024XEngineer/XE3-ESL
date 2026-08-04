package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxPracticeContextHTTPRequestBody = 128 * 1024

// Application is the transport-owned Practice boundary. Bearer
// authentication runs outside this handler and injects a trusted Actor.
type Application interface {
	CreateSession(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		practice.CreateSessionRequest,
	) (practice.SessionBootstrap, bool, error)
	GetSession(
		context.Context,
		requestcontext.Actor,
		string,
	) (practice.Session, error)
	GetSessionSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
	) (practice.SessionSnapshot, error)
	TransitionSession(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		int,
		practice.SessionTransition,
	) (practice.Session, bool, error)
}

// Handler exposes authenticated Practice Session routes.
type Handler struct {
	application Application
}

func NewHandler(application Application) (*Handler, error) {
	if application == nil {
		return nil, errors.New("practice: session HTTP application is required")
	}
	return &Handler{application: application}, nil
}

func (h *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/practice-plans/:practice_plan_id/practice-sessions",
		h.createSession,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id",
		h.getSession,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/pause",
		h.pauseSession,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/resume",
		h.resumeSession,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/end-early",
		h.endSessionEarly,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/snapshot",
		h.getSessionSnapshot,
	)
}

func (h *Handler) createSession(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, ok := practiceContextActor(c)
	if !ok {
		writePracticeContextAuthenticationRequired(c)
		return
	}
	idempotencyKey, ok := practiceContextIdempotencyKey(c)
	if !ok {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	planID := c.Param("practice_plan_id")
	if !validContextResourceID(planID) {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	var request practice.CreateSessionRequest
	if !decodePracticeContextJSONObject(c, &request) ||
		!validCreateSessionRequest(request) {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	bootstrap, _, err := h.application.CreateSession(
		c.Request.Context(),
		actor,
		planID,
		idempotencyKey,
		request,
	)
	if err != nil {
		if errors.Is(err, practice.ErrActiveSessionConflict) {
			writePracticeContextHTTPError(
				c,
				http.StatusConflict,
				"active_session_conflict",
			)
			return
		}
		if errors.Is(err, practice.ErrConflict) {
			writePracticeContextHTTPError(
				c,
				http.StatusConflict,
				"version_conflict",
			)
			return
		}
		writePracticeContextServiceError(c, err, "practice_plan_not_found")
		return
	}
	c.JSON(http.StatusCreated, bootstrap)
}

func (h *Handler) getSession(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, sessionID, ok := practiceSessionRequest(c)
	if !ok {
		return
	}
	session, err := h.application.GetSession(
		c.Request.Context(),
		actor,
		sessionID,
	)
	if err != nil {
		writePracticeContextReadError(
			c,
			err,
			"practice_session_not_found",
		)
		return
	}
	c.JSON(http.StatusOK, session)
}

func (h *Handler) getSessionSnapshot(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, sessionID, ok := practiceSessionRequest(c)
	if !ok {
		return
	}
	snapshot, err := h.application.GetSessionSnapshot(
		c.Request.Context(),
		actor,
		sessionID,
	)
	if err != nil {
		writePracticeContextReadError(
			c,
			err,
			"practice_session_not_found",
		)
		return
	}
	c.JSON(http.StatusOK, snapshot)
}

func (h *Handler) pauseSession(c *gin.Context) {
	h.transitionSession(c, practice.SessionPause)
}

func (h *Handler) resumeSession(c *gin.Context) {
	h.transitionSession(c, practice.SessionResume)
}

func (h *Handler) endSessionEarly(c *gin.Context) {
	h.transitionSession(c, practice.SessionEndEarly)
}

type practiceContextLifecycleRequest struct {
	ExpectedSessionVersion int `json:"expected_session_version"`
}

func (h *Handler) transitionSession(
	c *gin.Context,
	transition practice.SessionTransition,
) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, ok := practiceContextActor(c)
	if !ok {
		writePracticeContextAuthenticationRequired(c)
		return
	}
	idempotencyKey, ok := practiceContextIdempotencyKey(c)
	if !ok {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	sessionID := c.Param("practice_session_id")
	if !validContextResourceID(sessionID) {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	var request practiceContextLifecycleRequest
	if !decodePracticeContextJSONObject(c, &request) ||
		request.ExpectedSessionVersion < 1 {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	session, _, err := h.application.TransitionSession(
		c.Request.Context(),
		actor,
		sessionID,
		idempotencyKey,
		request.ExpectedSessionVersion,
		transition,
	)
	if err != nil {
		writePracticeContextServiceError(
			c,
			err,
			"practice_session_not_found",
		)
		return
	}
	c.JSON(http.StatusOK, session)
}

func practiceSessionRequest(
	c *gin.Context,
) (requestcontext.Actor, string, bool) {
	actor, ok := practiceContextActor(c)
	if !ok {
		writePracticeContextAuthenticationRequired(c)
		return requestcontext.Actor{}, "", false
	}
	sessionID := c.Param("practice_session_id")
	if !validContextResourceID(sessionID) {
		writePracticeContextHTTPError(
			c,
			http.StatusNotFound,
			"practice_session_not_found",
		)
		return requestcontext.Actor{}, "", false
	}
	return actor, sessionID, true
}

func practiceContextActor(
	c *gin.Context,
) (requestcontext.Actor, bool) {
	return requestcontext.ActorFromContext(c.Request.Context())
}

func setPracticeContextPrivateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func decodePracticeContextJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil ||
		c.Request.ContentLength > maxPracticeContextHTTPRequestBody ||
		!validPracticeContextJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxPracticeContextHTTPRequestBody,
	)
	raw, err := io.ReadAll(body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || !utf8.Valid(raw) || len(trimmed) == 0 ||
		trimmed[0] != '{' {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func validPracticeContextJSONContentType(value string) bool {
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

func practiceContextIdempotencyKey(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	key := values[0]
	return key, utf8.ValidString(key) && validContextIdempotencyKey(key)
}

func writePracticeContextAuthenticationRequired(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	writePracticeContextHTTPError(
		c,
		http.StatusUnauthorized,
		"authentication_required",
	)
}

func writePracticeContextServiceError(
	c *gin.Context,
	err error,
	notFoundCode string,
) {
	switch {
	case errors.Is(err, practice.ErrInvalidArgument):
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
	case errors.Is(err, practice.ErrNotFound):
		writePracticeContextHTTPError(
			c,
			http.StatusNotFound,
			notFoundCode,
		)
	case errors.Is(err, practice.ErrIdempotencyConflict):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"idempotency_key_conflict",
		)
	case errors.Is(err, practice.ErrConfirmationRequired):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"confirmation_required",
		)
	case errors.Is(err, practice.ErrSessionCompleted):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"practice_session_already_terminal",
		)
	case errors.Is(err, practice.ErrConflict),
		errors.Is(err, practice.ErrDeletionGeneration):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"resource_conflict",
		)
	default:
		writePracticeContextHTTPError(
			c,
			http.StatusInternalServerError,
			"internal_error",
		)
	}
}

func writePracticeContextReadError(
	c *gin.Context,
	err error,
	notFoundCode string,
) {
	if errors.Is(err, practice.ErrInvalidArgument) {
		writePracticeContextHTTPError(
			c,
			http.StatusNotFound,
			notFoundCode,
		)
		return
	}
	writePracticeContextServiceError(c, err, notFoundCode)
}

func writePracticeContextHTTPError(
	c *gin.Context,
	status int,
	code string,
) {
	messages := map[string]string{
		"invalid_request":                   "Request validation failed.",
		"authentication_required":           "Authentication is required.",
		"practice_plan_not_found":           "Practice plan was not found.",
		"practice_session_not_found":        "Practice session was not found.",
		"practice_session_already_terminal": "Practice session is already terminal.",
		"idempotency_key_conflict":          "Idempotency key conflicts with the original request.",
		"confirmation_required":             "Explicit user confirmation is required.",
		"version_conflict":                  "The Practice Plan revision has changed.",
		"active_session_conflict":           "An active Practice Session already exists.",
		"resource_conflict":                 "Resource state conflicts with this operation.",
		"internal_error":                    "An internal error occurred.",
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        messages[code],
			"retryable":      false,
			"correlation_id": newPracticeContextCorrelationID(),
		},
	})
}

func validCreateSessionRequest(request practice.CreateSessionRequest) bool {
	return request.ExpectedPlanRevision > 0
}

func validContextResourceID(value string) bool {
	return utf8.ValidString(value) && value != "" && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validContextIdempotencyKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func newPracticeContextCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_practice_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}

var _ Application = (*practice.SessionApplication)(nil)
