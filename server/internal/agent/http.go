package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	maxAgentDataRequestBody = 64 * 1024
	agentDataReadTimeout    = 5 * time.Second
)

type CorrelationIDGenerator func() string

type HTTPHandler struct {
	application   Application
	matters       matter.Application
	authenticator identity.Authenticator
	correlationID CorrelationIDGenerator
}

func NewHTTPHandler(
	application Application,
	matters matter.Application,
	authenticator identity.Authenticator,
	correlationID CorrelationIDGenerator,
) (*HTTPHandler, error) {
	if application == nil || matters == nil || authenticator == nil {
		return nil, errors.New("agent: HTTP dependency is required")
	}
	if correlationID == nil {
		correlationID = newCorrelationID
	}
	return &HTTPHandler{
		application:   application,
		matters:       matters,
		authenticator: authenticator,
		correlationID: correlationID,
	}, nil
}

func (h *HTTPHandler) RegisterRoutes(router *gin.Engine) {
	protected := router.Group("")
	protected.Use(h.authenticationMiddleware())

	protected.POST("/v1/matters", h.createMatter)
	protected.GET("/v1/matters", h.listMatters)
	protected.PATCH("/v1/matters/:matter_id", h.changeMatterStatus)

	protected.POST("/v1/agent-threads", h.createThread)
	protected.GET("/v1/agent-threads", h.listThreads)
	protected.GET("/v1/agent-threads/:thread_id", h.getThread)
	protected.PUT(
		"/v1/agent-threads/:thread_id/active-matter",
		h.setActiveMatter,
	)
	protected.POST(
		"/v1/agent-threads/:thread_id/messages",
		h.appendMessage,
	)
	protected.GET(
		"/v1/agent-threads/:thread_id/messages",
		h.listMessages,
	)
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
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	threads, err := h.application.ListThreads(c.Request.Context(), actor)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	result := make([]gin.H, 0, len(threads))
	for _, thread := range threads {
		result = append(result, threadResponse(thread))
	}
	c.JSON(http.StatusOK, gin.H{"threads": result})
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

func (h *HTTPHandler) appendMessage(c *gin.Context) {
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
	message, err := h.application.AppendUserMessage(
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
	c.JSON(http.StatusCreated, messageResponse(message))
}

func (h *HTTPHandler) listMessages(c *gin.Context) {
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	messages, err := h.application.ListMessages(
		c.Request.Context(),
		actor,
		c.Param("thread_id"),
	)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	result := make([]gin.H, 0, len(messages))
	for _, message := range messages {
		result = append(result, messageResponse(message))
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
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
