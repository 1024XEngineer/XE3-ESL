package spatius

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func recordSessionTokenCall(
	recorder providerobservability.Recorder,
	startedAt time.Time,
	kind providerobservability.ErrorKind,
) {
	if recorder == nil {
		return
	}
	recorder.Record(providerobservability.Observation{
		Provider:   providerobservability.ProviderSpatius,
		Capability: providerobservability.CapabilityAvatarSessionToken,
		Duration:   time.Since(startedAt),
		ErrorKind:  kind,
	})
}

func sessionTokenTransportKind(
	ctx context.Context,
	err error,
) providerobservability.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled), ctx != nil && errors.Is(ctx.Err(), context.Canceled):
		return providerobservability.ErrorCancelled
	case errors.Is(err, context.DeadlineExceeded), ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return providerobservability.ErrorTimeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return providerobservability.ErrorTimeout
	}
	return providerobservability.ErrorProviderUnavailable
}

func sessionTokenStatusKind(status int) providerobservability.ErrorKind {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return providerobservability.ErrorNone
	case status >= http.StatusMultipleChoices && status < http.StatusBadRequest:
		return providerobservability.ErrorInvalidResponse
	case status == http.StatusBadRequest:
		return providerobservability.ErrorInvalidRequest
	case status == http.StatusUnauthorized:
		return providerobservability.ErrorAuthentication
	case status == http.StatusPaymentRequired:
		return providerobservability.ErrorQuotaExhausted
	case status == http.StatusForbidden:
		return providerobservability.ErrorAuthorization
	case status == http.StatusTooManyRequests:
		return providerobservability.ErrorRateLimited
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		return providerobservability.ErrorInvalidRequest
	default:
		return providerobservability.ErrorProviderUnavailable
	}
}
