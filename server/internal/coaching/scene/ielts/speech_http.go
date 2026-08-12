package ielts

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
	routes.GET("/v1/ielts-speaking/answer-preparations/:answer_preparation_id/speech", handler.answer)
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

func (handler *SpeechHTTPHandler) answer(c *gin.Context) {
	handler.write(c, func(actor requestcontext.Actor) (platformmedia.ManagedAudioSource, error) {
		return handler.service.Answer(c.Request.Context(), actor, c.Param("answer_preparation_id"))
	})
}

func (handler *SpeechHTTPHandler) write(c *gin.Context, load func(requestcontext.Actor) (platformmedia.ManagedAudioSource, error)) {
	setAnswerPrivateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeAnswerAuthentication(c)
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
	c.DataFromReader(http.StatusOK, audio.Size(), audio.MediaType(), reader, nil)
}

func writeSpeechError(c *gin.Context, err error) {
	status, code, retryable := http.StatusInternalServerError, "internal_error", false
	switch {
	case errors.Is(err, ErrSpeechInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, ErrSpeechNotFound), errors.Is(err, ErrAnswerPreparationNotFound):
		status, code = http.StatusNotFound, "speech_resource_not_found"
	case errors.Is(err, ErrSpeechUnavailable):
		status, code, retryable = http.StatusBadGateway, "speech_synthesis_unavailable", true
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": "IELTS speech is unavailable.", "retryable": retryable, "correlation_id": newCorrelationID()}})
}
