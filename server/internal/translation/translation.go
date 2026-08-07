package translation

import "context"

const TargetLanguage = "zh-CN"

type Request struct {
	Text string
}

type Translator interface {
	Translate(context.Context, Request) (string, error)
}

type ProviderErrorKind string

const (
	ProviderErrorInvalidRequest  ProviderErrorKind = "invalid_request"
	ProviderErrorConfiguration   ProviderErrorKind = "configuration"
	ProviderErrorAuthentication  ProviderErrorKind = "authentication"
	ProviderErrorAuthorization   ProviderErrorKind = "authorization"
	ProviderErrorQuotaExhausted  ProviderErrorKind = "quota_exhausted"
	ProviderErrorRateLimited     ProviderErrorKind = "rate_limited"
	ProviderErrorTimeout         ProviderErrorKind = "timeout"
	ProviderErrorUnavailable     ProviderErrorKind = "provider_unavailable"
	ProviderErrorInvalidResponse ProviderErrorKind = "invalid_response"
	ProviderErrorCancelled       ProviderErrorKind = "cancelled"
)

func (kind ProviderErrorKind) Retryable() bool {
	switch kind {
	case ProviderErrorRateLimited,
		ProviderErrorTimeout,
		ProviderErrorUnavailable,
		ProviderErrorInvalidResponse,
		ProviderErrorCancelled:
		return true
	default:
		return false
	}
}

type ProviderError struct {
	Kind      ProviderErrorKind
	RequestID string
	cause     error
}

func NewProviderError(
	kind ProviderErrorKind,
	requestID string,
	cause error,
) *ProviderError {
	return &ProviderError{Kind: kind, RequestID: requestID, cause: cause}
}

func (err *ProviderError) Error() string {
	if err == nil {
		return "translation provider failed"
	}
	return "translation provider failed: " + string(err.Kind)
}

func (err *ProviderError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *ProviderError) Retryable() bool {
	return err != nil && err.Kind.Retryable()
}
