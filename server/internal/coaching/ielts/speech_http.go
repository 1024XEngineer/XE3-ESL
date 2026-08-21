package ielts

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type SpeechHTTPHandler struct{ service *SpeechService }

func NewSpeechHTTPHandler(service *SpeechService) (*SpeechHTTPHandler, error) {
	if service == nil {
		return nil, ErrSpeechInvalid
	}
	return &SpeechHTTPHandler{service: service}, nil
}

func (handler *SpeechHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/ielts-speaking/question-banks/:bank_id/:part/:source_id/questions/:question_position/speech", handler.question)
}

func (handler *SpeechHTTPHandler) question(c *gin.Context) {
	position, err := strconv.Atoi(c.Param("question_position"))
	if err != nil {
		writeSpeechError(c, ErrSpeechInvalid)
		return
	}
	handler.write(c, func(actor requestcontext.Actor) (platformmedia.ManagedAudioSource, error) {
		return handler.service.Question(c.Request.Context(), actor, QuestionReference{
			BankID: c.Param("bank_id"), Part: PracticeMode(c.Param("part")),
			SourceID: c.Param("source_id"), QuestionPosition: position,
		})
	})
}

func (handler *SpeechHTTPHandler) write(c *gin.Context, load func(requestcontext.Actor) (platformmedia.ManagedAudioSource, error)) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeIELTSError(c, apperror.Unauthenticated, "authentication_required", "Authentication is required.", false)
		return
	}
	audio, err := load(actor)
	if err != nil {
		writeSpeechError(c, err)
		return
	}
	defer func() { _ = audio.Close() }()
	reader, err := audio.Open()
	if err != nil {
		writeSpeechError(c, ErrSpeechUnavailable)
		return
	}
	defer reader.Close()
	c.DataFromReader(200, audio.Size(), audio.MediaType(), reader, nil)
}

func writeSpeechError(c *gin.Context, err error) {
	category, code, retryable := apperror.Internal, "internal_error", false
	switch {
	case errors.Is(err, ErrSpeechInvalid):
		category, code = apperror.InvalidArgument, "invalid_request"
	case errors.Is(err, ErrSpeechNotFound):
		category, code = apperror.NotFound, "resource_not_found"
	case errors.Is(err, ErrSpeechUnavailable):
		category, code, retryable = apperror.Unavailable, "provider_unavailable", true
	}
	writeIELTSError(c, category, code, "IELTS speech is unavailable.", retryable)
}
