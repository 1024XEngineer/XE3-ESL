package postgres

import (
	"errors"
	"testing"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
)

func TestNewRequiresPool(t *testing.T) {
	t.Parallel()

	if _, err := New(nil); !errors.Is(
		err,
		agentimage.ErrRepository,
	) {
		t.Fatalf("New error = %v", err)
	}
}
