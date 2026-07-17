package apperror

var retryableByCode = map[Code]bool{
	CodeInvalidArgument:    false,
	CodeUnauthenticated:    false,
	CodePermissionDenied:   false,
	CodeNotFound:           false,
	CodeAlreadyExists:      false,
	CodeConflict:           false,
	CodeFailedPrecondition: false,
	CodeResourceExhausted:  true,
	CodeDeadlineExceeded:   false,
	CodeUnimplemented:      false,
	CodeUnavailable:        false,
	CodeInternal:           false,
}

func defaultRetryable(code Code) bool {
	return retryableByCode[code]
}

// CodeOf 返回 err 携带的公共错误码，未知错误安全降级为 CodeInternal。
func CodeOf(err error) Code {
	appErr, ok := As(err)
	if !ok || !IsValidCode(appErr.Code) {
		return CodeInternal
	}
	return appErr.Code
}

// ReasonOf 返回应用错误携带的稳定业务原因。
func ReasonOf(err error) string {
	appErr, ok := As(err)
	if !ok || !IsValidCode(appErr.Code) {
		return ""
	}
	return appErr.Reason
}

// IsRetryable 根据应用错误判断失败操作是否可重试，未知错误不可重试。
func IsRetryable(err error) bool {
	appErr, ok := As(err)
	return ok && IsValidCode(appErr.Code) && appErr.Retryable
}
