package imagehttp

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const defaultReadTimeout = 30 * time.Second

type ThreadReader interface {
	GetThread(
		context.Context,
		requestcontext.Actor,
		string,
	) (agentconversation.Thread, error)
}

type Handler struct {
	application agentimage.Application
	threads     ThreadReader
	readTimeout time.Duration
	errors      *httpresponse.Renderer
}

func NewHandler(
	application agentimage.Application,
	threads ThreadReader,
	readTimeout time.Duration,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil || threads == nil || readTimeout < 0 {
		return nil, errors.New("agent image: HTTP dependencies are required")
	}
	if readTimeout == 0 {
		readTimeout = defaultReadTimeout
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{
		application: application,
		threads:     threads,
		readTimeout: readTimeout,
		errors:      errorRenderer,
	}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/agent-threads/:thread_id/image-assets",
		handler.upload,
	)
	routes.GET(
		"/v1/agent-image-assets/:image_asset_id/content",
		handler.content,
	)
	routes.DELETE(
		"/v1/agent-image-assets/:image_asset_id",
		handler.delete,
	)
}

func (handler *Handler) upload(c *gin.Context) {
	key, keyOK := httpinput.IdempotencyKey(c.Request)
	contentType, contentTypeOK := uploadContentType(c)
	if !keyOK || c.Request.Body == nil {
		handler.write(c, invalidRequest(nil))
		return
	}
	if !contentTypeOK {
		handler.write(c, apperror.New(
			apperror.InvalidArgument,
			"unsupported_image_format",
			"The image format is unsupported.",
		))
		return
	}
	if c.Request.ContentLength > agentimage.MaxBytes {
		handler.write(c, apperror.New(
			apperror.PayloadTooLarge,
			"image_too_large",
			"The image exceeds the allowed limits.",
		))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, err := handler.threads.GetThread(
		c.Request.Context(), actor, c.Param("thread_id"),
	)
	if err != nil {
		handler.write(c, mapThreadError(err))
		return
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(time.Now().Add(handler.readTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		handler.write(c, internalError(err))
		return
	}
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	body := http.MaxBytesReader(c.Writer, c.Request.Body, agentimage.MaxBytes)
	asset, err := handler.application.Upload(
		c.Request.Context(),
		actor,
		agentimage.UploadRequest{
			ThreadID: thread.ID, IdempotencyKey: key,
			ContentType: contentType, Body: body,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusCreated, AssetResponse(asset))
}

func (handler *Handler) content(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	content, err := handler.application.Content(
		c.Request.Context(), actor, c.Param("image_asset_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"content_url": content.URL,
		"expires_at":  content.ExpiresAt.UTC().Format(time.RFC3339Nano),
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
	if err := handler.application.Delete(
		c.Request.Context(), actor, c.Param("image_asset_id"),
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func uploadContentType(c *gin.Context) (string, bool) {
	contentType, parameters, err := mime.ParseMediaType(
		strings.TrimSpace(c.GetHeader("Content-Type")),
	)
	if err != nil || len(parameters) != 0 {
		return "", false
	}
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/png", "image/webp":
		return strings.ToLower(contentType), true
	default:
		return "", false
	}
}

func AssetResponse(asset agentimage.Asset) gin.H {
	response := gin.H{
		"image_asset_id": asset.ID,
		"thread_id":      asset.ThreadID,
		"content_type":   asset.ContentType,
		"size_bytes":     asset.Size,
		"width":          asset.Width,
		"height":         asset.Height,
		"status":         asset.Status,
		"created_at":     asset.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if !asset.AttachedAt.IsZero() {
		response["attached_at"] = asset.AttachedAt.UTC().Format(time.RFC3339Nano)
	}
	return response
}

func mapThreadError(err error) error {
	if errors.Is(err, agentconversation.ErrNotFound) {
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	}
	return internalError(err)
}

func mapError(err error) error {
	switch {
	case errors.Is(err, agentimage.ErrTooLarge):
		return apperror.New(
			apperror.PayloadTooLarge, "image_too_large",
			"The image exceeds the allowed limits.", apperror.WithCause(err),
		)
	case errors.Is(err, agentimage.ErrUnsupported):
		return apperror.New(
			apperror.InvalidArgument, "unsupported_image_format",
			"The image format is unsupported.", apperror.WithCause(err),
		)
	case errors.Is(err, agentimage.ErrInvalid):
		return apperror.New(
			apperror.InvalidArgument, "invalid_image",
			"The image payload is invalid.", apperror.WithCause(err),
		)
	case errors.Is(err, agentimage.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agentimage.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentimage.ErrIdempotencyConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentimage.ErrConflict):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	case errors.Is(err, objectstore.ErrOperationFailed),
		errors.Is(err, objectstore.ErrDisabled),
		errors.Is(err, objectstore.ErrCredentials):
		return apperror.New(
			apperror.Unavailable, "provider_unavailable",
			"The configured provider is temporarily unavailable.",
			apperror.WithRetryable(true), apperror.WithCause(err),
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
		if appError.Category() == apperror.Unavailable {
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
