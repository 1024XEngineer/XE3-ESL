package ielts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type AnswerGenerationHTTPHandler struct{ service *AnswerGenerationService }

func NewAnswerGenerationHTTPHandler(service *AnswerGenerationService) (*AnswerGenerationHTTPHandler, error) {
	if service == nil {
		return nil, ErrAnswerGenerationInvalid
	}
	return &AnswerGenerationHTTPHandler{service: service}, nil
}

func (handler *AnswerGenerationHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/ielts-speaking/answers:generate", handler.generate)
}

func (handler *AnswerGenerationHTTPHandler) generate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeIELTSError(c, apperror.Unauthenticated, "authentication_required", "Authentication is required.", false)
		return
	}
	var request AnswerGenerationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeIELTSError(c, apperror.InvalidArgument, "invalid_request", "The request is invalid.", false)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeIELTSError(c, apperror.InvalidArgument, "invalid_request", "The request is invalid.", false)
		return
	}
	answer, err := handler.service.Generate(c.Request.Context(), actor, request)
	if err != nil {
		switch {
		case errors.Is(err, ErrAnswerGenerationInvalid):
			writeIELTSError(c, apperror.InvalidArgument, "invalid_request", "The request is invalid.", false)
		case errors.Is(err, ErrAnswerGenerationNotFound):
			writeIELTSError(c, apperror.NotFound, "resource_not_found", "The requested IELTS question was not found.", false)
		default:
			writeIELTSError(c, apperror.Unavailable, "provider_unavailable", "IELTS answer generation is temporarily unavailable.", true)
		}
		return
	}
	c.JSON(http.StatusOK, answer)
}
