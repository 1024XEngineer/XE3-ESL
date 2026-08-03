package matterhttp

import (
	"errors"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	application matter.Application
	errors      *httpresponse.Renderer
}

func NewHandler(
	application matter.Application,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil {
		return nil, errors.New("matter: HTTP application is required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/matters", handler.create)
	routes.GET("/v1/matters", handler.list)
	routes.GET("/v1/matters/:matter_id", handler.get)
	routes.PATCH("/v1/matters/:matter_id", handler.changeStatus)
}

func (handler *Handler) create(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"title"},
		[]string{"title"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	title, ok := httpinput.String(values["title"])
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	item, err := handler.application.Create(c.Request.Context(), actor, title)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusCreated, Response(item))
}

func (handler *Handler) list(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	items, err := handler.application.List(c.Request.Context(), actor)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, Response(item))
	}
	c.JSON(http.StatusOK, gin.H{"matters": result})
}

func (handler *Handler) get(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	item, err := handler.application.ReadOwned(
		c.Request.Context(),
		actor,
		c.Param("matter_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, Response(item))
}

func (handler *Handler) changeStatus(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"status", "expected_version"},
		[]string{"status", "expected_version"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	status, statusOK := httpinput.String(values["status"])
	expectedVersion, versionOK := httpinput.Int64(values["expected_version"])
	if !statusOK || !versionOK {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	item, err := handler.application.ChangeStatus(
		c.Request.Context(),
		actor,
		c.Param("matter_id"),
		expectedVersion,
		matter.Status(status),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, Response(item))
}

func Response(item matter.Matter) gin.H {
	return gin.H{
		"matter_id":  item.ID,
		"title":      item.Title,
		"status":     item.Status,
		"version":    item.Version,
		"created_at": item.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (handler *Handler) write(c *gin.Context, err error) {
	if appError, ok := apperror.From(err); ok &&
		appError.Category() == apperror.Unauthenticated {
		c.Header("WWW-Authenticate", "Bearer")
	}
	handler.errors.Write(c, err)
}

func mapError(err error) error {
	switch {
	case errors.Is(err, matter.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, matter.ErrNotFound):
		return apperror.New(
			apperror.NotFound,
			"resource_not_found",
			"Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, matter.ErrConflict):
		return apperror.New(
			apperror.Conflict,
			"resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	default:
		return internalError(err)
	}
}

func invalidRequest(cause error) error {
	return apperror.New(
		apperror.InvalidArgument,
		"invalid_request",
		"Request validation failed.",
		apperror.WithCause(cause),
	)
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated,
		"authentication_required",
		"Authentication is required.",
	)
}

func internalError(cause error) error {
	return apperror.New(
		apperror.Internal,
		"internal_error",
		"An internal error occurred.",
		apperror.WithRetryable(true),
		apperror.WithCause(cause),
	)
}
