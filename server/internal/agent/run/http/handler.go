package runhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	application agentrun.Application
	errors      *httpresponse.Renderer
}

func NewHandler(
	application agentrun.Application,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil {
		return nil, errors.New("agent run: HTTP application is required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{application: application, errors: errorRenderer}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/agent-threads/:thread_id/runs", handler.submit)
	routes.POST("/v1/agent-threads/:thread_id/runs/stream", handler.submitStream)
	routes.GET("/v1/agent-runs/:run_id", handler.get)
	routes.POST("/v1/agent-runs/:run_id/retries", handler.retry)
	routes.POST("/v1/agent-runs/:run_id/retries/stream", handler.retryStream)
	routes.GET(
		"/v1/agent-runs/:run_id/context-manifest",
		handler.getContextManifest,
	)
}

func (handler *Handler) submit(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"client_message_id", "content", "image_asset_ids"},
		[]string{"client_message_id", "content"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	clientMessageID, clientIDOK := httpinput.String(values["client_message_id"])
	content, contentOK := httpinput.String(values["content"])
	if !clientIDOK || !contentOK {
		handler.write(c, invalidRequest(nil))
		return
	}
	var imageAssetIDs []string
	if raw, exists := values["image_asset_ids"]; exists {
		imageAssetIDs, ok = httpinput.StringArray(raw, agentrun.ValidUUID)
		if !ok || len(imageAssetIDs) > agentimage.MaxPerMessage {
			handler.write(c, invalidRequest(nil))
			return
		}
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	var submission agentrun.Submission
	var err error
	if len(imageAssetIDs) == 0 {
		submission, err = handler.application.SubmitText(
			c.Request.Context(), actor, c.Param("thread_id"),
			clientMessageID, content,
		)
	} else {
		submission, err = handler.application.SubmitWithImages(
			c.Request.Context(), actor, c.Param("thread_id"),
			clientMessageID, content, imageAssetIDs,
		)
	}
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(RunWriteStatus(submission.Run), RunResponse(submission.Run))
}

func (handler *Handler) submitStream(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"client_message_id", "content"},
		[]string{"client_message_id", "content"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	clientMessageID, clientIDOK := httpinput.String(values["client_message_id"])
	content, contentOK := httpinput.String(values["content"])
	if !clientIDOK || !contentOK {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	stream := &sseWriter{context: c}
	submission, err := handler.application.SubmitTextStream(
		c.Request.Context(), actor, c.Param("thread_id"),
		clientMessageID, content, stream,
	)
	handler.finishStream(c, stream, submission, err)
}

func (handler *Handler) retry(c *gin.Context) {
	retryClientID, ok := decodeRetry(c)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	retry, err := handler.application.RetryText(
		c.Request.Context(), actor, c.Param("run_id"), retryClientID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(RunWriteStatus(retry.Run), RunResponse(retry.Run))
}

func (handler *Handler) retryStream(c *gin.Context) {
	retryClientID, ok := decodeRetry(c)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	stream := &sseWriter{context: c}
	retry, err := handler.application.RetryTextStream(
		c.Request.Context(), actor, c.Param("run_id"), retryClientID, stream,
	)
	handler.finishStream(
		c, stream, agentrun.Submission{Run: retry.Run}, err,
	)
}

func decodeRetry(c *gin.Context) (string, bool) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"client_retry_id"},
		[]string{"client_retry_id"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		return "", false
	}
	return httpinput.String(values["client_retry_id"])
}

func (handler *Handler) get(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	run, err := handler.application.GetRun(
		c.Request.Context(), actor, c.Param("run_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, RunResponse(run))
}

func (handler *Handler) getContextManifest(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	manifest, err := handler.application.GetContextManifest(
		c.Request.Context(), actor, c.Param("run_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, ContextManifestResponse(manifest))
}

func (handler *Handler) finishStream(
	c *gin.Context,
	stream *sseWriter,
	submission agentrun.Submission,
	err error,
) {
	if err != nil {
		if !stream.started {
			handler.write(c, mapError(err))
			return
		}
		_ = stream.write("run.failed", gin.H{
			"run_id": submission.Run.ID, "kind": "stream_interrupted",
			"retryable": true,
		})
		return
	}
	switch submission.Run.Status {
	case agentrun.StatusCompleted:
		_ = stream.write("run.completed", gin.H{"run": RunResponse(submission.Run)})
	case agentrun.StatusFailed:
		_ = stream.write("run.failed", gin.H{
			"run": RunResponse(submission.Run), "kind": submission.Run.FailureKind,
			"retryable": submission.Run.FailureRetryable,
		})
	default:
		_ = stream.write("run.failed", gin.H{
			"run": RunResponse(submission.Run), "kind": "run_not_terminal",
			"retryable": true,
		})
	}
}

type sseWriter struct {
	context *gin.Context
	started bool
	runID   string
}

func (writer *sseWriter) OnInputCommitted(
	_ context.Context,
	submission agentrun.Submission,
) error {
	writer.runID = submission.Run.ID
	return writer.write("input.committed", gin.H{
		"run":     RunResponse(submission.Run),
		"message": agentconversationhttp.MessageResponse(submission.UserMessage),
	})
}

func (writer *sseWriter) OnAssistantStarted(
	_ context.Context,
	run agentrun.Run,
) error {
	writer.runID = run.ID
	return writer.write("assistant.started", gin.H{"run_id": run.ID})
}

func (writer *sseWriter) OnAssistantDelta(
	_ context.Context,
	delta string,
) error {
	return writer.write("assistant.delta", gin.H{
		"run_id": writer.runID, "delta": delta,
	})
}

func (writer *sseWriter) write(event string, data any) error {
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

func RunWriteStatus(run agentrun.Run) int {
	if run.Status == agentrun.StatusPending || run.Status == agentrun.StatusRunning {
		return http.StatusAccepted
	}
	return http.StatusCreated
}

func RunResponse(run agentrun.Run) gin.H {
	result := gin.H{
		"run_id": run.ID, "thread_id": run.ThreadID,
		"input_message_id": run.InputMessageID, "attempt": run.Attempt,
		"status": run.Status, "requested_provider": run.RequestedProvider,
		"requested_model":   run.RequestedModel,
		"max_output_tokens": run.MaxOutputTokens,
		"created_at":        run.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":        run.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if run.RetryOfRunID != "" {
		result["retry_of_run_id"] = run.RetryOfRunID
		result["client_retry_id"] = run.RetryClientID
	}
	if !run.StartedAt.IsZero() {
		result["started_at"] = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !run.CompletedAt.IsZero() {
		result["completed_at"] = run.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if run.Status == agentrun.StatusCompleted {
		result["assistant_message_id"] = run.AssistantMessageID
		result["provider_completion_id"] = run.ProviderCompletionID
		result["provider_model"] = run.ProviderModel
		result["finish_reason"] = run.FinishReason
		result["usage"] = gin.H{
			"input_tokens":  run.Usage.InputTokens,
			"output_tokens": run.Usage.OutputTokens,
			"total_tokens":  run.Usage.TotalTokens,
		}
	}
	if run.Status == agentrun.StatusFailed {
		result["failure"] = gin.H{
			"kind": run.FailureKind, "retryable": run.FailureRetryable,
		}
	}
	return result
}

func ContextManifestResponse(manifest agentcontext.Manifest) gin.H {
	messages := make([]gin.H, 0, len(manifest.SelectedMessages))
	for _, message := range manifest.SelectedMessages {
		messages = append(messages, gin.H{
			"message_id": message.MessageID, "sequence": message.Sequence,
			"role": message.Role,
		})
	}
	memories := make([]gin.H, 0, len(manifest.SelectedMemories))
	for _, item := range manifest.SelectedMemories {
		memory := gin.H{
			"memory_id": item.MemoryID, "memory_version": item.MemoryVersion,
			"type": item.Type, "scope": item.Scope, "similarity": item.Similarity,
			"score": item.Score, "embedding_provider": item.EmbeddingProvider,
			"embedding_model":          item.EmbeddingModel,
			"embedding_dimensions":     item.EmbeddingDimensions,
			"embedding_policy_version": item.EmbeddingPolicyVersion,
			"retrieval_policy_version": item.RetrievalPolicyVersion,
		}
		if item.MatterID != "" {
			memory["matter_id"] = item.MatterID
		}
		memories = append(memories, memory)
	}
	stableProfile := make([]gin.H, 0, len(manifest.SelectedStableProfile))
	for _, item := range manifest.SelectedStableProfile {
		stableProfile = append(stableProfile, gin.H{
			"memory_id": item.MemoryID, "memory_version": item.MemoryVersion,
			"canonical_key": item.CanonicalKey, "type": item.Type,
			"scope": item.Scope,
		})
	}
	result := gin.H{
		"run_id": manifest.RunID, "thread_id": manifest.ThreadID,
		"input_message_id":                      manifest.InputMessageID,
		"instruction_version":                   manifest.InstructionVersion,
		"stable_profile_context_policy_version": manifest.StableProfileContextPolicyVersion,
		"selected_stable_profile":               stableProfile,
		"memory_context_policy_version":         manifest.MemoryContextPolicyVersion,
		"selected_memories":                     memories,
		"summary_context_policy_version":        manifest.SummaryContextPolicyVersion,
		"summary_context_status":                manifest.SummaryContextStatus,
		"selected_messages":                     messages,
		"omitted_message_count":                 manifest.OmittedMessageCount,
		"trim_reason":                           manifest.TrimReason,
		"max_input_characters":                  manifest.MaxInputCharacters,
		"used_input_characters":                 manifest.UsedInputCharacters,
		"requested_provider":                    manifest.RequestedProvider,
		"requested_model":                       manifest.RequestedModel,
		"max_output_tokens":                     manifest.MaxOutputTokens,
		"created_at":                            manifest.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if manifest.SelectedSummary != nil {
		result["selected_summary"] = gin.H{
			"checkpoint_id":            manifest.SelectedSummary.CheckpointID,
			"source_from_sequence":     manifest.SelectedSummary.SourceFromSequence,
			"covered_through_sequence": manifest.SelectedSummary.CoveredThroughSequence,
			"policy_version":           manifest.SelectedSummary.PolicyVersion,
			"prompt_version":           manifest.SelectedSummary.PromptVersion,
			"provider":                 manifest.SelectedSummary.Provider,
			"model":                    manifest.SelectedSummary.Model,
		}
	}
	if manifest.ActiveMatterID != "" {
		result["active_matter"] = gin.H{
			"matter_id": manifest.ActiveMatterID,
			"version":   manifest.ActiveMatterVersion,
		}
	}
	return result
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
	case errors.Is(err, agentrun.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agentrun.ErrNotFound), errors.Is(err, agentcontext.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentrun.ErrIdempotencyConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentrun.ErrConflict), errors.Is(err, agentcontext.ErrConflict):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	default:
		return internalError(err)
	}
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
