package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const maxIdentityRequestBody = 4096

type CorrelationIDGenerator func() string

type HTTPHandler struct {
	application   Application
	authenticator Authenticator
	rateLimits    RateLimiters
	correlationID CorrelationIDGenerator
}

func NewHTTPHandler(
	application Application,
	authenticator Authenticator,
	rateLimits RateLimiters,
	correlationID CorrelationIDGenerator,
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
	return &HTTPHandler{
		application:   application,
		authenticator: authenticator,
		rateLimits:    rateLimits,
		correlationID: correlationID,
	}, nil
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
		"register-ip:"+remoteIP(c.Request),
	) {
		return
	}
	request, ok := decodeCredentials(c)
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
	sourceIP := remoteIP(c.Request)
	if !h.enforceLimit(c, h.rateLimits.LoginIP, "login-ip:"+sourceIP) {
		return
	}
	request, ok := decodeCredentials(c)
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
		"expires_at":    result.ExpiresAt.UTC().Format(time.RFC3339),
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

func decodeCredentials(c *gin.Context) (credentialsRequest, bool) {
	var request credentialsRequest
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxIdentityRequestBody)
	raw, err := io.ReadAll(body)
	if err != nil || !utf8.Valid(raw) {
		return credentialsRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return credentialsRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return credentialsRequest{}, false
	}
	if request.Email == "" || request.Password == "" {
		return credentialsRequest{}, false
	}
	return request, true
}

func userResponse(user User) gin.H {
	return gin.H{
		"user_id": user.ID,
		"email":   user.Email,
	}
}

func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func newCorrelationID() string {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "corr_unavailable"
	}
	return "corr_" + base64.RawURLEncoding.EncodeToString(value)
}
