package audiohttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

// Application is the consumer-owned MessageAudio and speech boundary. The
// voice-input implementation can satisfy it without exposing candidate writes.
type Application interface {
	Playback(
		context.Context,
		requestcontext.Actor,
		string,
	) (objectstore.SignedGetResult, error)
	DeleteAudio(context.Context, requestcontext.Actor, string) error
	SynthesizeMessage(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (agentvoice.SynthesisResult, error)
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
		return nil, errors.New("agent conversation audio: HTTP application is required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET(
		"/v1/agent-message-audios/:audio_id/playback",
		handler.playback,
	)
	routes.DELETE(
		"/v1/agent-message-audios/:audio_id",
		handler.delete,
	)
	routes.GET(
		"/v1/agent-messages/:message_id/speech",
		handler.messageSpeech,
	)
	routes.POST(
		"/v1/agent-messages/:message_id/speech-previews",
		handler.messageSpeechPreview,
	)
}

func (handler *Handler) playback(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	result, err := handler.application.Playback(
		c.Request.Context(), actor, c.Param("audio_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"playback_url": result.URL,
		"expires_at":   result.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func (handler *Handler) delete(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if err := handler.application.DeleteAudio(
		c.Request.Context(), actor, c.Param("audio_id"),
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) messageSpeech(c *gin.Context) {
	handler.serveMessageSpeech(c, "")
}

func (handler *Handler) messageSpeechPreview(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"text"},
		[]string{"text"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	text, ok := httpinput.String(values["text"])
	if !ok || strings.TrimSpace(text) != text || text == "" {
		handler.write(c, invalidRequest(nil))
		return
	}
	handler.serveMessageSpeech(c, text)
}

func (handler *Handler) serveMessageSpeech(c *gin.Context, text string) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	speech, err := handler.application.SynthesizeMessage(
		c.Request.Context(), actor, c.Param("message_id"), text,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	if speech.Audio == nil {
		handler.write(c, internalError(nil))
		return
	}
	defer func() { _ = speech.Audio.Close() }()
	reader, err := speech.Audio.Open()
	if err != nil {
		handler.write(c, internalError(err))
		return
	}
	defer reader.Close()
	c.Header("Cache-Control", "no-store")
	c.DataFromReader(
		http.StatusOK,
		speech.Audio.Size(),
		speech.Audio.MediaType(),
		reader,
		nil,
	)
}

func mapError(err error) error {
	var speechError *agentvoice.SpeechError
	switch {
	case errors.Is(err, agentvoice.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agentvoice.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentvoice.ErrConflict),
		errors.Is(err, agentvoice.ErrCandidateStale):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentvoice.ErrCleanupPending),
		errors.Is(err, objectstore.ErrOperationFailed),
		errors.Is(err, objectstore.ErrDisabled),
		errors.Is(err, objectstore.ErrCredentials):
		return providerUnavailable(err)
	case errors.As(err, &speechError):
		code := "provider_unavailable"
		message := "The configured provider is temporarily unavailable."
		if speechError.Kind == agentvoice.ErrorQuotaExhausted {
			code = "quota_exhausted"
			message = "The configured provider quota is exhausted."
		}
		return apperror.New(
			apperror.Unavailable, code, message,
			apperror.WithRetryable(speechError.Kind.Retryable()),
			apperror.WithCause(err),
		)
	default:
		return internalError(err)
	}
}

func providerUnavailable(cause error) error {
	return apperror.New(
		apperror.Unavailable, "provider_unavailable",
		"The configured provider is temporarily unavailable.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
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
