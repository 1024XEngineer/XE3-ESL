package objectstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
