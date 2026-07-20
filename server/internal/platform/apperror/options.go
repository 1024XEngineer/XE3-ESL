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
		err.Details = cloneDetails(details)
	}
}

func cloneDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}

	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = cloneDetailValue(value)
	}
	return cloned
}

func cloneDetailValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneDetails(value)
	case []any:
		cloned := make([]any, len(value))
		for index, item := range value {
			cloned[index] = cloneDetailValue(item)
		}
		return cloned
	default:
		return value
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
