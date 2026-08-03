// Package httpresponse renders application errors for the REST API.
package httpresponse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
)

const (
	internalErrorCode    = "internal_error"
	internalErrorMessage = "Internal server error."
)

type correlationIDContextKey struct{}

// ErrorResponse is the canonical REST error response.
type ErrorResponse struct {
	Error ErrorPayload `json:"error"`
}

// ErrorPayload contains only fields declared by api/common/errors.yaml.
type ErrorPayload struct {
	Code          string        `json:"code"`
	Message       string        `json:"message"`
	Retryable     bool          `json:"retryable"`
	CorrelationID string        `json:"correlation_id"`
	Details       []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail is a sanitized field-specific REST error detail.
type ErrorDetail struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// CorrelationIDGenerator supplies correlation IDs until ERR-2 introduces
// centralized middleware.
type CorrelationIDGenerator func() string

// Renderer converts application errors into canonical REST responses.
type Renderer struct {
	generateCorrelationID CorrelationIDGenerator
}

// NewRenderer creates a REST error renderer. A nil generator uses the package's
// non-empty default generator.
func NewRenderer(generator CorrelationIDGenerator) *Renderer {
	if generator == nil {
		generator = newCorrelationID
	}
	return &Renderer{generateCorrelationID: generator}
}

// WithCorrelationID stores a request-scoped correlation ID in ctx. ERR-2 can
// use this boundary when centralized request middleware is introduced.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDContextKey{}, correlationID)
}

// CorrelationIDFromContext returns a valid request-scoped correlation ID.
func CorrelationIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	correlationID, ok := ctx.Value(correlationIDContextKey{}).(string)
	return correlationID, ok && validPublicText(correlationID)
}

// Write renders err without exposing its internal cause or raw error string.
func (renderer *Renderer) Write(c *gin.Context, err error) {
	status, payload := payloadFor(err)
	payload.CorrelationID = renderer.correlationID(c.Request.Context())
	c.JSON(status, ErrorResponse{Error: payload})
}

func (renderer *Renderer) correlationID(ctx context.Context) string {
	if correlationID, ok := CorrelationIDFromContext(ctx); ok {
		return correlationID
	}

	if renderer != nil && renderer.generateCorrelationID != nil {
		if correlationID := renderer.generateCorrelationID(); validPublicText(correlationID) {
			return correlationID
		}
	}
	return newCorrelationID()
}

func payloadFor(err error) (int, ErrorPayload) {
	appError, ok := apperror.From(err)
	if !ok {
		return internalPayload()
	}

	status, ok := statusForCategory(appError.Category())
	contractStatus, codeOK := canonicalHTTPStatusByCode[appError.Code()]
	if !ok ||
		!codeOK ||
		contractStatus != status ||
		!validPublicText(appError.Message()) {
		return internalPayload()
	}

	details, ok := responseDetails(appError.Details())
	if !ok {
		return internalPayload()
	}

	return status, ErrorPayload{
		Code:      appError.Code(),
		Message:   appError.Message(),
		Retryable: appError.Retryable(),
		Details:   details,
	}
}

func internalPayload() (int, ErrorPayload) {
	return http.StatusInternalServerError, ErrorPayload{
		Code:      internalErrorCode,
		Message:   internalErrorMessage,
		Retryable: false,
	}
}

func statusForCategory(category apperror.Category) (int, bool) {
	switch category {
	case apperror.InvalidArgument:
		return http.StatusBadRequest, true
	case apperror.PayloadTooLarge:
		return http.StatusRequestEntityTooLarge, true
	case apperror.Unauthenticated:
		return http.StatusUnauthorized, true
	case apperror.PermissionDenied:
		return http.StatusForbidden, true
	case apperror.NotFound:
		return http.StatusNotFound, true
	case apperror.AlreadyExists:
		return http.StatusConflict, true
	case apperror.Conflict:
		return http.StatusConflict, true
	case apperror.FailedPrecondition:
		return http.StatusPreconditionFailed, true
	case apperror.UnprocessableEntity:
		return http.StatusUnprocessableEntity, true
	case apperror.ResourceExhausted:
		return http.StatusTooManyRequests, true
	case apperror.DeadlineExceeded:
		return http.StatusGatewayTimeout, true
	case apperror.Unimplemented:
		return http.StatusNotImplemented, true
	case apperror.Unavailable:
		return http.StatusServiceUnavailable, true
	case apperror.Internal:
		return http.StatusInternalServerError, true
	default:
		return 0, false
	}
}

// canonicalHTTPStatusByCode mirrors ErrorCode.x-http-status-map in
// api/common/errors.yaml for delivery-time validation. Business modules still
// own and select their error codes; this snapshot only prevents the REST
// renderer from emitting codes or code/status combinations outside the public
// API contract. A regression test keeps the snapshot synchronized with the
// schema.
var canonicalHTTPStatusByCode = map[string]int{
	"invalid_request":                         http.StatusBadRequest,
	"invalid_image":                           http.StatusBadRequest,
	"unsupported_image_format":                http.StatusBadRequest,
	"image_too_large":                         http.StatusRequestEntityTooLarge,
	"answer_invalid":                          http.StatusBadRequest,
	"unsupported_message":                     http.StatusBadRequest,
	"authentication_required":                 http.StatusUnauthorized,
	"confirmation_required":                   http.StatusConflict,
	"version_conflict":                        http.StatusConflict,
	"active_session_conflict":                 http.StatusConflict,
	"invalid_credentials":                     http.StatusUnauthorized,
	"practice_participant_not_authorized":     http.StatusForbidden,
	"scenario_definition_not_found":           http.StatusNotFound,
	"role_definition_not_found":               http.StatusNotFound,
	"preparation_profile_not_found":           http.StatusNotFound,
	"job_target_not_found":                    http.StatusNotFound,
	"practice_plan_not_found":                 http.StatusNotFound,
	"practice_session_not_found":              http.StatusNotFound,
	"question_not_found":                      http.StatusNotFound,
	"turn_not_found":                          http.StatusNotFound,
	"turn_analysis_not_found":                 http.StatusNotFound,
	"feedback_item_not_found":                 http.StatusNotFound,
	"retry_request_not_found":                 http.StatusNotFound,
	"evaluation_not_found":                    http.StatusNotFound,
	"resource_not_found":                      http.StatusNotFound,
	"profile_not_found":                       http.StatusNotFound,
	"account_registration_unavailable":        http.StatusConflict,
	"profile_version_conflict":                http.StatusConflict,
	"resume_not_found":                        http.StatusNotFound,
	"resume_limit_exceeded":                   http.StatusConflict,
	"resume_version_conflict":                 http.StatusConflict,
	"unsupported_resume_format":               http.StatusBadRequest,
	"resume_file_too_large":                   http.StatusBadRequest,
	"resume_parse_failed":                     http.StatusServiceUnavailable,
	"resume_not_implemented":                  http.StatusNotImplemented,
	"idempotency_key_conflict":                http.StatusConflict,
	"resource_conflict":                       http.StatusConflict,
	"resource_processing":                     http.StatusConflict,
	"preparation_version_conflict":            http.StatusConflict,
	"job_target_version_conflict":             http.StatusConflict,
	"job_target_analysis_claim_lost":          http.StatusConflict,
	"practice_plan_not_ready":                 http.StatusConflict,
	"practice_plan_archived":                  http.StatusConflict,
	"practice_plan_has_active_session":        http.StatusConflict,
	"practice_plan_revision_conflict":         http.StatusConflict,
	"practice_session_transition_not_allowed": http.StatusConflict,
	"practice_session_already_terminal":       http.StatusConflict,
	"practice_session_version_conflict":       http.StatusConflict,
	"practice_participant_invalid":            http.StatusConflict,
	"practice_option_invalid":                 http.StatusConflict,
	"turn_conflict":                           http.StatusConflict,
	"turn_outcome_session_mismatch":           http.StatusConflict,
	"turn_analysis_conflict":                  http.StatusConflict,
	"retry_request_conflict":                  http.StatusConflict,
	"evaluation_version_conflict":             http.StatusConflict,
	"transcript_unavailable":                  http.StatusUnprocessableEntity,
	"evaluation_strategy_not_available":       http.StatusUnprocessableEntity,
	"evaluation_policy_violation":             http.StatusUnprocessableEntity,
	"rate_limited":                            http.StatusTooManyRequests,
	"provider_unavailable":                    http.StatusServiceUnavailable,
	"quota_exhausted":                         http.StatusServiceUnavailable,
	"voice_capacity_exhausted":                http.StatusServiceUnavailable,
	"job_target_analysis_failed":              http.StatusServiceUnavailable,
	"evaluation_retryable_failure":            http.StatusServiceUnavailable,
	internalErrorCode:                         http.StatusInternalServerError,
}

func responseDetails(details []apperror.Detail) ([]ErrorDetail, bool) {
	if len(details) == 0 {
		return nil, true
	}

	response := make([]ErrorDetail, len(details))
	for index, detail := range details {
		if !validPublicText(detail.Field) || !validPublicText(detail.Reason) {
			return nil, false
		}
		response[index] = ErrorDetail{
			Field:  detail.Field,
			Reason: detail.Reason,
		}
	}
	return response, true
}

func validPublicText(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

var fallbackCorrelationSequence atomic.Uint64

func newCorrelationID() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err == nil {
		return "corr_" + hex.EncodeToString(random)
	}

	return fmt.Sprintf(
		"corr_%x_%x",
		time.Now().UnixNano(),
		fallbackCorrelationSequence.Add(1),
	)
}
