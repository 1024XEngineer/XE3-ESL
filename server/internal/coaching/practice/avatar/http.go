package avatar

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type HTTPHandler struct {
	application *Service
	errors      *httpresponse.Renderer
}

func NewHTTPHandler(application *Service) (*HTTPHandler, error) {
	if application == nil {
		return nil, apperror.New(
			apperror.Internal,
			"internal_error",
			"Internal server error.",
		)
	}
	return &HTTPHandler{
		application: application,
		errors:      httpresponse.NewRenderer(nil),
	}, nil
}

func (handler *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/avatar-session-token",
		handler.issueSessionToken,
	)
}

func (handler *HTTPHandler) issueSessionToken(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.ContentLength != 0 ||
		len(c.Request.TransferEncoding) != 0 {
		handler.errors.Write(c, apperror.New(
			apperror.InvalidArgument,
			"invalid_request",
			"Request validation failed.",
		))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		c.Header("WWW-Authenticate", "Bearer")
		handler.errors.Write(c, apperror.New(
			apperror.Unauthenticated,
			"authentication_required",
			"Authentication is required.",
		))
		return
	}
	result, err := handler.application.IssueSessionToken(
		c.Request.Context(),
		actor,
		c.Param("practice_session_id"),
	)
	if err != nil {
		handler.errors.Write(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
