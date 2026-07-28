package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

func TestRunCompletionNotifierFiresOnlyAfterSuccessfulCommit(t *testing.T) {
	t.Parallel()

	completedRun := core.Run{Status: core.RunStatusCompleted}
	underlying := &completionRepositoryStub{
		complete: func() (core.Run, error) {
			return completedRun, nil
		},
	}
	notifier := &countingNotifier{}
	repository := &runCompletionNotifyingRepository{
		RunRepository: underlying,
		notifier:      notifier,
	}
	if _, err := repository.CompleteRun(
		context.Background(),
		"owner",
		"run",
		"lease",
		"content",
		ai.TextResult{},
	); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", notifier.calls)
	}

	underlying.complete = func() (core.Run, error) {
		return core.Run{}, errors.New("commit failed")
	}
	if _, err := repository.CompleteRun(
		context.Background(),
		"owner",
		"run",
		"lease",
		"content",
		ai.TextResult{},
	); err == nil {
		t.Fatal("CompleteRun error = nil")
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier fired before commit: %d calls", notifier.calls)
	}
}

type completionRepositoryStub struct {
	core.RunRepository
	complete func() (core.Run, error)
}

func (repository *completionRepositoryStub) CompleteRun(
	context.Context,
	string,
	string,
	string,
	string,
	ai.TextResult,
) (core.Run, error) {
	return repository.complete()
}

type countingNotifier struct {
	calls int
}

func (notifier *countingNotifier) Notify() {
	notifier.calls++
}
