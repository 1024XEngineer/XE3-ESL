package review

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type RetryRequestHTTPHandler struct {
	service *RetryRequestService
	errors  *httpresponse.Renderer
}

func NewRetryRequestHTTPHandler(
	service *RetryRequestService,
) (*RetryRequestHTTPHandler, error) {
	if service == nil {
		return nil, ErrRetryRequestInvalid
	}
	return &RetryRequestHTTPHandler{
		service: service,
		errors:  httpresponse.NewRenderer(nil),
	}, nil
}

func (handler *RetryRequestHTTPHandler) RegisterRoutes(
	routes gin.IRoutes,
) {
	routes.POST(
		"/v1/feedback-items/:feedback_item_id/retry-requests",
		handler.request,
	)
	routes.GET(
		"/v1/retry-requests/:retry_request_id",
		handler.get,
	)
}

func (handler *RetryRequestHTTPHandler) request(c *gin.Context) {
	setSpeechFeedbackPrivateHeaders(c)
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.writeInvalid(c)
		return
	}
	feedbackItemID := c.Param("feedback_item_id")
	if !validUUID(feedbackItemID) {
		handler.writeNotFound(c)
		return
	}
	idempotencyKey, ok := retryRequestIdempotencyKey(c)
	if !ok {
		handler.writeInvalid(c)
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeUnauthenticated(c)
		return
	}
	request, created, err := handler.service.Request(
		c.Request.Context(),
		actor,
		feedbackItemID,
		idempotencyKey,
	)
	if err != nil {
		handler.writeServiceError(c, err)
		return
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > maxSpeechFeedbackResponseBytes {
		handler.writeInternal(c, errors.Join(
			ErrRetryRequestInvalid,
			err,
		))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		c.Header("Location", request.StatusURL)
	}
	c.Data(status, "application/json; charset=utf-8", encoded)
}

func (handler *RetryRequestHTTPHandler) get(c *gin.Context) {
	setSpeechFeedbackPrivateHeaders(c)
	retryRequestID := c.Param("retry_request_id")
	if !validUUID(retryRequestID) {
		handler.writeNotFound(c)
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeUnauthenticated(c)
		return
	}
	request, err := handler.service.Get(
		c.Request.Context(),
		actor,
		retryRequestID,
	)
	if err != nil {
		handler.writeServiceError(c, err)
		return
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > maxSpeechFeedbackResponseBytes {
		handler.writeInternal(c, errors.Join(
			ErrRetryRequestInvalid,
			err,
		))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", encoded)
}

func (handler *RetryRequestHTTPHandler) writeServiceError(
	c *gin.Context,
	err error,
) {
	switch {
	case errors.Is(err, ErrRetryRequestInvalid):
		handler.writeInvalid(c)
	case errors.Is(err, ErrRetryRequestNotFound),
		errors.Is(err, ErrRetryRequestSourceUnavailable):
		handler.writeNotFound(c)
	case errors.Is(err, ErrRetryRequestConflict):
		handler.errors.Write(c, apperror.New(
			apperror.Conflict,
			"resource_conflict",
			"Resource state conflicts with this operation.",
		))
	default:
		handler.writeInternal(c, err)
	}
}

func (handler *RetryRequestHTTPHandler) writeInvalid(c *gin.Context) {
	handler.errors.Write(c, apperror.New(
		apperror.InvalidArgument,
		"invalid_request",
		"Request validation failed.",
	))
}

func (handler *RetryRequestHTTPHandler) writeUnauthenticated(
	c *gin.Context,
) {
	c.Header("WWW-Authenticate", "Bearer")
	handler.errors.Write(c, apperror.New(
		apperror.Unauthenticated,
		"invalid_credentials",
		"Authentication is required.",
	))
}

func (handler *RetryRequestHTTPHandler) writeNotFound(c *gin.Context) {
	handler.errors.Write(c, apperror.New(
		apperror.NotFound,
		"resource_not_found",
		"Resource was not found.",
	))
}

func (handler *RetryRequestHTTPHandler) writeInternal(
	c *gin.Context,
	cause error,
) {
	handler.errors.Write(c, apperror.New(
		apperror.Internal,
		"internal_error",
		"An internal error occurred.",
		apperror.WithCause(cause),
	))
}

func retryRequestIdempotencyKey(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	value := values[0]
	if value != strings.TrimSpace(value) ||
		len(value) < 8 || len(value) > 128 {
		return "", false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return "", false
		}
	}
	return value, true
}
