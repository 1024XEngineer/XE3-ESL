package practice

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

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

const maxPracticeContextHTTPRequestBody = 128 * 1024

// ContextHTTPApplication is the transport-owned Practice boundary. Bearer
// authentication runs outside this handler and injects a trusted Actor.
type ContextHTTPApplication interface {
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		CreatePlanRequest,
	) (persistence.Plan, bool, error)
	GetPlan(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.Plan, error)
	UpdatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		UpdatePlanRequest,
	) (persistence.Plan, bool, error)
	CreateSession(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		CreateSessionRequest,
	) (persistence.ContextSessionBootstrap, bool, error)
	GetSession(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.ContextSession, error)
	GetSessionSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.ContextSessionSnapshot, error)
	TransitionSession(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		int,
		persistence.ContextSessionTransition,
	) (persistence.ContextSession, bool, error)
}

// ContextHTTPHandler exposes authenticated Plan and Session context routes.
type ContextHTTPHandler struct {
	application ContextHTTPApplication
}

func NewContextHTTPHandler(
	application ContextHTTPApplication,
) (*ContextHTTPHandler, error) {
	if application == nil {
		return nil, errors.New("practice: context HTTP application is required")
	}
	return &ContextHTTPHandler{application: application}, nil
}

func (h *ContextHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/practice-plans", h.createPlan)
	routes.GET("/v1/practice-plans/:practice_plan_id", h.getPlan)
	routes.PUT("/v1/practice-plans/:practice_plan_id", h.updatePlan)
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

func (h *ContextHTTPHandler) createPlan(c *gin.Context) {
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
	var request CreatePlanRequest
	if !decodePracticeContextJSONObject(c, &request) ||
		!validCreatePlanRequest(request) {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	plan, _, err := h.application.CreatePlan(
		c.Request.Context(),
		actor,
		idempotencyKey,
		request,
	)
	if err != nil {
		writePracticeContextServiceError(c, err, "practice_plan_not_found")
		return
	}
	c.JSON(http.StatusCreated, plan)
}

func (h *ContextHTTPHandler) getPlan(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, ok := practiceContextActor(c)
	if !ok {
		writePracticeContextAuthenticationRequired(c)
		return
	}
	planID := c.Param("practice_plan_id")
	if !validContextResourceID(planID) {
		writePracticeContextHTTPError(
			c,
			http.StatusNotFound,
			"practice_plan_not_found",
		)
		return
	}
	plan, err := h.application.GetPlan(
		c.Request.Context(),
		actor,
		planID,
	)
	if err != nil {
		writePracticeContextReadError(c, err, "practice_plan_not_found")
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *ContextHTTPHandler) updatePlan(c *gin.Context) {
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
	var request UpdatePlanRequest
	if !decodePracticeContextJSONObject(c, &request) ||
		!validUpdatePlanRequest(request) {
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
		return
	}
	plan, _, err := h.application.UpdatePlan(
		c.Request.Context(),
		actor,
		planID,
		idempotencyKey,
		request,
	)
	if err != nil {
		writePracticeContextServiceError(c, err, "practice_plan_not_found")
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *ContextHTTPHandler) createSession(c *gin.Context) {
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
	var request CreateSessionRequest
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
		writePracticeContextServiceError(c, err, "practice_plan_not_found")
		return
	}
	c.JSON(http.StatusCreated, bootstrap)
}

func (h *ContextHTTPHandler) getSession(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, sessionID, ok := practiceContextSessionRequest(c)
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

func (h *ContextHTTPHandler) getSessionSnapshot(c *gin.Context) {
	setPracticeContextPrivateResponseHeaders(c)
	actor, sessionID, ok := practiceContextSessionRequest(c)
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

func (h *ContextHTTPHandler) pauseSession(c *gin.Context) {
	h.transitionSession(c, persistence.ContextSessionPause)
}

func (h *ContextHTTPHandler) resumeSession(c *gin.Context) {
	h.transitionSession(c, persistence.ContextSessionResume)
}

func (h *ContextHTTPHandler) endSessionEarly(c *gin.Context) {
	h.transitionSession(c, persistence.ContextSessionEndEarly)
}

type practiceContextLifecycleRequest struct {
	ExpectedSessionVersion int `json:"expected_session_version"`
}

func (h *ContextHTTPHandler) transitionSession(
	c *gin.Context,
	transition persistence.ContextSessionTransition,
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

func practiceContextSessionRequest(
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
	case errors.Is(err, persistence.ErrInvalidArgument):
		writePracticeContextHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
		)
	case errors.Is(err, persistence.ErrNotFound):
		writePracticeContextHTTPError(
			c,
			http.StatusNotFound,
			notFoundCode,
		)
	case errors.Is(err, persistence.ErrIdempotencyConflict):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"idempotency_key_conflict",
		)
	case errors.Is(err, persistence.ErrConfirmationRequired):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"confirmation_required",
		)
	case errors.Is(err, persistence.ErrSessionCompleted):
		writePracticeContextHTTPError(
			c,
			http.StatusConflict,
			"practice_session_already_terminal",
		)
	case errors.Is(err, persistence.ErrConflict),
		errors.Is(err, persistence.ErrDeletionGeneration):
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
	if errors.Is(err, persistence.ErrInvalidArgument) {
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

func newPracticeContextCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_practice_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}

var _ ContextHTTPApplication = (*ContextApplication)(nil)
