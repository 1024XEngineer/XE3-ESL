package memehttp

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/meme"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Content(context.Context, requestcontext.Actor, string) (*os.File, meme.Attachment, error)
}

type Handler struct {
	application Application
	errors      *httpresponse.Renderer
}

func NewHandler(application Application, renderer *httpresponse.Renderer) (*Handler, error) {
	if application == nil {
		return nil, errors.New("agent meme: HTTP application is required")
	}
	if renderer == nil {
		renderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: renderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/agent-message-memes/:meme_attachment_id/content", handler.content)
}

func (handler *Handler) content(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.errors.Write(c, apperror.New(
			apperror.Unauthenticated, "authentication_required", "Authentication is required.",
		))
		return
	}
	file, attachment, err := handler.application.Content(
		c.Request.Context(), actor, c.Param("meme_attachment_id"),
	)
	if err != nil {
		handler.errors.Write(c, mapError(err))
		return
	}
	defer file.Close()
	c.Header("Cache-Control", "private, max-age=86400, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, attachment.SizeBytes, attachment.ContentType, file, nil)
}

func mapError(err error) error {
	switch {
	case errors.Is(err, meme.ErrInvalidRequest):
		return apperror.New(
			apperror.InvalidArgument, "invalid_request", "Request validation failed.",
			apperror.WithCause(err),
		)
	case errors.Is(err, meme.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	default:
		return apperror.New(
			apperror.Internal, "internal_error", "An internal error occurred.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	}
}
