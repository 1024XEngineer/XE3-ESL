package postgres

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestNewRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, nil); !errors.Is(err, agentvoice.ErrRepository) {
		t.Fatalf("New error = %v, want repository error", err)
	}
}

func TestCrossResourceTransactionErrorsRemainVoiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  error
		want error
	}{
		{
			name: "conversation not found",
			got:  mapConversationTransactionError(conversation.ErrNotFound),
			want: agentvoice.ErrNotFound,
		},
		{
			name: "conversation idempotency conflict",
			got: mapConversationTransactionError(
				conversation.ErrIdempotencyConflict,
			),
			want: agentvoice.ErrIdempotencyConflict,
		},
		{
			name: "run invalid request",
			got:  mapRunTransactionError(agentrun.ErrInvalidRequest),
			want: agentvoice.ErrInvalidRequest,
		},
		{
			name: "run conflict",
			got:  mapRunTransactionError(agentrun.ErrConflict),
			want: agentvoice.ErrConflict,
		},
		{
			name: "run repository failure",
			got:  mapRunTransactionError(agentrun.ErrRepository),
			want: agentvoice.ErrRepository,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(test.got, test.want) {
				t.Fatalf("mapped error = %v, want %v", test.got, test.want)
			}
		})
	}
}
