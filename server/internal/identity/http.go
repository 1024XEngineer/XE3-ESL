package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	maxIdentityRequestBody     = 4096
	defaultIdentityReadTimeout = 5 * time.Second
)

type CorrelationIDGenerator func() string

type HTTPHandler struct {
	application     Application
	authenticator   Authenticator
	rateLimits      RateLimiters
	correlationID   CorrelationIDGenerator
	sourceIP        SourceIPResolver
	bodyReadTimeout time.Duration
}

type HTTPOption func(*HTTPHandler) error

func WithSourceIPResolver(resolver SourceIPResolver) HTTPOption {
	return func(handler *HTTPHandler) error {
		if resolver == nil {
			return errors.New("identity: source IP resolver is required")
		}
		handler.sourceIP = resolver
		return nil
	}
}

func WithBodyReadTimeout(timeout time.Duration) HTTPOption {
	return func(handler *HTTPHandler) error {
		if timeout <= 0 {
			return errors.New("identity: body read timeout must be positive")
		}
		handler.bodyReadTimeout = timeout
		return nil
	}
}

func NewHTTPHandler(
	application Application,
	authenticator Authenticator,
	rateLimits RateLimiters,
	correlationID CorrelationIDGenerator,
	options ...HTTPOption,
) (*HTTPHandler, error) {
	if application == nil || authenticator == nil ||
		rateLimits.RegistrationIP == nil ||
		rateLimits.LoginIP == nil ||
		rateLimits.LoginAccount == nil {
		return nil, errors.New("identity: HTTP dependency is required")
	}
	if correlationID == nil {
		correlationID = newCorrelationID
	}
	handler := &HTTPHandler{
		application:     application,
		authenticator:   authenticator,
		rateLimits:      rateLimits,
		correlationID:   correlationID,
		sourceIP:        directSourceIPResolver{},
		bodyReadTimeout: defaultIdentityReadTimeout,
	}
	for _, option := range options {
		if err := option(handler); err != nil {
			return nil, err
		}
	}
	return handler, nil
}

func (h *HTTPHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/v1/auth/register", h.register)
	router.POST("/v1/auth/login", h.login)

	protected := router.Group("")
	protected.Use(h.AuthenticationMiddleware())
	protected.POST("/v1/auth/logout", h.logout)
	protected.GET("/v1/me", h.currentUser)
}

func (h *HTTPHandler) register(c *gin.Context) {
	if !h.enforceLimit(
		c,
		h.rateLimits.RegistrationIP,
		"register-ip:"+h.sourceIP.Resolve(c.Request),
	) {
		return
	}
	request, ok := h.decodeCredentials(c)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	user, err := h.application.Register(
		c.Request.Context(),
		request.Email,
		request.Password,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, userResponse(user))
}

func (h *HTTPHandler) login(c *gin.Context) {
	sourceIP := h.sourceIP.Resolve(c.Request)
	if !h.enforceLimit(c, h.rateLimits.LoginIP, "login-ip:"+sourceIP) {
		return
	}
	request, ok := h.decodeCredentials(c)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	if !h.enforceLimit(
		c,
		h.rateLimits.LoginAccount,
		accountRateLimitKey(request.Email),
	) {
		return
	}
	result, err := h.application.Login(
		c.Request.Context(),
		request.Email,
		request.Password,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, gin.H{
		"user":          userResponse(result.User),
		"session_token": result.Token,
		"token_type":    "Bearer",
		"expires_at":    result.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func (h *HTTPHandler) logout(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	if err := h.application.Logout(c.Request.Context(), actor); err != nil {
		h.writeApplicationError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HTTPHandler) currentUser(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	user, err := h.application.CurrentUser(c.Request.Context(), actor)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	c.JSON(http.StatusOK, userResponse(user))
}

// AuthenticationMiddleware accepts only an Authorization Bearer credential,
// resolves it through the Session authority, and injects a server-trusted
// Actor into the standard request context for protected HTTP or WebSocket
// handlers.
func (h *HTTPHandler) AuthenticationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken, ok := bearerToken(c.Request.Header.Values("Authorization"))
		if !ok {
			h.writeAuthenticationRequired(c)
			c.Abort()
			return
		}
		actor, err := h.authenticator.AuthenticateSession(
			c.Request.Context(),
			rawToken,
		)
		if err != nil {
			if errors.Is(err, ErrAuthenticationRequired) {
				h.writeAuthenticationRequired(c)
			} else {
				h.writeError(c, http.StatusInternalServerError, "internal_error", true)
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

func (h *HTTPHandler) enforceLimit(
	c *gin.Context,
	limiter RateLimiter,
	key string,
) bool {
	decision := limiter.Allow(key)
	if decision.Allowed {
		return true
	}
	seconds := int64((decision.RetryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	h.writeError(c, http.StatusTooManyRequests, "rate_limited", true)
	return false
}

func (h *HTTPHandler) writeApplicationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
	case errors.Is(err, ErrInvalidCredentials):
		h.writeError(c, http.StatusUnauthorized, "invalid_credentials", false)
	case errors.Is(err, ErrRegistrationUnavailable):
		h.writeError(
			c,
			http.StatusConflict,
			"account_registration_unavailable",
			false,
		)
	case errors.Is(err, ErrAuthenticationRequired):
		h.writeAuthenticationRequired(c)
	case errors.Is(err, ErrPasswordUnavailable):
		c.Header("Retry-After", "1")
		h.writeError(c, http.StatusTooManyRequests, "rate_limited", true)
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
		"invalid_request":                  "Request validation failed.",
		"authentication_required":          "Authentication is required.",
		"invalid_credentials":              "Email or password is invalid.",
		"account_registration_unavailable": "Account registration is unavailable.",
		"rate_limited":                     "Too many requests. Try again later.",
		"internal_error":                   "An internal error occurred.",
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

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *HTTPHandler) decodeCredentials(c *gin.Context) (credentialsRequest, bool) {
	var request credentialsRequest
	if !validJSONContentType(c.GetHeader("Content-Type")) {
		return request, false
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(time.Now().Add(h.bodyReadTimeout)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return request, false
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxIdentityRequestBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		return credentialsRequest{}, false
	}
	if err := controller.SetReadDeadline(time.Time{}); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return credentialsRequest{}, false
	}
	if !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return credentialsRequest{}, false
	}
	return decodeCredentialObject(raw)
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

func decodeCredentialObject(raw []byte) (credentialsRequest, bool) {
	var request credentialsRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return credentialsRequest{}, false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || (key != "email" && key != "password") {
			return credentialsRequest{}, false
		}
		seen[key] = true
		var value string
		if err := decoder.Decode(&value); err != nil {
			return credentialsRequest{}, false
		}
		if key == "email" {
			request.Email = value
		} else {
			request.Password = value
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return credentialsRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return credentialsRequest{}, false
	}
	if !seen["email"] || !seen["password"] ||
		request.Email == "" || request.Password == "" {
		return credentialsRequest{}, false
	}
	return request, true
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

func userResponse(user User) gin.H {
	return gin.H{
		"user_id": user.ID,
		"email":   user.Email,
	}
}

func newCorrelationID() string {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "corr_unavailable"
	}
	return "corr_" + base64.RawURLEncoding.EncodeToString(value)
}
