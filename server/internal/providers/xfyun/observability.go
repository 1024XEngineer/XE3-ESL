package xfyun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func recordISECall(
	recorder providerobservability.Recorder,
	startedAt time.Time,
	kind providerobservability.ErrorKind,
) {
	if recorder == nil {
		return
	}
	recorder.Record(providerobservability.Observation{
		Provider:   providerobservability.ProviderXFYunISE,
		Capability: providerobservability.CapabilitySpeechEvaluation,
		Duration:   time.Since(startedAt),
		ErrorKind:  kind,
	})
}

func iseTransportErrorKind(
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
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &syntaxError) || errors.As(err, &typeError) {
		return providerobservability.ErrorInvalidResponse
	}
	return providerobservability.ErrorProviderUnavailable
}

func iseHandshakeErrorKind(
	ctx context.Context,
	response *http.Response,
	err error,
) providerobservability.ErrorKind {
	if kind := iseTransportErrorKind(ctx, err); kind != providerobservability.ErrorProviderUnavailable {
		return kind
	}
	if response == nil {
		return providerobservability.ErrorProviderUnavailable
	}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return providerobservability.ErrorAuthentication
	case http.StatusForbidden:
		return providerobservability.ErrorAuthorization
	default:
		return providerobservability.ErrorProviderUnavailable
	}
}

func iseProviderCodeKind(code int) providerobservability.ErrorKind {
	switch code {
	case 10313, 10105:
		return providerobservability.ErrorAuthentication
	case 11200:
		return providerobservability.ErrorAuthorization
	case 11201, 42306:
		return providerobservability.ErrorQuotaExhausted
	case 10114, 10200:
		return providerobservability.ErrorTimeout
	case 10163, 40007, 10043, 10161, 10160, 60114, 10139, 48196,
		40006, 40017, 40023, 40034, 40037, 40038, 40040, 68676,
		30002, 48195, 30011, 68675, 48205:
		return providerobservability.ErrorInvalidRequest
	default:
		return providerobservability.ErrorProviderUnavailable
	}
}
