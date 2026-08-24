package objectstore

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

// RecordProviderCall records one sanitized object-provider operation.
func RecordProviderCall(
	observer providerobservability.Recorder,
	provider providerobservability.Provider,
	capability providerobservability.Capability,
	startedAt time.Time,
	err error,
	bytes int64,
) {
	if observer == nil {
		return
	}
	observer.Record(providerobservability.Observation{
		Provider:   provider,
		Capability: capability,
		Duration:   time.Since(startedAt),
		ErrorKind:  ProviderErrorKind(err),
		Usage:      providerobservability.Usage{Bytes: float64(bytes)},
	})
}

// ProviderErrorKind maps object-storage errors to a fixed metric category.
func ProviderErrorKind(err error) providerobservability.ErrorKind {
	switch {
	case err == nil:
		return providerobservability.ErrorNone
	case errors.Is(err, context.DeadlineExceeded):
		return providerobservability.ErrorTimeout
	case errors.Is(err, context.Canceled):
		return providerobservability.ErrorCancelled
	case errors.Is(err, ErrCredentials):
		return providerobservability.ErrorCredentials
	case errors.Is(err, ErrInvalidKey), errors.Is(err, ErrInvalidTTL):
		return providerobservability.ErrorInvalidRequest
	case errors.Is(err, ErrInvalidObject):
		return providerobservability.ErrorInvalidObject
	case errors.Is(err, ErrAlreadyExists):
		return providerobservability.ErrorAlreadyExists
	default:
		return providerobservability.ErrorOperationFailed
	}
}
