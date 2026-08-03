package postgres

import (
	"errors"
	"testing"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestNewImageSubmissionRepositoryRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewImageSubmissionRepository(nil, nil); !errors.Is(
		err,
		agentrun.ErrRepository,
	) {
		t.Fatalf("NewImageSubmissionRepository error = %v", err)
	}
}
