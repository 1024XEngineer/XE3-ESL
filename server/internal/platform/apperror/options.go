package apperror

// Option 用于在创建位置配置应用错误。
type Option func(*Error)

func WithReason(reason string) Option {
	return func(err *Error) {
		err.Reason = reason
	}
}

func WithDetail(detail string) Option {
	return func(err *Error) {
		err.Detail = detail
	}
}

func WithDetails(details map[string]any) Option {
	return func(err *Error) {
		if details == nil {
			err.Details = nil
			return
		}

		err.Details = make(map[string]any, len(details))
		for key, value := range details {
			err.Details[key] = value
		}
	}
}

func WithCause(cause error) Option {
	return func(err *Error) {
		err.Cause = cause
	}
}

func WithRetryable(retryable bool) Option {
	return func(err *Error) {
		err.Retryable = retryable
	}
}
