package http

import (
	"bytes"
	"context"
	"errors"
	basehttp "net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const maxRequestBody = int64(8 * 1024)

type Application interface {
	GetCatalog(context.Context, requestcontext.Actor) (presentation.Catalog, error)
	GetPreference(context.Context, requestcontext.Actor) (presentation.Preference, error)
	UpdatePreference(
		context.Context,
		requestcontext.Actor,
		presentation.UpdateCommand,
	) (presentation.Preference, error)
}

type Handler struct {
	application Application
	errors      *httpresponse.Renderer
}

func New(
	application Application,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil {
		return nil, presentation.ErrRepository
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/coach-presentation-catalog", handler.getCatalog)
	routes.GET("/v1/me/coach-presentation", handler.getPreference)
	routes.PATCH("/v1/me/coach-presentation", handler.patchPreference)
}

func (handler *Handler) getCatalog(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, authenticationRequired())
		return
	}
	catalog, err := handler.application.GetCatalog(c.Request.Context(), actor)
	if err != nil {
		handler.writeError(c, mapError(err))
		return
	}
	writePrivateJSON(c, gin.H{
		"avatars": catalog.Avatars,
		"voices":  catalog.Voices,
		"defaults": gin.H{
			"avatar_option_id": catalog.DefaultAvatarOptionID,
			"voice_option_id":  catalog.DefaultVoiceOptionID,
		},
	})
}

func (handler *Handler) getPreference(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, authenticationRequired())
		return
	}
	preference, err := handler.application.GetPreference(c.Request.Context(), actor)
	if err != nil {
		handler.writeError(c, mapError(err))
		return
	}
	writePreference(c, preference)
}

func (handler *Handler) patchPreference(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, authenticationRequired())
		return
	}
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"avatar_option_id", "voice_option_id", "expected_version"},
		[]string{"avatar_option_id", "voice_option_id", "expected_version"},
		maxRequestBody,
		5*time.Second,
	)
	if !ok {
		handler.writeError(c, invalidRequest())
		return
	}
	avatarOptionID, avatarOK := httpinput.String(values["avatar_option_id"])
	voiceOptionID, voiceOK := httpinput.String(values["voice_option_id"])
	expectedVersion, versionOK := httpinput.Int64(values["expected_version"])
	versionOK = versionOK && !bytes.Equal(
		bytes.TrimSpace(values["expected_version"]),
		[]byte("null"),
	)
	command := presentation.UpdateCommand{
		AvatarOptionID:  avatarOptionID,
		VoiceOptionID:   voiceOptionID,
		ExpectedVersion: expectedVersion,
	}
	if !avatarOK || !voiceOK || !versionOK || !command.Valid() {
		handler.writeError(c, invalidRequest())
		return
	}
	preference, err := handler.application.UpdatePreference(
		c.Request.Context(), actor, command,
	)
	if err != nil {
		handler.writeError(c, mapError(err))
		return
	}
	writePreference(c, preference)
}

func writePreference(c *gin.Context, preference presentation.Preference) {
	response := gin.H{
		"avatar_option_id": preference.AvatarOptionID,
		"voice_option_id":  preference.VoiceOptionID,
		"version":          preference.Version,
	}
	if !preference.CreatedAt.IsZero() {
		response["created_at"] = preference.CreatedAt
		response["updated_at"] = preference.UpdatedAt
	}
	writePrivateJSON(c, response)
}

func writePrivateJSON(c *gin.Context, response any) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(basehttp.StatusOK, response)
}

func (handler *Handler) writeError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	if appError, ok := apperror.From(err); ok &&
		appError.Category() == apperror.Unauthenticated {
		c.Header("WWW-Authenticate", "Bearer")
	}
	handler.errors.Write(c, err)
}

func mapError(err error) error {
	switch {
	case errors.Is(err, presentation.ErrInvalidRequest):
		return invalidRequest()
	case errors.Is(err, presentation.ErrVersionConflict):
		return apperror.New(
			apperror.Conflict,
			"coach_presentation_version_conflict",
			"Coach presentation version conflicts with this update.",
		)
	default:
		return apperror.New(
			apperror.Internal,
			"internal_error",
			"Internal server error.",
		)
	}
}

func invalidRequest() error {
	return apperror.New(
		apperror.InvalidArgument,
		"invalid_request",
		"Request validation failed.",
	)
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated,
		"authentication_required",
		"Authentication is required.",
	)
}

var _ Application = (*presentation.Service)(nil)
