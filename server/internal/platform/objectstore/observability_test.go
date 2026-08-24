package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func TestProviderErrorKindUsesOnlyStableCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want providerobservability.ErrorKind
	}{
		{name: "success", want: providerobservability.ErrorNone},
		{name: "timeout", err: context.DeadlineExceeded, want: providerobservability.ErrorTimeout},
		{name: "cancelled", err: context.Canceled, want: providerobservability.ErrorCancelled},
		{name: "credentials", err: fmt.Errorf("wrapped: %w", ErrCredentials), want: providerobservability.ErrorCredentials},
		{name: "invalid key", err: ErrInvalidKey, want: providerobservability.ErrorInvalidRequest},
		{name: "invalid TTL", err: ErrInvalidTTL, want: providerobservability.ErrorInvalidRequest},
		{name: "invalid object", err: ErrInvalidObject, want: providerobservability.ErrorInvalidObject},
		{name: "already exists", err: ErrAlreadyExists, want: providerobservability.ErrorAlreadyExists},
		{
			name: "provider detail is discarded",
			err:  errors.New("private-user@example.com provider response"),
			want: providerobservability.ErrorOperationFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProviderErrorKind(test.err); got != test.want {
				t.Fatalf("ProviderErrorKind() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestObservedReadCloserCompletesAtBodyOutcome(t *testing.T) {
	t.Run("complete read", func(t *testing.T) {
		recorder := &readObservationRecorder{}
		reader := ObserveOpenReadCloser(
			io.NopCloser(strings.NewReader("payload")),
			recorder,
			providerobservability.ProviderAliyunOSS,
			time.Now(),
		)
		if len(recorder.observations) != 0 {
			t.Fatal("opening a body recorded success before it was consumed")
		}
		data, err := io.ReadAll(reader)
		if err != nil || string(data) != "payload" {
			t.Fatalf("ReadAll() = %q, %v", data, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		observation := onlyReadObservation(t, recorder)
		if observation.ErrorKind != providerobservability.ErrorNone ||
			observation.Usage.Bytes != 7 {
			t.Fatalf("observation = %#v", observation)
		}
	})

	t.Run("read error", func(t *testing.T) {
		recorder := &readObservationRecorder{}
		reader := ObserveOpenReadCloser(
			&failingReadCloser{},
			recorder,
			providerobservability.ProviderQiniuKodo,
			time.Now(),
		)
		data, err := io.ReadAll(reader)
		if err == nil || string(data) != "bad" {
			t.Fatalf("ReadAll() = %q, %v", data, err)
		}
		_ = reader.Close()
		observation := onlyReadObservation(t, recorder)
		if observation.ErrorKind != providerobservability.ErrorOperationFailed ||
			observation.Usage.Bytes != 3 {
			t.Fatalf("observation = %#v", observation)
		}
	})

	t.Run("early close", func(t *testing.T) {
		recorder := &readObservationRecorder{}
		reader := ObserveOpenReadCloser(
			io.NopCloser(strings.NewReader("unread")),
			recorder,
			providerobservability.ProviderAliyunOSS,
			time.Now(),
		)
		if err := reader.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		observation := onlyReadObservation(t, recorder)
		if observation.ErrorKind != providerobservability.ErrorCancelled ||
			observation.Usage.Bytes != 0 {
			t.Fatalf("observation = %#v", observation)
		}
	})
}

type readObservationRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *readObservationRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

func onlyReadObservation(
	t *testing.T,
	recorder *readObservationRecorder,
) providerobservability.Observation {
	t.Helper()
	if len(recorder.observations) != 1 {
		t.Fatalf("observations = %#v", recorder.observations)
	}
	return recorder.observations[0]
}

type failingReadCloser struct {
	read bool
}

func (reader *failingReadCloser) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, errors.New("private-user@example.com provider read detail")
	}
	reader.read = true
	return copy(buffer, "bad"), nil
}

func (*failingReadCloser) Close() error { return nil }
