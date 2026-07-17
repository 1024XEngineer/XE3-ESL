package apperror

import (
	"errors"
	"fmt"
)

// Error 描述与交付协议无关的应用失败。
// Cause 仅供内部错误链和日志使用，禁止序列化后返回客户端。
type Error struct {
	Code      Code
	Reason    string
	Message   string
	Detail    string
	Details   map[string]any
	Retryable bool
	Cause     error
}

// New 创建应用错误。
// 非法错误码会安全降级为内部错误，且不会保留调用方提供的公开字段。
func New(code Code, message string, options ...Option) *Error {
	invalidCode := !IsValidCode(code)
	if invalidCode {
		code = CodeInternal
		message = ""
	}

	appErr := &Error{
		Code:      code,
		Message:   message,
		Retryable: defaultRetryable(code),
	}
	for _, option := range options {
		if option != nil {
			option(appErr)
		}
	}
	if invalidCode {
		appErr.Reason = ""
		appErr.Detail = ""
		appErr.Details = nil
		appErr.Retryable = false
	}
	return appErr
}

// Error 返回安全的公开错误上下文，并且不会包含 Cause。
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 向 Go 错误链操作和内部日志开放 Cause。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// As 从错误链中提取应用错误。
func As(err error) (*Error, bool) {
	var appErr *Error
	if !errors.As(err, &appErr) || appErr == nil {
		return nil, false
	}
	return appErr, true
}

func InvalidArgument(message string, options ...Option) *Error {
	return New(CodeInvalidArgument, message, options...)
}

func NotFound(message string, options ...Option) *Error {
	return New(CodeNotFound, message, options...)
}

func FailedPrecondition(message string, options ...Option) *Error {
	return New(CodeFailedPrecondition, message, options...)
}

func Unavailable(message string, options ...Option) *Error {
	return New(CodeUnavailable, message, options...)
}

func Internal(options ...Option) *Error {
	return New(CodeInternal, "", options...)
}
