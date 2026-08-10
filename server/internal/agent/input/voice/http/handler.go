package voicehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentconversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	agentrunhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/http"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const defaultReadTimeout = 15 * time.Second

type Application interface {
	UploadStream(
		context.Context,
		requestcontext.Actor,
		agentvoice.UploadRequest,
		agentvoice.TranscriptionObserver,
	) (agentvoice.Candidate, error)
	GetCandidate(
		context.Context,
		requestcontext.Actor,
		string,
	) (agentvoice.Candidate, error)
	Retry(
		context.Context,
		requestcontext.Actor,
		string,
	) (agentvoice.Candidate, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		agentvoice.ConfirmCandidateCommand,
	) (agentvoice.Confirmation, error)
	DeleteCandidate(context.Context, requestcontext.Actor, string) error
}

type streamingConfirmationApplication interface {
	ConfirmStream(
		context.Context,
		requestcontext.Actor,
		agentvoice.ConfirmCandidateCommand,
		agentvoice.ConfirmationStreamObserver,
	) (agentvoice.Confirmation, error)
}

type ThreadReader interface {
	GetThread(
		context.Context,
		requestcontext.Actor,
		string,
	) (agentconversation.Thread, error)
}

type Handler struct {
	application Application
	threads     ThreadReader
	readTimeout time.Duration
	errors      *httpresponse.Renderer
}

func NewHandler(
	application Application,
	threads ThreadReader,
	readTimeout time.Duration,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil || threads == nil || readTimeout < 0 {
		return nil, errors.New("agent voice input: HTTP dependencies are required")
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
	routes.GET(
		"/v1/agent-threads/:thread_id/voice-message-candidates/realtime",
		handler.uploadRealtime,
	)
	routes.GET(
		"/v1/agent-voice-message-candidates/:candidate_id",
		handler.get,
	)
	routes.POST(
		"/v1/agent-voice-message-candidates/:candidate_id/retries",
		handler.retry,
	)
	routes.DELETE(
		"/v1/agent-voice-message-candidates/:candidate_id",
		handler.delete,
	)
	routes.POST(
		"/v1/agent-voice-message-candidates/:candidate_id/confirmations",
		handler.confirm,
	)
	routes.POST(
		"/v1/agent-voice-message-candidates/:candidate_id/confirmations/stream",
		handler.confirmStream,
	)
}

func (handler *Handler) get(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	candidate, err := handler.application.GetCandidate(
		c.Request.Context(), actor, c.Param("candidate_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, CandidateResponse(candidate))
}

func (handler *Handler) retry(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	candidate, err := handler.application.Retry(
		c.Request.Context(), actor, c.Param("candidate_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, CandidateResponse(candidate))
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
	if err := handler.application.DeleteCandidate(
		c.Request.Context(), actor, c.Param("candidate_id"),
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) confirm(c *gin.Context) {
	actor, command, ok := handler.prepareConfirmation(c)
	if !ok {
		return
	}
	confirmation, err := handler.application.Confirm(
		c.Request.Context(),
		actor,
		command,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	confirmation.Message.Audio = &confirmation.Audio
	c.JSON(
		agentrunhttp.RunWriteStatus(confirmation.Run),
		confirmationResponse(confirmation),
	)
}

func (handler *Handler) confirmStream(c *gin.Context) {
	application, ok := handler.application.(streamingConfirmationApplication)
	if !ok {
		handler.write(c, internalError(nil))
		return
	}
	actor, command, ok := handler.prepareConfirmation(c)
	if !ok {
		return
	}
	stream := &confirmationSSEWriter{context: c}
	confirmation, err := application.ConfirmStream(
		c.Request.Context(),
		actor,
		command,
		stream,
	)
	if err != nil {
		if !stream.started {
			handler.write(c, mapError(err))
			return
		}
		_ = stream.write("run.failed", gin.H{
			"run_id":    confirmation.Run.ID,
			"kind":      "stream_interrupted",
			"retryable": true,
		})
		return
	}
	switch confirmation.Run.Status {
	case agentrun.StatusCompleted:
		_ = stream.write("run.completed", gin.H{
			"run": agentrunhttp.RunResponse(confirmation.Run),
		})
	case agentrun.StatusFailed:
		_ = stream.write("run.failed", gin.H{
			"run":       agentrunhttp.RunResponse(confirmation.Run),
			"kind":      confirmation.Run.FailureKind,
			"retryable": confirmation.Run.FailureRetryable,
		})
	default:
		_ = stream.write("run.failed", gin.H{
			"run":       agentrunhttp.RunResponse(confirmation.Run),
			"kind":      "run_not_terminal",
			"retryable": true,
		})
	}
}

func (handler *Handler) prepareConfirmation(c *gin.Context) (
	requestcontext.Actor,
	agentvoice.ConfirmCandidateCommand,
	bool,
) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"candidate_version", "client_message_id", "confirmed_text"},
		[]string{"candidate_version", "client_message_id", "confirmed_text"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return requestcontext.Actor{}, agentvoice.ConfirmCandidateCommand{}, false
	}
	version, versionOK := httpinput.Int64(values["candidate_version"])
	clientMessageID, clientOK := httpinput.String(values["client_message_id"])
	confirmedText, textOK := httpinput.String(values["confirmed_text"])
	if !versionOK || !clientOK || !textOK {
		handler.write(c, invalidRequest(nil))
		return requestcontext.Actor{}, agentvoice.ConfirmCandidateCommand{}, false
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return requestcontext.Actor{}, agentvoice.ConfirmCandidateCommand{}, false
	}
	return actor, agentvoice.ConfirmCandidateCommand{
		CandidateID: c.Param("candidate_id"), CandidateVersion: version,
		ClientMessageID: clientMessageID, ConfirmedText: confirmedText,
	}, true
}

func confirmationResponse(confirmation agentvoice.Confirmation) gin.H {
	return gin.H{
		"candidate": CandidateResponse(confirmation.Candidate),
		"message":   agentconversationhttp.MessageResponse(confirmation.Message),
		"run":       agentrunhttp.RunResponse(confirmation.Run),
	}
}

type confirmationSSEWriter struct {
	context *gin.Context
	started bool
	runID   string
}

func (writer *confirmationSSEWriter) OnConfirmationCommitted(
	_ context.Context,
	confirmation agentvoice.Confirmation,
) error {
	writer.runID = confirmation.Run.ID
	confirmation.Message.Audio = &confirmation.Audio
	return writer.write("input.committed", confirmationResponse(confirmation))
}

func (writer *confirmationSSEWriter) OnAssistantStarted(
	_ context.Context,
	pending agentrun.Run,
) error {
	writer.runID = pending.ID
	return writer.write("assistant.started", gin.H{"run_id": pending.ID})
}

func (writer *confirmationSSEWriter) OnAssistantDelta(
	_ context.Context,
	delta string,
) error {
	return writer.write("assistant.delta", gin.H{
		"run_id": writer.runID,
		"delta":  delta,
	})
}

func (writer *confirmationSSEWriter) write(event string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if !writer.started {
		writer.context.Header("Content-Type", "text/event-stream; charset=utf-8")
		writer.context.Header("Cache-Control", "no-cache, no-store")
		writer.context.Header("X-Accel-Buffering", "no")
		writer.context.Header("Connection", "keep-alive")
		writer.context.Status(http.StatusOK)
		writer.started = true
	}
	if _, err := writer.context.Writer.WriteString(
		"event: " + event + "\ndata: " + string(encoded) + "\n\n",
	); err != nil {
		return err
	}
	writer.context.Writer.Flush()
	return writer.context.Request.Context().Err()
}

func CandidateResponse(candidate agentvoice.Candidate) gin.H {
	result := gin.H{
		"candidate_id": candidate.ID, "thread_id": candidate.ThreadID,
		"status": candidate.Status, "asr_attempt": candidate.ASRAttempt,
		"candidate_version": candidate.CandidateVersion,
		"recording": gin.H{
			"content_type": candidate.ContentType, "size_bytes": candidate.Size,
			"duration_ms": durationMilliseconds(candidate.Duration),
			"sample_rate": candidate.SampleRate,
		},
		"expires_at": candidate.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"created_at": candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": candidate.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if candidate.ASRCandidateText != "" {
		transcript := gin.H{
			"candidate_text": candidate.ASRCandidateText,
			"request_id":     candidate.ASRRequestID,
			"provider":       candidate.ASRProvider, "model": candidate.ASRModel,
		}
		if candidate.ASRLanguage != "" {
			transcript["language"] = candidate.ASRLanguage
		}
		if candidate.ASREmotion != "" {
			transcript["emotion"] = candidate.ASREmotion
		}
		if candidate.ASRFinishReason != "" {
			transcript["finish_reason"] = candidate.ASRFinishReason
		}
		result["transcript"] = transcript
	}
	if candidate.FailureKind != "" {
		result["failure"] = gin.H{
			"kind": candidate.FailureKind, "retryable": candidate.FailureRetryable,
		}
	}
	if candidate.ConfirmedMessageID != "" {
		result["confirmed_message_id"] = candidate.ConfirmedMessageID
		result["confirmed_run_id"] = candidate.ConfirmedRunID
		result["message_audio_id"] = candidate.MessageAudioID
		result["confirmed_at"] = candidate.ConfirmedAt.UTC().Format(time.RFC3339Nano)
	}
	if !candidate.DeletedAt.IsZero() {
		result["deleted_at"] = candidate.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
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
	var speechError *agentvoice.SpeechError
	switch {
	case errors.Is(err, agentvoice.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agentvoice.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentvoice.ErrIdempotencyConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentvoice.ErrCandidateProcessing):
		return apperror.New(
			apperror.Conflict, "resource_processing",
			"Resource processing is still in progress.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	case errors.Is(err, agentvoice.ErrCandidateStale),
		errors.Is(err, agentvoice.ErrConflict):
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
		return providerError(speechError.Kind, err)
	default:
		return internalError(err)
	}
}

func providerError(kind agentvoice.ErrorKind, cause error) error {
	code := "provider_unavailable"
	message := "The configured provider is temporarily unavailable."
	if kind == agentvoice.ErrorQuotaExhausted {
		code = "quota_exhausted"
		message = "The configured provider quota is exhausted."
	}
	return apperror.New(
		apperror.Unavailable, code, message,
		apperror.WithRetryable(kind.Retryable()), apperror.WithCause(cause),
	)
}

func providerUnavailable(cause error) error {
	return apperror.New(
		apperror.Unavailable, "provider_unavailable",
		"The configured provider is temporarily unavailable.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
}

func (handler *Handler) write(c *gin.Context, err error) {
	writeHTTPError(c, handler.errors, err)
}

func writeHTTPError(
	c *gin.Context,
	renderer *httpresponse.Renderer,
	err error,
) {
	if appError, ok := apperror.From(err); ok {
		if appError.Category() == apperror.Unauthenticated {
			c.Header("WWW-Authenticate", "Bearer")
		}
		if appError.Retryable() &&
			(appError.Category() == apperror.Unavailable ||
				appError.Code() == "resource_processing") {
			c.Header("Retry-After", "1")
		}
	}
	renderer.Write(c, err)
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
