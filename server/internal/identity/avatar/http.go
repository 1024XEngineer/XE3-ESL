package avatar

import (
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const defaultReadTimeout = 30 * time.Second

type Handler struct {
	application Application
	errors      *httpresponse.Renderer
	readTimeout time.Duration
}

func NewHandler(application Application, renderer *httpresponse.Renderer) (*Handler, error) {
	if application == nil {
		return nil, ErrInvalidRequest
	}
	if renderer == nil {
		renderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: renderer, readTimeout: defaultReadTimeout}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/me/avatar", handler.upload)
	routes.GET("/v1/me/avatar/content", handler.content)
	routes.DELETE("/v1/me/avatar", handler.useDefault)
}

func (handler *Handler) upload(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	key, keyOK := httpinput.IdempotencyKey(c.Request)
	version, versionOK := expectedVersion(c.Request)
	contentType, typeOK := imageContentType(c.Request.Header.Get("Content-Type"))
	if !ok || !keyOK || !versionOK || !typeOK || c.Request.Body == nil {
		handler.write(c, invalidRequest(nil))
		return
	}
	if c.Request.ContentLength > MaxBytes {
		handler.write(c, imageTooLarge(nil))
		return
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(time.Now().Add(handler.readTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		handler.write(c, internalError(err))
		return
	}
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	profile, err := handler.application.Upload(
		c.Request.Context(), actor,
		UploadRequest{
			IdempotencyKey: key, ContentType: contentType,
			Body:                   http.MaxBytesReader(c.Writer, c.Request.Body, MaxBytes),
			ExpectedProfileVersion: version,
		},
	)
	if err != nil {
		handler.write(c, mapHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, profileResponse(profile))
}

func (handler *Handler) content(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	content, err := handler.application.Content(c.Request.Context(), actor)
	if err != nil {
		handler.write(c, mapHTTPError(err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"content_url": content.URL,
		"expires_at":  content.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func (handler *Handler) useDefault(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	version, versionOK := expectedVersion(c.Request)
	if !ok || !versionOK || c.Request.ContentLength > 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	profile, err := handler.application.UseDefault(c.Request.Context(), actor, version)
	if err != nil {
		handler.write(c, mapHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, profileResponse(profile))
}

func expectedVersion(request *http.Request) (int64, bool) {
	raw := strings.TrimSpace(request.Header.Get("If-Match"))
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, false
	}
	version, err := strconv.ParseInt(raw[1:len(raw)-1], 10, 64)
	return version, err == nil && version >= 1
}

func imageContentType(raw string) (string, bool) {
	contentType, parameters, err := mime.ParseMediaType(strings.TrimSpace(raw))
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

func profileResponse(profile identity.UserProfile) gin.H {
	response := gin.H{
		"user_id": profile.UserID, "display_name": profile.DisplayName,
		"profile_version": profile.ProfileVersion,
		"created_at":      profile.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":      profile.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if profile.Avatar != nil {
		response["avatar"] = gin.H{
			"width": profile.Avatar.Width, "height": profile.Avatar.Height,
			"updated_at": profile.Avatar.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return response
}

func mapHTTPError(err error) error {
	switch {
	case errors.Is(err, agentimage.ErrTooLarge):
		return imageTooLarge(err)
	case errors.Is(err, agentimage.ErrUnsupported):
		return apperror.New(apperror.InvalidArgument, "unsupported_image_format", "The image format is unsupported.", apperror.WithCause(err))
	case errors.Is(err, agentimage.ErrInvalid):
		return apperror.New(apperror.InvalidArgument, "invalid_image", "The image payload is invalid.", apperror.WithCause(err))
	case errors.Is(err, ErrIdempotencyConflict):
		return apperror.New(apperror.Conflict, "idempotency_key_conflict", "Idempotency key conflicts with the original request.", apperror.WithCause(err))
	case errors.Is(err, ErrConflict):
		return apperror.New(apperror.Conflict, "profile_version_conflict", "User profile changed before this update.", apperror.WithCause(err))
	case errors.Is(err, ErrUploadInProgress):
		return apperror.New(apperror.Conflict, "resource_processing", "Avatar upload is still in progress.", apperror.WithRetryable(true), apperror.WithCause(err))
	case errors.Is(err, ErrNotFound):
		return apperror.New(apperror.NotFound, "resource_not_found", "User avatar was not found.", apperror.WithCause(err))
	case errors.Is(err, ErrInvalidRequest):
		return invalidRequest(err)
	default:
		return internalError(err)
	}
}

func imageTooLarge(err error) error {
	return apperror.New(apperror.PayloadTooLarge, "image_too_large", "The image exceeds the allowed limits.", apperror.WithCause(err))
}

func invalidRequest(err error) error {
	return apperror.New(apperror.InvalidArgument, "invalid_request", "The request is invalid.", apperror.WithCause(err))
}

func authenticationRequired() error {
	return apperror.New(apperror.Unauthenticated, "authentication_required", "Authentication is required.")
}

func internalError(err error) error {
	return apperror.New(apperror.Internal, "internal_error", "The request could not be completed.", apperror.WithCause(err))
}

func (handler *Handler) write(c *gin.Context, err error) {
	handler.errors.Write(c, err)
}

var _ interface{ RegisterRoutes(gin.IRoutes) } = (*Handler)(nil)
