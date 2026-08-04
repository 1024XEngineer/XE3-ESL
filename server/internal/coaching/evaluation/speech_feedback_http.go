package evaluation

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxSpeechFeedbackResponseBytes = 512 * 1024

type SpeechFeedbackHTTPHandler struct {
	reader SpeechFeedbackReader
	errors *httpresponse.Renderer
}

func NewSpeechFeedbackHTTPHandler(
	reader SpeechFeedbackReader,
) (*SpeechFeedbackHTTPHandler, error) {
	if reader == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &SpeechFeedbackHTTPHandler{
		reader: reader,
		errors: httpresponse.NewRenderer(nil),
	}, nil
}

func (handler *SpeechFeedbackHTTPHandler) RegisterRoutes(
	routes gin.IRoutes,
) {
	routes.GET(
		"/v1/speech-feedback/:speech_feedback_id",
		handler.get,
	)
}

func (handler *SpeechFeedbackHTTPHandler) get(c *gin.Context) {
	setSpeechFeedbackPrivateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(
		c.Request.Context(),
	)
	if !ok {
		c.Header("WWW-Authenticate", "Bearer")
		handler.errors.Write(c, apperror.New(
			apperror.Unauthenticated,
			"invalid_credentials",
			"Authentication is required.",
		))
		return
	}
	speechFeedbackID := c.Param("speech_feedback_id")
	if !validUUID(speechFeedbackID) {
		handler.writeNotFound(c)
		return
	}
	feedback, err := handler.reader.Get(
		c.Request.Context(),
		actor,
		speechFeedbackID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrSpeechFeedbackNotFound),
			errors.Is(err, ErrSpeechFeedbackNotApplicable):
			handler.writeNotFound(c)
		default:
			handler.writeInternal(c, err)
		}
		return
	}
	encoded, err := json.Marshal(feedback)
	if err != nil || len(encoded) > maxSpeechFeedbackResponseBytes {
		handler.writeInternal(c, errors.Join(
			ErrInvalidSpeechFeedback,
			err,
		))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", encoded)
}

func (handler *SpeechFeedbackHTTPHandler) writeNotFound(
	c *gin.Context,
) {
	handler.errors.Write(c, apperror.New(
		apperror.NotFound,
		"resource_not_found",
		"Resource was not found.",
	))
}

func (handler *SpeechFeedbackHTTPHandler) writeInternal(
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

func setSpeechFeedbackPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}
