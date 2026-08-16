package review

import (
	"errors"
	"net/http"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type RetryHTTPHandler struct {
	service RetryTurnCreator
	errors  *httpresponse.Renderer
}

func NewRetryHTTPHandler(service RetryTurnCreator) (*RetryHTTPHandler, error) {
	if service == nil {
		return nil, ErrInvalidRequest
	}
	return &RetryHTTPHandler{
		service: service,
		errors:  httpresponse.NewRenderer(nil),
	}, nil
}

func (handler *RetryHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/evaluation-feedback-items/:feedback_item_id/retry-turns",
		handler.create,
	)
}

func (handler *RetryHTTPHandler) create(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	if c.Request.ContentLength > 0 {
		handler.writeError(c, ErrInvalidRequest)
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok || !actor.Valid() {
		handler.errors.Write(c, apperror.New(
			apperror.Unauthenticated,
			"authentication_required",
			"Authentication is required.",
		))
		return
	}
	requestID, ok := singleHeader(c.Request.Header.Values("Idempotency-Key"))
	if !ok {
		handler.writeError(c, ErrInvalidRequest)
		return
	}
	turn, replayed, err := handler.service.CreateTurn(
		c.Request.Context(), actor.UserID, c.Param("feedback_item_id"), requestID,
	)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{
		"turn": gin.H{
			"turn_id":             turn.ID,
			"practice_session_id": turn.SessionID,
			"question_id":         turn.QuestionID,
			"original_turn_id":    turn.OriginalTurnID,
			"sequence":            turn.Sequence,
			"status":              turn.Status,
			"created_at":          turn.CreatedAt,
		},
		"replayed": replayed,
	})
}

func (handler *RetryHTTPHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, evaluation.ErrNotFound),
		errors.Is(err, practice.ErrNotFound):
		handler.errors.Write(c, apperror.New(
			apperror.NotFound,
			"resource_not_found",
			"Feedback item was not found.",
		))
	case errors.Is(err, ErrInvalidRequest),
		errors.Is(err, evaluation.ErrInvalidRequest),
		errors.Is(err, practice.ErrInvalidArgument):
		handler.errors.Write(c, apperror.New(
			apperror.InvalidArgument,
			"invalid_request",
			"The request is invalid.",
		))
	case errors.Is(err, ErrRetryUnavailable),
		errors.Is(err, practice.ErrConflict),
		errors.Is(err, practice.ErrIdempotencyConflict):
		handler.errors.Write(c, apperror.New(
			apperror.Conflict,
			"resource_conflict",
			"This feedback item cannot create a retry turn in the current state.",
		))
	case errors.Is(err, evaluation.ErrAccountUnavailable):
		handler.errors.Write(c, apperror.New(
			apperror.PermissionDenied,
			"account_unavailable",
			"The account is unavailable.",
		))
	default:
		handler.errors.Write(c, err)
	}
}

func singleHeader(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	return value, value != "" && len(value) <= 128
}
