package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	basehttp "net/http"
	"strings"
	"unicode/utf8"

	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const maxRequestBody = 16 * 1024

type Application interface {
	Get(context.Context, requestcontext.Actor) (coachingprofile.Profile, error)
	Update(
		context.Context,
		requestcontext.Actor,
		coachingprofile.UpdateCommand,
	) (coachingprofile.Profile, error)
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
		return nil, coachingprofile.ErrRepository
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/me/coaching-profile", handler.get)
	routes.PATCH("/v1/me/coaching-profile", handler.patch)
}

func (handler *Handler) get(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, authenticationRequired())
		return
	}
	item, err := handler.application.Get(c.Request.Context(), actor)
	if err != nil {
		handler.writeError(c, mapError(err))
		return
	}
	writeProfile(c, item)
}

func (handler *Handler) patch(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, authenticationRequired())
		return
	}
	var request patchRequest
	if !decodeJSONObject(c, &request) {
		handler.writeError(c, invalidRequest())
		return
	}
	command, ok := request.command()
	if !ok {
		handler.writeError(c, invalidRequest())
		return
	}
	item, err := handler.application.Update(c.Request.Context(), actor, command)
	if err != nil {
		handler.writeError(c, mapError(err))
		return
	}
	writeProfile(c, item)
}

type patchRequest struct {
	ExpectedVersion *int64                  `json:"expected_version"`
	Updates         *profilePatch           `json:"updates,omitempty"`
	ForgetFields    []coachingprofile.Field `json:"forget_fields,omitempty"`
	ClearProfile    bool                    `json:"clear_profile,omitempty"`
	MemoryEnabled   *bool                   `json:"memory_enabled,omitempty"`
}

func (request patchRequest) command() (coachingprofile.UpdateCommand, bool) {
	if request.ExpectedVersion == nil {
		return coachingprofile.UpdateCommand{}, false
	}
	var patch coachingprofile.DataPatch
	if request.Updates != nil {
		patch = request.Updates.domain()
	}
	command := coachingprofile.UpdateCommand{
		ExpectedVersion: *request.ExpectedVersion,
		Patch:           patch,
		ForgetFields:    append([]coachingprofile.Field(nil), request.ForgetFields...),
		ClearProfile:    request.ClearProfile,
		MemoryEnabled:   request.MemoryEnabled,
		SourceType:      coachingprofile.SourceUserSetting,
	}
	return command, command.Valid()
}

type profilePatch struct {
	FormOfAddress       *string                         `json:"form_of_address,omitempty"`
	Occupation          *string                         `json:"occupation,omitempty"`
	ProfessionalContext *string                         `json:"professional_context,omitempty"`
	NativeLanguage      *string                         `json:"native_language,omitempty"`
	ExplanationLanguage *string                         `json:"explanation_language,omitempty"`
	ResponseDetail      *coachingprofile.ResponseDetail `json:"response_detail,omitempty"`
	Interests           *[]string                       `json:"interests,omitempty"`
}

func (patch profilePatch) domain() coachingprofile.DataPatch {
	return coachingprofile.DataPatch{
		FormOfAddress:       patch.FormOfAddress,
		Occupation:          patch.Occupation,
		ProfessionalContext: patch.ProfessionalContext,
		NativeLanguage:      patch.NativeLanguage,
		ExplanationLanguage: patch.ExplanationLanguage,
		ResponseDetail:      patch.ResponseDetail,
		Interests:           patch.Interests,
	}
}

func decodeJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil || c.Request.ContentLength > maxRequestBody ||
		!validJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := basehttp.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBody)
	raw, err := io.ReadAll(body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 || !utf8.Valid(raw) ||
		containsJSONNull(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func containsJSONNull(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(value any) bool {
		switch typed := value.(type) {
		case nil:
			return true
		case map[string]any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func validJSONContentType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	for name, value := range parameters {
		if !strings.EqualFold(name, "charset") ||
			!strings.EqualFold(value, "utf-8") {
			return false
		}
	}
	return true
}

func writeProfile(c *gin.Context, item coachingprofile.Profile) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	response := gin.H{
		"memory_enabled": item.MemoryEnabled,
		"profile":        item.Data,
		"field_sources":  item.FieldSources,
		"version":        item.Version,
	}
	if !item.CreatedAt.IsZero() {
		response["created_at"] = item.CreatedAt
		response["updated_at"] = item.UpdatedAt
	}
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
	case errors.Is(err, coachingprofile.ErrInvalidRequest):
		return invalidRequest()
	case errors.Is(err, coachingprofile.ErrVersionConflict):
		return apperror.New(
			apperror.Conflict,
			"profile_version_conflict",
			"Coaching profile version conflicts with this update.",
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

var _ Application = (*coachingprofile.Service)(nil)
