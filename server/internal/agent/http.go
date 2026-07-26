package agent

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	maxAgentDataRequestBody = 64 * 1024
	agentDataReadTimeout    = 5 * time.Second
	defaultThreadPageSize   = 20
	defaultMessagePageSize  = 50
	defaultVoiceReadTimeout = 15 * time.Second
	maxReviewHistoryBody    = 768 * 1024
	reviewCursorVersion     = 1
	reviewCursorKind        = "formal_reviews"
	minReviewCursorKeyBytes = 32
)

type CorrelationIDGenerator func() string

type VoiceHTTPOptions struct {
	AudioReadTimeout       time.Duration
	ReviewHistoryCursorKey []byte
}

type HTTPHandler struct {
	application      Application
	runs             RunApplication
	voice            *VoiceSessionApplication
	audioAssets      conversation.AudioAssetHTTPService
	matters          matter.Application
	authenticator    identity.Authenticator
	correlationID    CorrelationIDGenerator
	voiceReadTimeout time.Duration
	reviewCursorKey  []byte
}

func NewHTTPHandler(
	application Application,
	matters matter.Application,
	authenticator identity.Authenticator,
	correlationID CorrelationIDGenerator,
) (*HTTPHandler, error) {
	return NewHTTPHandlerWithRuns(
		application,
		nil,
		matters,
		authenticator,
		correlationID,
	)
}

func NewHTTPHandlerWithRuns(
	application Application,
	runs RunApplication,
	matters matter.Application,
	authenticator identity.Authenticator,
	correlationID CorrelationIDGenerator,
) (*HTTPHandler, error) {
	return NewHTTPHandlerWithRunsAndVoice(
		application,
		runs,
		nil,
		matters,
		authenticator,
		correlationID,
	)
}

func NewHTTPHandlerWithRunsAndVoice(
	application Application,
	runs RunApplication,
	voice *VoiceSessionApplication,
	matters matter.Application,
	authenticator identity.Authenticator,
	correlationID CorrelationIDGenerator,
	voiceOptions ...VoiceHTTPOptions,
) (*HTTPHandler, error) {
	return NewHTTPHandlerWithRunsVoiceAndAudio(
		application,
		runs,
		voice,
		nil,
		matters,
		authenticator,
		correlationID,
		voiceOptions...,
	)
}

func NewHTTPHandlerWithRunsVoiceAndAudio(
	application Application,
	runs RunApplication,
	voice *VoiceSessionApplication,
	audioAssets conversation.AudioAssetHTTPService,
	matters matter.Application,
	authenticator identity.Authenticator,
	correlationID CorrelationIDGenerator,
	voiceOptions ...VoiceHTTPOptions,
) (*HTTPHandler, error) {
	if application == nil || matters == nil || authenticator == nil {
		return nil, errors.New("agent: HTTP dependency is required")
	}
	if correlationID == nil {
		correlationID = newCorrelationID
	}
	voiceReadTimeout := defaultVoiceReadTimeout
	if len(voiceOptions) > 1 {
		return nil, errors.New("agent: duplicate voice HTTP options")
	}
	if len(voiceOptions) == 1 {
		if voiceOptions[0].AudioReadTimeout < 0 {
			return nil, errors.New("agent: voice audio read timeout is required")
		}
		if voiceOptions[0].AudioReadTimeout > 0 {
			voiceReadTimeout = voiceOptions[0].AudioReadTimeout
		}
	}
	var reviewCursorKey []byte
	if voice != nil {
		if len(voiceOptions) != 1 ||
			len(voiceOptions[0].ReviewHistoryCursorKey) <
				minReviewCursorKeyBytes {
			return nil, errors.New(
				"agent: Review history cursor signing key is required",
			)
		}
		reviewCursorKey = append(
			[]byte(nil),
			voiceOptions[0].ReviewHistoryCursorKey...,
		)
	}
	return &HTTPHandler{
		application:      application,
		runs:             runs,
		voice:            voice,
		audioAssets:      audioAssets,
		matters:          matters,
		authenticator:    authenticator,
		correlationID:    correlationID,
		voiceReadTimeout: voiceReadTimeout,
		reviewCursorKey:  reviewCursorKey,
	}, nil
}

func (h *HTTPHandler) RegisterRoutes(router *gin.Engine) {
	protected := router.Group("")
	protected.Use(h.authenticationMiddleware())

	protected.POST("/v1/matters", h.createMatter)
	protected.GET("/v1/matters", h.listMatters)
	protected.GET("/v1/matters/:matter_id", h.getMatter)
	protected.PATCH("/v1/matters/:matter_id", h.changeMatterStatus)

	protected.POST("/v1/agent-threads", h.createThread)
	protected.GET("/v1/agent-threads", h.listThreads)
	protected.GET("/v1/agent-threads/focused", h.getFocusedThread)
	protected.PUT("/v1/agent-threads/focused", h.setFocusedThread)
	protected.DELETE("/v1/agent-threads/focused", h.clearFocusedThread)
	protected.GET("/v1/agent-threads/:thread_id", h.getThread)
	protected.PUT(
		"/v1/agent-threads/:thread_id/active-matter",
		h.setActiveMatter,
	)
	// User Message writes are intentionally exposed only through /runs so the
	// Message and its initial pending Run cannot be committed independently.
	protected.GET(
		"/v1/agent-threads/:thread_id/messages",
		h.listMessages,
	)
	if h.runs != nil {
		protected.POST(
			"/v1/agent-threads/:thread_id/runs",
			h.submitRun,
		)
		protected.GET("/v1/agent-runs/:run_id", h.getRun)
		protected.POST(
			"/v1/agent-runs/:run_id/retries",
			h.retryRun,
		)
		protected.GET(
			"/v1/agent-runs/:run_id/context-manifest",
			h.getContextManifest,
		)
	}
	if h.voice != nil {
		protected.POST(
			"/v1/agent-threads/:thread_id/voice-practice-sessions",
			h.startVoiceSession,
		)
		protected.GET(
			"/v1/agent-threads/:thread_id/voice-practice-session",
			h.resumeVoiceSession,
		)
		protected.POST(
			"/v1/voice-practice-sessions/:practice_session_id/questions/:question_id/transcription-candidates",
			h.transcribeVoiceCandidate,
		)
		protected.POST(
			"/v1/transcription-candidates/:candidate_id/confirmations",
			h.confirmVoiceCandidate,
		)
		protected.GET(
			"/v1/voice-questions/:question_id/speech",
			h.questionSpeech,
		)
		protected.GET("/v1/formal-reviews", h.listFormalReviews)
		protected.GET("/v1/formal-reviews/:review_id", h.getFormalReview)
	}
	if h.audioAssets != nil {
		_ = conversation.RegisterAudioAssetRoutes(
			protected,
			h.audioAssets,
			conversation.AudioAssetActorResolverFunc(
				func(request *http.Request) (
					conversation.AudioAssetActor,
					bool,
				) {
					actor, ok := requestcontext.ActorFromContext(
						request.Context(),
					)
					return conversation.AudioAssetActor{
						UserID: actor.UserID,
					}, ok && actor.Valid()
				},
			),
		)
	}
}

func (h *HTTPHandler) createMatter(c *gin.Context) {
	values, ok := decodeObject(c, []string{"title"}, []string{"title"})
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	title, ok := decodeString(values["title"])
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	item, err := h.matters.Create(c.Request.Context(), actor, title)
	if err != nil {
		h.writeMatterError(c, err)
		return
	}
	c.JSON(http.StatusCreated, matterResponse(item))
}

func (h *HTTPHandler) listMatters(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	items, err := h.matters.List(c.Request.Context(), actor)
	if err != nil {
		h.writeMatterError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, matterResponse(item))
	}
	c.JSON(http.StatusOK, gin.H{"matters": result})
}

func (h *HTTPHandler) getMatter(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	item, err := h.matters.ReadOwned(
		c.Request.Context(),
		actor,
		c.Param("matter_id"),
	)
	if err != nil {
		h.writeMatterError(c, err)
		return
	}
	c.JSON(http.StatusOK, matterResponse(item))
}

func (h *HTTPHandler) changeMatterStatus(c *gin.Context) {
	values, ok := decodeObject(
		c,
		[]string{"status", "expected_version"},
		[]string{"status", "expected_version"},
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	status, statusOK := decodeString(values["status"])
	expectedVersion, versionOK := decodeInt64(values["expected_version"])
	if !statusOK || !versionOK {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	item, err := h.matters.ChangeStatus(
		c.Request.Context(),
		actor,
		c.Param("matter_id"),
		expectedVersion,
		matter.Status(status),
	)
	if err != nil {
		h.writeMatterError(c, err)
		return
	}
	c.JSON(http.StatusOK, matterResponse(item))
}

func (h *HTTPHandler) createThread(c *gin.Context) {
	values, ok := decodeObject(
		c,
		[]string{"active_matter_id"},
		nil,
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	activeMatterID := ""
	if raw, exists := values["active_matter_id"]; exists {
		activeMatterID, ok = decodeString(raw)
		if !ok || activeMatterID == "" {
			h.writeError(c, http.StatusBadRequest, "invalid_request", false)
			return
		}
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, err := h.application.CreateThread(
		c.Request.Context(),
		actor,
		activeMatterID,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, threadResponse(thread))
}

func (h *HTTPHandler) listThreads(c *gin.Context) {
	pageSize, cursor, ok := decodeAgentPageQuery(
		c.Request.URL.RawQuery,
		defaultThreadPageSize,
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	page, err := h.application.PageThreads(
		c.Request.Context(),
		actor,
		pageSize,
		cursor,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	threads := make([]gin.H, 0, len(page.Threads))
	for _, thread := range page.Threads {
		threads = append(threads, threadResponse(thread))
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

func (h *HTTPHandler) getFocusedThread(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, found, err := h.application.GetFocusedThread(
		c.Request.Context(),
		actor,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	if !found {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, threadResponse(thread))
}

func (h *HTTPHandler) setFocusedThread(c *gin.Context) {
	values, ok := decodeObject(c, []string{"thread_id"}, []string{"thread_id"})
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	threadID, ok := decodeString(values["thread_id"])
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, err := h.application.SetFocusedThread(
		c.Request.Context(),
		actor,
		threadID,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, threadResponse(thread))
}

func (h *HTTPHandler) clearFocusedThread(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	if err := h.application.ClearFocusedThread(
		c.Request.Context(),
		actor,
	); err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HTTPHandler) getThread(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, err := h.application.GetThread(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, threadResponse(thread))
}

func (h *HTTPHandler) setActiveMatter(c *gin.Context) {
	values, ok := decodeObject(c, []string{"matter_id"}, []string{"matter_id"})
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	matterID, ok := decodeString(values["matter_id"])
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	link, err := h.application.SetActiveMatter(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
		matterID,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, linkResponse(link))
}

func (h *HTTPHandler) listMessages(c *gin.Context) {
	pageSize, cursor, ok := decodeAgentPageQuery(
		c.Request.URL.RawQuery,
		defaultMessagePageSize,
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	page, err := h.application.PageMessages(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
		pageSize,
		cursor,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	messages := make([]gin.H, 0, len(page.Messages))
	for _, message := range page.Messages {
		messages = append(messages, messageResponse(message))
	}
	result := gin.H{"messages": messages}
	if page.NextCursor != "" {
		result["next_cursor"] = page.NextCursor
	}
	c.JSON(http.StatusOK, result)
}

func decodeAgentPageQuery(
	rawQuery string,
	defaultPageSize int,
) (int, string, bool) {
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
		if len(rawValues) != 1 || !decimalDigits(rawValues[0]) {
			return 0, "", false
		}
		pageSize, err = strconv.Atoi(rawValues[0])
		if err != nil || pageSize < 1 || pageSize > maxAgentPageSize {
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

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (h *HTTPHandler) submitRun(c *gin.Context) {
	values, ok := decodeObject(
		c,
		[]string{"client_message_id", "content"},
		[]string{"client_message_id", "content"},
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	clientMessageID, clientIDOK := decodeString(values["client_message_id"])
	content, contentOK := decodeString(values["content"])
	if !clientIDOK || !contentOK {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	submission, err := h.runs.SubmitText(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
		clientMessageID,
		content,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(runWriteStatus(submission.Run), runResponse(submission.Run))
}

func (h *HTTPHandler) retryRun(c *gin.Context) {
	values, ok := decodeObject(
		c,
		[]string{"client_retry_id"},
		[]string{"client_retry_id"},
	)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	retryClientID, ok := decodeString(values["client_retry_id"])
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	retry, err := h.runs.RetryText(
		c.Request.Context(),
		actor,
		c.Param("run_id"),
		retryClientID,
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(runWriteStatus(retry.Run), runResponse(retry.Run))
}

func (h *HTTPHandler) getRun(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	run, err := h.runs.GetRun(
		c.Request.Context(),
		actor,
		c.Param("run_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, runResponse(run))
}

func (h *HTTPHandler) getContextManifest(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	manifest, err := h.runs.GetContextManifest(
		c.Request.Context(),
		actor,
		c.Param("run_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, contextManifestResponse(manifest))
}

func (h *HTTPHandler) startVoiceSession(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	key, ok := voiceIdempotencyKey(c)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, err := h.application.GetThread(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	if thread.ActiveMatterID == "" {
		h.writeError(c, http.StatusConflict, "resource_conflict", false)
		return
	}
	state, err := h.voice.Start(
		c.Request.Context(),
		actor,
		thread.ID,
		thread.ActiveMatterID,
		key,
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, voiceSessionStateResponse(state))
}

func (h *HTTPHandler) resumeVoiceSession(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	thread, err := h.application.GetThread(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	if thread.ActiveMatterID == "" {
		h.writeError(c, http.StatusConflict, "resource_conflict", false)
		return
	}
	state, err := h.voice.Resume(
		c.Request.Context(),
		actor,
		thread.ID,
		thread.ActiveMatterID,
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	ownedMatter, err := h.matters.ReadOwned(
		c.Request.Context(),
		actor,
		state.Matter.ID,
	)
	if err != nil || ownedMatter.ID != state.Matter.ID {
		h.writeError(c, http.StatusNotFound, "resource_not_found", false)
		return
	}
	c.JSON(http.StatusOK, voiceSessionStateResponse(state))
}

func (h *HTTPHandler) transcribeVoiceCandidate(c *gin.Context) {
	key, ok := voiceIdempotencyKey(c)
	if !ok || c.Request.Body == nil ||
		c.Request.ContentLength > platformmedia.MaxAudioBytes ||
		!strings.EqualFold(
			strings.TrimSpace(c.GetHeader("Content-Type")),
			platformmedia.ContentTypeWAV,
		) {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(
		time.Now().Add(h.voiceReadTimeout),
	); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
		return
	}
	defer func() {
		_ = controller.SetReadDeadline(time.Time{})
	}()
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		platformmedia.MaxAudioBytes,
	)
	candidate, err := h.voice.Transcribe(
		c.Request.Context(),
		actor,
		conversation.TranscribeVoiceCommand{
			SessionID:      c.Param("practice_session_id"),
			QuestionID:     c.Param("question_id"),
			IdempotencyKey: key,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          body,
		},
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, transcriptionCandidateResponse(candidate))
}

func (h *HTTPHandler) confirmVoiceCandidate(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	key, ok := voiceIdempotencyKey(c)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	state, err := h.voice.Confirm(
		c.Request.Context(),
		actor,
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    c.Param("candidate_id"),
			IdempotencyKey: key,
		},
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, voiceSessionStateResponse(state))
}

func (h *HTTPHandler) questionSpeech(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	speech, err := h.voice.QuestionSpeech(
		c.Request.Context(),
		actor,
		c.Param("question_id"),
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	if speech.Audio == nil {
		if speech.Failure != nil {
			h.writeProviderError(c, speech.Failure.Kind)
		} else {
			c.Header("Retry-After", "1")
			h.writeError(
				c,
				http.StatusServiceUnavailable,
				"provider_unavailable",
				true,
			)
		}
		return
	}
	defer func() { _ = speech.Audio.Close() }()
	reader, err := speech.Audio.Open()
	if err != nil {
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
		return
	}
	defer reader.Close()
	c.DataFromReader(
		http.StatusOK,
		speech.Audio.Size(),
		speech.Audio.MediaType(),
		reader,
		nil,
	)
}

func (h *HTTPHandler) getFormalReview(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	formalReview, err := h.voice.GetReview(
		c.Request.Context(),
		actor,
		c.Param("review_id"),
	)
	if err != nil {
		if errors.Is(err, ErrInvalidContext) {
			h.writeError(
				c,
				http.StatusInternalServerError,
				"internal_error",
				true,
			)
			return
		}
		h.writeVoiceError(c, err)
		return
	}
	h.writeBoundedReviewJSON(c, formalReviewResponse(formalReview))
}

func (h *HTTPHandler) listFormalReviews(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	query, ok := h.decodeReviewHistoryQuery(c.Request, actor.UserID)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	page, err := h.voice.ListReviews(c.Request.Context(), actor, query)
	if err != nil {
		if errors.Is(err, ErrInvalidContext) {
			h.writeError(
				c,
				http.StatusInternalServerError,
				"internal_error",
				true,
			)
			return
		}
		h.writeVoiceError(c, err)
		return
	}
	items := make([]gin.H, len(page.Items))
	for index, item := range page.Items {
		items[index] = formalReviewResponse(item)
	}
	result := gin.H{"items": items}
	if page.Next != nil {
		cursor, cursorOK := h.encodeReviewHistoryCursor(
			actor.UserID,
			*page.Next,
		)
		if !cursorOK {
			h.writeError(
				c,
				http.StatusInternalServerError,
				"internal_error",
				true,
			)
			return
		}
		result["next_cursor"] = cursor
	}
	h.writeBoundedReviewJSON(c, result)
}

func (h *HTTPHandler) writeBoundedReviewJSON(
	c *gin.Context,
	value any,
) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > maxReviewHistoryBody {
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
		return
	}
	c.Data(
		http.StatusOK,
		"application/json; charset=utf-8",
		payload,
	)
}

func (h *HTTPHandler) authenticationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.Request.Header.Values("Authorization"))
		if !ok {
			h.writeAuthenticationRequired(c)
			c.Abort()
			return
		}
		actor, err := h.authenticator.AuthenticateSession(
			c.Request.Context(),
			token,
		)
		if err != nil {
			if errors.Is(err, identity.ErrAuthenticationRequired) {
				h.writeAuthenticationRequired(c)
			} else {
				h.writeError(
					c,
					http.StatusInternalServerError,
					"internal_error",
					true,
				)
			}
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(
			requestcontext.WithActor(c.Request.Context(), actor),
		)
		c.Next()
	}
}

func bearerToken(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

func trustedActor(c *gin.Context) (requestcontext.Actor, bool) {
	return requestcontext.ActorFromContext(c.Request.Context())
}

func (h *HTTPHandler) writeMatterError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, matter.ErrInvalidRequest):
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
	case errors.Is(err, matter.ErrNotFound):
		h.writeError(c, http.StatusNotFound, "resource_not_found", false)
	case errors.Is(err, matter.ErrConflict):
		h.writeError(c, http.StatusConflict, "resource_conflict", false)
	default:
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
	}
}

func (h *HTTPHandler) writeAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
	case errors.Is(err, ErrNotFound):
		h.writeError(c, http.StatusNotFound, "resource_not_found", false)
	case errors.Is(err, ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, "idempotency_key_conflict", false)
	case errors.Is(err, ErrConflict):
		h.writeError(c, http.StatusConflict, "resource_conflict", false)
	default:
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
	}
}

func (h *HTTPHandler) writeVoiceError(c *gin.Context, err error) {
	var speechError *ai.SpeechError
	var generationError *ai.GenerationError
	switch {
	case errors.Is(err, ErrInvalidRequest),
		errors.Is(err, ErrInvalidContext),
		errors.Is(err, conversation.ErrVoiceRoundInvalid):
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
	case errors.Is(err, ErrNotFound),
		errors.Is(err, conversation.ErrVoiceRoundNotFound):
		h.writeError(c, http.StatusNotFound, "resource_not_found", false)
	case errors.Is(err, ErrIdempotencyConflict),
		errors.Is(err, conversation.ErrVoiceRoundConflict):
		h.writeError(c, http.StatusConflict, "idempotency_key_conflict", false)
	case errors.Is(err, ErrConflict):
		h.writeError(c, http.StatusConflict, "resource_conflict", false)
	case errors.Is(err, conversation.ErrVoiceRoundProcessing):
		c.Header("Retry-After", "1")
		h.writeError(c, http.StatusConflict, "resource_processing", true)
	case errors.Is(err, conversation.ErrVoiceRoundCapacity):
		c.Header("Retry-After", "1")
		h.writeError(
			c,
			http.StatusServiceUnavailable,
			"voice_capacity_exhausted",
			true,
		)
	case errors.As(err, &speechError):
		h.writeProviderError(c, speechError.Kind)
	case errors.As(err, &generationError):
		h.writeProviderError(c, generationError.Kind)
	default:
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
	}
}

func (h *HTTPHandler) writeProviderError(
	c *gin.Context,
	kind ai.ErrorKind,
) {
	retryable := kind.Retryable()
	code := "provider_unavailable"
	if kind == ai.ErrorQuotaExhausted {
		code = "quota_exhausted"
	}
	if retryable {
		c.Header("Retry-After", "1")
	}
	h.writeError(
		c,
		http.StatusServiceUnavailable,
		code,
		retryable,
	)
}

func (h *HTTPHandler) writeAuthenticationRequired(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	h.writeError(c, http.StatusUnauthorized, "authentication_required", false)
}

func (h *HTTPHandler) writeError(
	c *gin.Context,
	status int,
	code string,
	retryable bool,
) {
	messages := map[string]string{
		"invalid_request":          "Request validation failed.",
		"authentication_required":  "Authentication is required.",
		"resource_not_found":       "Resource was not found.",
		"resource_conflict":        "Resource state conflicts with this operation.",
		"idempotency_key_conflict": "Idempotency key conflicts with the original request.",
		"resource_processing":      "Resource processing is still in progress.",
		"provider_unavailable":     "The configured provider is temporarily unavailable.",
		"quota_exhausted":          "The configured provider quota is exhausted.",
		"voice_capacity_exhausted": "Voice processing capacity is temporarily exhausted.",
		"internal_error":           "An internal error occurred.",
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        messages[code],
			"retryable":      retryable,
			"correlation_id": h.correlationID(),
		},
	})
}

func voiceIdempotencyKey(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	key := strings.TrimSpace(values[0])
	return key, len(key) >= 8 && len(key) <= 128 &&
		!strings.ContainsAny(key, "\r\n\x00")
}

func voiceSessionStateResponse(state VoiceSessionState) gin.H {
	result := gin.H{
		"practice_session_id": state.Session.ID,
		"practice_plan_id":    state.Session.PlanID,
		"thread_id":           state.Session.ThreadID,
		"matter":              matterResponse(state.Matter),
		"session_version":     state.Session.SessionVersion,
		"effective_turns":     state.Session.EffectiveTurns,
		"turn_limit":          state.Session.TurnLimit,
		"session_completed":   state.Session.Completed,
	}
	if state.Question != nil {
		result["current_question"] = voiceQuestionResponse(*state.Question)
	}
	if state.Turn != nil {
		result["current_turn"] = confirmedVoiceTurnResponse(*state.Turn)
	}
	if state.Review != nil {
		result["review"] = formalReviewResponse(*state.Review)
	}
	return result
}

func voiceQuestionResponse(question conversation.VoiceQuestion) gin.H {
	return gin.H{
		"question_id":               question.ID,
		"practice_session_id":       question.SessionID,
		"content":                   question.Text,
		"speaker_participant_id":    question.SpeakerParticipantID,
		"addressee_participant_ids": question.AddresseeParticipantIDs,
		"speech_path":               "/v1/voice-questions/" + question.ID + "/speech",
	}
}

func transcriptionCandidateResponse(
	candidate conversation.TranscriptionCandidate,
) gin.H {
	return gin.H{
		"candidate_id":              candidate.ID,
		"practice_session_id":       candidate.SessionID,
		"question_id":               candidate.QuestionID,
		"respondent_participant_id": candidate.RespondentParticipantID,
		"transcript_id":             candidate.TranscriptID,
		"evidence_version":          candidate.EvidenceVersion,
		"transcript":                candidate.Transcript,
		"created_at":                candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func confirmedVoiceTurnResponse(turn conversation.ConfirmedVoiceTurn) gin.H {
	result := gin.H{
		"turn_id":                   turn.ID,
		"practice_session_id":       turn.SessionID,
		"question_id":               turn.QuestionID,
		"respondent_participant_id": turn.RespondentParticipantID,
		"candidate_id":              turn.CandidateID,
		"answer_text":               turn.AnswerText,
		"evidence_version":          turn.EvidenceVersion,
		"effective_turns":           turn.EffectiveTurns,
		"session_completed":         turn.SessionCompleted,
	}
	if turn.ReviewID != "" {
		result["review_id"] = turn.ReviewID
	}
	if turn.AudioAssetID != "" {
		result["audio_asset_id"] = turn.AudioAssetID
	}
	return result
}

func formalReviewResponse(item VoiceSessionReview) gin.H {
	result := gin.H{
		"review_id":              item.ID,
		"practice_session_id":    item.SessionID,
		"status":                 item.Status,
		"implementation_version": item.ImplementationVersion,
		"source_turn_id":         item.SourceTurnID,
		"source_turn_version":    item.SourceTurnVersion,
		"created_at":             item.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":             item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if item.Result != nil {
		result["result"] = item.Result
	}
	if item.CompletedAt != nil {
		result["completed_at"] = item.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func (h *HTTPHandler) decodeReviewHistoryQuery(
	request *http.Request,
	actorUserID string,
) (VoiceReviewHistoryQuery, bool) {
	const defaultLimit = 20
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return VoiceReviewHistoryQuery{}, false
	}
	for key := range values {
		if key != "limit" && key != "cursor" {
			return VoiceReviewHistoryQuery{}, false
		}
	}
	query := VoiceReviewHistoryQuery{Limit: defaultLimit}
	if limitValues, exists := values["limit"]; exists {
		if len(limitValues) != 1 {
			return VoiceReviewHistoryQuery{}, false
		}
		limit, err := strconv.Atoi(limitValues[0])
		if err != nil || limit < 1 || limit > 50 {
			return VoiceReviewHistoryQuery{}, false
		}
		query.Limit = limit
	}
	if cursorValues, exists := values["cursor"]; exists {
		if len(cursorValues) != 1 {
			return VoiceReviewHistoryQuery{}, false
		}
		cursor, ok := h.decodeReviewHistoryCursor(
			actorUserID,
			cursorValues[0],
		)
		if !ok {
			return VoiceReviewHistoryQuery{}, false
		}
		query.Before = &cursor
	}
	return query, true
}

type reviewHistoryCursorEnvelope struct {
	Version   int    `json:"v"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	ReviewID  string `json:"review_id"`
}

func (h *HTTPHandler) encodeReviewHistoryCursor(
	actorUserID string,
	cursor VoiceReviewHistoryCursor,
) (string, bool) {
	if h == nil || len(h.reviewCursorKey) < minReviewCursorKeyBytes ||
		strings.TrimSpace(actorUserID) == "" ||
		cursor.CreatedAt.IsZero() ||
		!validUUID(cursor.ReviewID) {
		return "", false
	}
	payload, err := json.Marshal(reviewHistoryCursorEnvelope{
		Version:   reviewCursorVersion,
		Kind:      reviewCursorKind,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ReviewID:  cursor.ReviewID,
	})
	if err != nil {
		return "", false
	}
	signature := reviewHistoryCursorMAC(
		h.reviewCursorKey,
		actorUserID,
		payload,
	)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature), true
}

func (h *HTTPHandler) decodeReviewHistoryCursor(
	actorUserID string,
	value string,
) (VoiceReviewHistoryCursor, bool) {
	if h == nil || len(h.reviewCursorKey) < minReviewCursorKeyBytes ||
		strings.TrimSpace(actorUserID) == "" ||
		value == "" || len(value) > 512 ||
		strings.Count(value, ".") != 1 {
		return VoiceReviewHistoryCursor{}, false
	}
	parts := strings.SplitN(value, ".", 2)
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(payload) > 256 {
		return VoiceReviewHistoryCursor{}, false
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size ||
		!hmac.Equal(
			signature,
			reviewHistoryCursorMAC(
				h.reviewCursorKey,
				actorUserID,
				payload,
			),
		) {
		return VoiceReviewHistoryCursor{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope reviewHistoryCursorEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Version != reviewCursorVersion ||
		envelope.Kind != reviewCursorKind ||
		!validUUID(envelope.ReviewID) {
		return VoiceReviewHistoryCursor{}, false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return VoiceReviewHistoryCursor{}, false
	}
	cursor := VoiceReviewHistoryCursor{
		CreatedAt: createdAt,
		ReviewID:  envelope.ReviewID,
	}
	canonical, ok := h.encodeReviewHistoryCursor(actorUserID, cursor)
	if !ok || canonical != value {
		return VoiceReviewHistoryCursor{}, false
	}
	return cursor, true
}

func reviewHistoryCursorMAC(
	key []byte,
	actorUserID string,
	payload []byte,
) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(reviewCursorKind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(actorUserID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func matterResponse(item matter.Matter) gin.H {
	return gin.H{
		"matter_id":  item.ID,
		"title":      item.Title,
		"status":     item.Status,
		"version":    item.Version,
		"created_at": item.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func threadResponse(thread Thread) gin.H {
	result := gin.H{
		"thread_id":  thread.ID,
		"created_at": thread.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": thread.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if thread.ActiveMatterID != "" {
		result["active_matter_id"] = thread.ActiveMatterID
	}
	return result
}

func linkResponse(link ThreadMatterLink) gin.H {
	return gin.H{
		"thread_id":  link.ThreadID,
		"matter_id":  link.MatterID,
		"active":     link.Active,
		"linked_at":  link.LinkedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": link.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func messageResponse(message Message) gin.H {
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
	return result
}

func runWriteStatus(run Run) int {
	if run.Status == RunStatusPending || run.Status == RunStatusRunning {
		return http.StatusAccepted
	}
	return http.StatusCreated
}

func runResponse(run Run) gin.H {
	result := gin.H{
		"run_id":             run.ID,
		"thread_id":          run.ThreadID,
		"input_message_id":   run.InputMessageID,
		"attempt":            run.Attempt,
		"status":             run.Status,
		"requested_provider": run.RequestedProvider,
		"requested_model":    run.RequestedModel,
		"max_output_tokens":  run.MaxOutputTokens,
		"created_at":         run.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":         run.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
	if run.Status == RunStatusCompleted {
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
	if run.Status == RunStatusFailed {
		result["failure"] = gin.H{
			"kind":      run.FailureKind,
			"retryable": run.FailureRetryable,
		}
	}
	return result
}

func contextManifestResponse(manifest ContextManifest) gin.H {
	messages := make([]gin.H, 0, len(manifest.SelectedMessages))
	for _, message := range manifest.SelectedMessages {
		messages = append(messages, gin.H{
			"message_id": message.MessageID,
			"sequence":   message.Sequence,
			"role":       message.Role,
		})
	}
	result := gin.H{
		"run_id":                manifest.RunID,
		"thread_id":             manifest.ThreadID,
		"input_message_id":      manifest.InputMessageID,
		"instruction_version":   manifest.InstructionVersion,
		"selected_messages":     messages,
		"omitted_message_count": manifest.OmittedMessageCount,
		"trim_reason":           manifest.TrimReason,
		"max_input_characters":  manifest.MaxInputCharacters,
		"used_input_characters": manifest.UsedInputCharacters,
		"requested_provider":    manifest.RequestedProvider,
		"requested_model":       manifest.RequestedModel,
		"max_output_tokens":     manifest.MaxOutputTokens,
		"created_at":            manifest.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if manifest.ActiveMatterID != "" {
		result["active_matter"] = gin.H{
			"matter_id": manifest.ActiveMatterID,
			"version":   manifest.ActiveMatterVersion,
		}
	}
	return result
}

func decodeObject(
	c *gin.Context,
	allowed []string,
	required []string,
) (map[string]json.RawMessage, bool) {
	result := make(map[string]json.RawMessage)
	if !validJSONContentType(c.GetHeader("Content-Type")) {
		return result, false
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(
		time.Now().Add(agentDataReadTimeout),
	); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return result, false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxAgentDataRequestBody,
	)
	raw, err := io.ReadAll(body)
	if err != nil {
		return result, false
	}
	if err := controller.SetReadDeadline(time.Time{}); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return result, false
	}
	if !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return result, false
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, false
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return result, false
		}
		if _, exists := result[key]; exists {
			return result, false
		}
		if _, exists := allowedSet[key]; !exists {
			return result, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return result, false
		}
		result[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return result, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, false
	}
	for _, key := range required {
		if _, exists := result[key]; !exists {
			return result, false
		}
	}
	return result, true
}

func decodeString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func decodeInt64(raw json.RawMessage) (int64, bool) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
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

func validJSONSurrogates(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			if index+1 >= len(raw) {
				return false
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			codeUnit, ok := parseHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			switch {
			case codeUnit >= 0xd800 && codeUnit <= 0xdbff:
				if index+12 > len(raw) ||
					raw[index+6] != '\\' ||
					raw[index+7] != 'u' {
					return false
				}
				low, ok := parseHexCodeUnit(raw, index+8)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return false
				}
				index += 11
			case codeUnit >= 0xdc00 && codeUnit <= 0xdfff:
				return false
			default:
				index += 5
			}
		}
	}
	return true
}

func parseHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value |= uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value |= uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value |= uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func newCorrelationID() string {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "corr_unavailable"
	}
	return "corr_" + base64.RawURLEncoding.EncodeToString(value)
}
