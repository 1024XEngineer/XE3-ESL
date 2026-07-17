package httpresponse

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
)

const (
	internalReason  = "service_internal_error"
	internalMessage = "an internal service error occurred"
)

var statusByCode = map[apperror.Code]int{
	apperror.CodeInvalidArgument:    http.StatusBadRequest,
	apperror.CodeUnauthenticated:    http.StatusUnauthorized,
	apperror.CodePermissionDenied:   http.StatusForbidden,
	apperror.CodeNotFound:           http.StatusNotFound,
	apperror.CodeAlreadyExists:      http.StatusConflict,
	apperror.CodeConflict:           http.StatusConflict,
	apperror.CodeFailedPrecondition: http.StatusConflict,
	apperror.CodeResourceExhausted:  http.StatusTooManyRequests,
	apperror.CodeDeadlineExceeded:   http.StatusGatewayTimeout,
	apperror.CodeUnimplemented:      http.StatusNotImplemented,
	apperror.CodeUnavailable:        http.StatusServiceUnavailable,
	apperror.CodeInternal:           http.StatusInternalServerError,
}

var defaultMessageByCode = map[apperror.Code]string{
	apperror.CodeInvalidArgument:    "invalid request",
	apperror.CodeUnauthenticated:    "authentication required",
	apperror.CodePermissionDenied:   "permission denied",
	apperror.CodeNotFound:           "resource not found",
	apperror.CodeAlreadyExists:      "resource already exists",
	apperror.CodeConflict:           "request conflicts with current state",
	apperror.CodeFailedPrecondition: "operation is not allowed in the current state",
	apperror.CodeResourceExhausted:  "resource limit exceeded",
	apperror.CodeDeadlineExceeded:   "request deadline exceeded",
	apperror.CodeUnimplemented:      "operation is not implemented",
	apperror.CodeUnavailable:        "service temporarily unavailable",
	apperror.CodeInternal:           internalMessage,
}

// ErrorEnvelope 是稳定的 REST 顶层错误响应。
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody 包含可安全返回客户端的应用错误字段。
type ErrorBody struct {
	Code      apperror.Code  `json:"code"`
	Reason    string         `json:"reason,omitempty"`
	Message   string         `json:"message"`
	Detail    string         `json:"detail,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
	RequestID string         `json:"request_id,omitempty"`
}

// Render 将任意错误转换为 HTTP 状态码和客户端安全的错误响应。
func Render(err error, requestID string) (int, ErrorEnvelope) {
	appErr, ok := apperror.As(err)
	if !ok || !apperror.IsValidCode(appErr.Code) {
		return internalResponse(requestID)
	}

	status, ok := statusByCode[appErr.Code]
	if !ok {
		return internalResponse(requestID)
	}

	message := appErr.Message
	if message == "" {
		message = defaultMessageByCode[appErr.Code]
	}

	reason := appErr.Reason
	if appErr.Code == apperror.CodeInternal && reason == "" {
		reason = internalReason
	}

	return status, ErrorEnvelope{
		Error: ErrorBody{
			Code:      appErr.Code,
			Reason:    reason,
			Message:   message,
			Detail:    appErr.Detail,
			Details:   copyDetails(appErr.Details),
			Retryable: appErr.Retryable,
			RequestID: requestID,
		},
	}
}

// Write 渲染 err 并通过 Gin 写入响应。
func Write(c *gin.Context, err error, requestID string) {
	status, envelope := Render(err, requestID)
	c.JSON(status, envelope)
}

func internalResponse(requestID string) (int, ErrorEnvelope) {
	return http.StatusInternalServerError, ErrorEnvelope{
		Error: ErrorBody{
			Code:      apperror.CodeInternal,
			Reason:    internalReason,
			Message:   internalMessage,
			Retryable: false,
			RequestID: requestID,
		},
	}
}

func copyDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	copied := make(map[string]any, len(details))
	for key, value := range details {
		copied[key] = value
	}
	return copied
}
