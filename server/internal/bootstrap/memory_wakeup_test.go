package bootstrap

import (
	"context"
	"errors"
	"testing"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestRunCompletionNotifierFiresOnlyAfterSuccessfulCommit(t *testing.T) {
	t.Parallel()

	completedRun := agentrun.Run{Status: agentrun.StatusCompleted}
	underlying := &completionRepositoryStub{
		complete: func() (agentrun.Run, error) {
			return completedRun, nil
		},
	}
	notifier := &countingNotifier{}
	repository := &runCompletionNotifyingRepository{
		Repository: underlying,
		notifiers:  []interface{ Notify() }{notifier},
	}
	if _, err := repository.Complete(
		context.Background(),
		"owner",
		"run",
		"lease",
		agentrun.Completion{},
	); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", notifier.calls)
	}

	underlying.complete = func() (agentrun.Run, error) {
		return agentrun.Run{}, errors.New("commit failed")
	}
	if _, err := repository.Complete(
		context.Background(),
		"owner",
		"run",
		"lease",
		agentrun.Completion{},
	); err == nil {
		t.Fatal("CompleteRun error = nil")
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier fired before commit: %d calls", notifier.calls)
	}
}

func TestRunCompletionNotifierFansOutPayloadFreeWakeups(t *testing.T) {
	t.Parallel()

	first := &countingNotifier{}
	second := &countingNotifier{}
	repository := &runCompletionNotifyingRepository{
		Repository: &completionRepositoryStub{
			complete: func() (agentrun.Run, error) {
				return agentrun.Run{Status: agentrun.StatusCompleted}, nil
			},
		},
		notifiers: []interface{ Notify() }{first, second},
	}
	if _, err := repository.Complete(
		context.Background(),
		"owner",
		"run",
		"lease",
		agentrun.Completion{},
	); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf(
			"notifier calls = %d/%d, want 1/1",
			first.calls,
			second.calls,
		)
	}
}

type completionRepositoryStub struct {
	agentrun.Repository
	complete func() (agentrun.Run, error)
}

func (repository *completionRepositoryStub) Complete(
	context.Context,
	string,
	string,
	string,
	agentrun.Completion,
) (agentrun.Run, error) {
	return repository.complete()
}

type countingNotifier struct {
	calls int
}

func (notifier *countingNotifier) Notify() {
	notifier.calls++
}
