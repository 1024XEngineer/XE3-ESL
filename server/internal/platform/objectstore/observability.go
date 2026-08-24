package objectstore

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
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

// ObserveOpenReadCloser completes an object-open observation only when the
// response body reaches EOF, fails while reading, or is closed early.
func ObserveOpenReadCloser(
	body io.ReadCloser,
	observer providerobservability.Recorder,
	provider providerobservability.Provider,
	startedAt time.Time,
) io.ReadCloser {
	if observer == nil {
		return body
	}
	return &observedReadCloser{
		body: body, observer: observer, provider: provider, startedAt: startedAt,
	}
}

type observedReadCloser struct {
	body      io.ReadCloser
	observer  providerobservability.Recorder
	provider  providerobservability.Provider
	startedAt time.Time
	bytesRead atomic.Int64
	finished  sync.Once
}

func (reader *observedReadCloser) Read(buffer []byte) (int, error) {
	count, err := reader.body.Read(buffer)
	if count > 0 {
		reader.bytesRead.Add(int64(count))
	}
	if errors.Is(err, io.EOF) {
		reader.finish(nil)
	} else if err != nil {
		reader.finish(err)
	}
	return count, err
}

func (reader *observedReadCloser) Close() error {
	err := reader.body.Close()
	if err != nil {
		reader.finish(err)
	} else {
		reader.finish(context.Canceled)
	}
	return err
}

func (reader *observedReadCloser) finish(err error) {
	reader.finished.Do(func() {
		RecordProviderCall(
			reader.observer,
			reader.provider,
			providerobservability.CapabilityObjectOpen,
			reader.startedAt,
			err,
			reader.bytesRead.Load(),
		)
	})
}

var _ io.ReadCloser = (*observedReadCloser)(nil)

// ProviderErrorKind maps object-storage errors to a fixed metric category.
func ProviderErrorKind(err error) providerobservability.ErrorKind {
	switch {
	case err == nil:
		return providerobservability.ErrorNone
	case errors.Is(err, context.DeadlineExceeded):
		return providerobservability.ErrorTimeout
	case errors.Is(err, context.Canceled):
		return providerobservability.ErrorCancelled
	}
	var stableError interface {
		ProviderMetricErrorKind() providerobservability.ErrorKind
	}
	if errors.As(err, &stableError) {
		return stableError.ProviderMetricErrorKind()
	}
	switch {
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
