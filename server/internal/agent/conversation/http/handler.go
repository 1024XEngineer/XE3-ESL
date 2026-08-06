package conversationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	defaultThreadPageSize  = 20
	defaultMessagePageSize = 50
)

type ToolCallReader interface {
	GetToolCalls(
		context.Context,
		requestcontext.Actor,
		string,
	) ([]agentrun.ToolCall, error)
}

type MessageImageReader interface {
	MessageAssets(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) ([]agentimage.Asset, error)
}

type SpeechFeedbackProjectionReader interface {
	StatusURLForAgentVoiceMessage(
		context.Context,
		requestcontext.Actor,
		string,
	) (string, bool, error)
}

type Handler struct {
	application     agentconversation.Application
	toolCalls       ToolCallReader
	images          MessageImageReader
	speechFeedback  SpeechFeedbackProjectionReader
	assistantSpeech agentconversation.AssistantSpeechSynthesizer
	errors          *httpresponse.Renderer
}

func WithAssistantSpeech(
	synthesizer agentconversation.AssistantSpeechSynthesizer,
) Option {
	return func(handler *Handler) error {
		if synthesizer == nil {
			return errors.New("agent conversation: assistant speech synthesizer is required")
		}
		handler.assistantSpeech = synthesizer
		return nil
	}
}

type Option func(*Handler) error

func WithToolCalls(reader ToolCallReader) Option {
	return func(handler *Handler) error {
		if reader == nil {
			return errors.New("agent conversation: tool-call reader is required")
		}
		handler.toolCalls = reader
		return nil
	}
}

func WithMessageImages(reader MessageImageReader) Option {
	return func(handler *Handler) error {
		if reader == nil {
			return errors.New("agent conversation: image reader is required")
		}
		handler.images = reader
		return nil
	}
}

func WithSpeechFeedback(reader SpeechFeedbackProjectionReader) Option {
	return func(handler *Handler) error {
		if reader == nil {
			return errors.New("agent conversation: speech feedback reader is required")
		}
		handler.speechFeedback = reader
		return nil
	}
}

func NewHandler(
	application agentconversation.Application,
	errorRenderer *httpresponse.Renderer,
	options ...Option,
) (*Handler, error) {
	if application == nil {
		return nil, errors.New("agent conversation: HTTP application is required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	handler := &Handler{application: application, errors: errorRenderer}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("agent conversation: HTTP option is invalid")
		}
		if err := option(handler); err != nil {
			return nil, err
		}
	}
	return handler, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/agent-threads", handler.createThread)
	routes.GET("/v1/agent-threads", handler.listThreads)
	routes.GET("/v1/agent-threads/focused", handler.getFocusedThread)
	routes.PUT("/v1/agent-threads/focused", handler.setFocusedThread)
	routes.DELETE("/v1/agent-threads/focused", handler.clearFocusedThread)
	routes.GET("/v1/agent-threads/:thread_id", handler.getThread)
	routes.DELETE("/v1/agent-threads/:thread_id", handler.deleteThread)
	routes.PUT(
		"/v1/agent-threads/:thread_id/active-goal",
		handler.setActiveGoal,
	)
	routes.GET(
		"/v1/agent-threads/:thread_id/messages",
		handler.listMessages,
	)
	if handler.assistantSpeech != nil {
		routes.GET(
			"/v1/agent-threads/:thread_id/assistant-speech/realtime",
			handler.streamAssistantSpeech,
		)
	}
}

func (handler *Handler) createThread(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"active_goal_id"},
		nil,
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	activeGoalID := ""
	if raw, exists := values["active_goal_id"]; exists {
		activeGoalID, ok = httpinput.String(raw)
		if !ok || activeGoalID == "" {
			handler.write(c, invalidRequest(nil))
			return
		}
	}
	actor, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, err := handler.application.CreateThread(
		c.Request.Context(), actor, activeGoalID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusCreated, ThreadResponse(thread))
}

func (handler *Handler) listThreads(c *gin.Context) {
	pageSize, cursor, ok := decodePageQuery(
		c.Request.URL.RawQuery,
		defaultThreadPageSize,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	page, err := handler.application.PageThreads(
		c.Request.Context(), trusted, pageSize, cursor,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	threads := make([]gin.H, 0, len(page.Threads))
	for _, thread := range page.Threads {
		threads = append(threads, ThreadResponse(thread))
	}
	result := gin.H{"threads": threads}
	if page.FocusedThreadID != "" {
		result["focused_thread_id"] = page.FocusedThreadID
	}
	if page.NextCursor != "" {
		result["next_cursor"] = page.NextCursor
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) getFocusedThread(c *gin.Context) {
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, found, err := handler.application.GetFocusedThread(
		c.Request.Context(), trusted,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	if !found {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, ThreadResponse(thread))
}

func (handler *Handler) setFocusedThread(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"thread_id"},
		[]string{"thread_id"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	threadID, ok := httpinput.String(values["thread_id"])
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, err := handler.application.SetFocusedThread(
		c.Request.Context(), trusted, threadID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, ThreadResponse(thread))
}

func (handler *Handler) clearFocusedThread(c *gin.Context) {
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if err := handler.application.ClearFocusedThread(
		c.Request.Context(), trusted,
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) getThread(c *gin.Context) {
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	thread, err := handler.application.GetThread(
		c.Request.Context(), trusted, c.Param("thread_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, ThreadResponse(thread))
}

func (handler *Handler) deleteThread(c *gin.Context) {
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	if err := handler.application.DeleteThread(
		c.Request.Context(), trusted, c.Param("thread_id"),
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) setActiveGoal(c *gin.Context) {
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"goal_id"},
		[]string{"goal_id"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	goalID, ok := httpinput.String(values["goal_id"])
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	link, err := handler.application.SetActiveGoal(
		c.Request.Context(), trusted, c.Param("thread_id"), goalID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, LinkResponse(link))
}

func (handler *Handler) listMessages(c *gin.Context) {
	pageSize, cursor, ok := decodePageQuery(
		c.Request.URL.RawQuery,
		defaultMessagePageSize,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	trusted, ok := actor(c)
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	page, err := handler.application.PageMessages(
		c.Request.Context(), trusted, c.Param("thread_id"), pageSize, cursor,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	messages := make([]gin.H, 0, len(page.Messages))
	for _, message := range page.Messages {
		response, err := handler.messageResponseWithHandoffs(
			c.Request.Context(), trusted, message,
		)
		if err != nil {
			handler.write(c, mapError(err))
			return
		}
		messages = append(messages, response)
	}
	result := gin.H{"messages": messages}
	if page.NextCursor != "" {
		result["next_cursor"] = page.NextCursor
	}
	c.JSON(http.StatusOK, result)
}

func decodePageQuery(rawQuery string, defaultPageSize int) (int, string, bool) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return 0, "", false
	}
	for key := range values {
		if key != "page_size" && key != "cursor" {
			return 0, "", false
		}
	}
	pageSize := defaultPageSize
	if rawValues, exists := values["page_size"]; exists {
		if len(rawValues) != 1 || !httpinput.DecimalDigits(rawValues[0]) {
			return 0, "", false
		}
		pageSize, err = strconv.Atoi(rawValues[0])
		if err != nil || pageSize < 1 || pageSize > agentconversation.MaxPageSize {
			return 0, "", false
		}
	}
	cursor := ""
	if rawValues, exists := values["cursor"]; exists {
		if len(rawValues) != 1 || rawValues[0] == "" {
			return 0, "", false
		}
		cursor = rawValues[0]
	}
	return pageSize, cursor, true
}

func ThreadResponse(thread agentconversation.Thread) gin.H {
	result := gin.H{
		"thread_id":  thread.ID,
		"title":      nil,
		"created_at": thread.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": thread.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if thread.Title != "" {
		result["title"] = thread.Title
	}
	if thread.ActiveGoalID != "" {
		result["active_goal_id"] = thread.ActiveGoalID
	}
	return result
}

func LinkResponse(link agentconversation.ThreadGoalLink) gin.H {
	return gin.H{
		"thread_id":  link.ThreadID,
		"goal_id":    link.GoalID,
		"active":     link.Active,
		"linked_at":  link.LinkedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": link.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func MessageResponse(message agentconversation.Message) gin.H {
	result := gin.H{
		"message_id": message.ID,
		"thread_id":  message.ThreadID,
		"sequence":   message.Sequence,
		"role":       message.Role,
		"content":    message.Content,
		"created_at": message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if message.ClientMessageID != "" {
		result["client_message_id"] = message.ClientMessageID
	}
	if message.ProducedByRunID != "" {
		result["produced_by_run_id"] = message.ProducedByRunID
	}
	if message.Audio != nil {
		result["modality"] = agentconversation.MessageModalityVoice
		result["audio"] = MessageAudioResponse(*message.Audio)
	} else if message.Modality == agentconversation.MessageModalityMultimodal {
		result["modality"] = agentconversation.MessageModalityMultimodal
	}
	if message.SpeechFeedbackStatusURL != "" {
		result["speech_feedback_status_url"] = message.SpeechFeedbackStatusURL
	}
	return result
}

func MessageAudioResponse(audio agentconversation.MessageAudio) gin.H {
	result := gin.H{
		"audio_id":     audio.ID,
		"status":       audio.Status,
		"content_type": audio.ContentType,
		"size_bytes":   audio.Size,
		"duration_ms":  durationMilliseconds(audio.Duration),
	}
	if audio.Status == agentconversation.MessageAudioReadable {
		result["playback_path"] =
			"/v1/agent-message-audios/" + audio.ID + "/playback"
	}
	if !audio.DeletedAt.IsZero() {
		result["deleted_at"] = audio.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
}

func (handler *Handler) messageResponseWithHandoffs(
	ctx context.Context,
	trusted requestcontext.Actor,
	message agentconversation.Message,
) (gin.H, error) {
	if handler.speechFeedback != nil &&
		message.SpeechFeedbackStatusURL == "" &&
		message.Role == agentconversation.MessageRoleUser &&
		message.Audio != nil {
		statusURL, found, err := handler.speechFeedback.
			StatusURLForAgentVoiceMessage(ctx, trusted, message.ID)
		if err != nil {
			return nil, err
		}
		if found {
			message.SpeechFeedbackStatusURL = statusURL
		}
	}
	response := MessageResponse(message)
	if message.Modality == agentconversation.MessageModalityMultimodal {
		if handler.images == nil {
			return nil, agentimage.ErrRepository
		}
		assets, err := handler.images.MessageAssets(
			ctx, trusted, message.ThreadID, message.ID,
		)
		if err != nil {
			return nil, err
		}
		images := make([]gin.H, 0, len(assets))
		for _, asset := range assets {
			images = append(images, ImageAssetResponse(asset))
		}
		response["images"] = images
	}
	if handler.toolCalls == nil ||
		message.Role != agentconversation.MessageRoleAssistant ||
		message.ProducedByRunID == "" {
		return response, nil
	}
	records, err := handler.toolCalls.GetToolCalls(
		ctx, trusted, message.ProducedByRunID,
	)
	if err != nil {
		return nil, err
	}
	handoffs, err := messageHandoffs(records)
	if err != nil {
		return nil, err
	}
	if len(handoffs) > 0 {
		response["handoffs"] = handoffs
	}
	return response, nil
}

func ImageAssetResponse(asset agentimage.Asset) gin.H {
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

func messageHandoffs(
	records []agentrun.ToolCall,
) ([]agenthandoff.Item, error) {
	handoffs := make([]agenthandoff.Item, 0, 1)
	for _, record := range records {
		if record.Status != agentrun.ToolCallSucceeded ||
			len(record.Handoffs) == 0 {
			continue
		}
		if err := agenthandoff.ValidateItems(record.Handoffs); err != nil {
			return nil, agentrun.ErrRepository
		}
		handoffs = append(handoffs, agenthandoff.CloneItems(record.Handoffs)...)
	}
	if err := agenthandoff.ValidateItems(handoffs); err != nil {
		return nil, agentrun.ErrRepository
	}
	return handoffs, nil
}

func actor(c *gin.Context) (requestcontext.Actor, bool) {
	return requestcontext.ActorFromContext(c.Request.Context())
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
	case errors.Is(err, agentconversation.ErrInvalidRequest),
		errors.Is(err, agentrun.ErrInvalidRequest),
		errors.Is(err, agentimage.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, agentconversation.ErrNotFound),
		errors.Is(err, agentrun.ErrNotFound),
		errors.Is(err, agentimage.ErrNotFound):
		return apperror.New(
			apperror.NotFound, "resource_not_found",
			"Resource was not found.", apperror.WithCause(err),
		)
	case errors.Is(err, agentconversation.ErrIdempotencyConflict),
		errors.Is(err, agentrun.ErrIdempotencyConflict),
		errors.Is(err, agentimage.ErrIdempotencyConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, agentconversation.ErrConflict),
		errors.Is(err, agentrun.ErrConflict),
		errors.Is(err, agentimage.ErrConflict):
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
		apperror.InvalidArgument, "invalid_request",
		"Request validation failed.", apperror.WithCause(cause),
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
