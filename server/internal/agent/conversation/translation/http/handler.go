package translationhttp

import (
	"context"
	"errors"
	"net/http"

	agenttranslation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/translation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Translate(
		context.Context,
		requestcontext.Actor,
		string,
	) (agenttranslation.Result, error)
}

type Handler struct {
	application Application
	errors      *httpresponse.Renderer
}

func NewHandler(
	application Application,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil {
		return nil, errors.New("agent message translation: HTTP application is required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET(
		"/v1/agent-messages/:message_id/translation",
		handler.translation,
	)
}

func (handler *Handler) translation(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	result, err := handler.application.Translate(
		c.Request.Context(), actor, c.Param("message_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{
		"message_id":      result.MessageID,
		"target_language": result.TargetLanguage,
		"translation":     result.Content,
	})
}

func mapError(err error) error {
	var providerError *sharedtranslation.ProviderError
	switch {
	case errors.Is(err, agenttranslation.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agenttranslation.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agenttranslation.ErrInvalidContext):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	case errors.As(err, &providerError):
		code := "provider_unavailable"
		message := "The configured provider is temporarily unavailable."
		if providerError.Kind == sharedtranslation.ProviderErrorQuotaExhausted {
			code = "quota_exhausted"
			message = "The configured provider quota is exhausted."
		}
		return apperror.New(
			apperror.Unavailable, code, message,
			apperror.WithRetryable(providerError.Retryable()),
			apperror.WithCause(err),
		)
	default:
		return internalError(err)
	}
}

func (handler *Handler) write(c *gin.Context, err error) {
	if appError, ok := apperror.From(err); ok {
		if appError.Category() == apperror.Unauthenticated {
			c.Header("WWW-Authenticate", "Bearer")
		}
		if appError.Retryable() && appError.Category() == apperror.Unavailable {
			c.Header("Retry-After", "1")
		}
	}
	handler.errors.Write(c, err)
}

func invalidRequest(cause error) error {
	return apperror.New(
		apperror.InvalidArgument, "invalid_request", "Request validation failed.",
		apperror.WithCause(cause),
	)
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated, "authentication_required",
		"Authentication is required.",
	)
}

func internalError(cause error) error {
	return apperror.New(
		apperror.Internal, "internal_error", "An internal error occurred.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
}
