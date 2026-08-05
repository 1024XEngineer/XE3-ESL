package bootstrap

import (
	"context"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

type runCompletionNotifyingRepository struct {
	agentrun.Repository
	notifiers []interface{ Notify() }
}

func (repository *runCompletionNotifyingRepository) Complete(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	completion agentrun.Completion,
) (agentrun.Run, error) {
	run, err := repository.Repository.Complete(
		ctx,
		ownerID,
		runID,
		workerLeaseToken,
		completion,
	)
	if err == nil && run.Status == agentrun.StatusCompleted {
		for _, notifier := range repository.notifiers {
			if notifier != nil {
				notifier.Notify()
			}
		}
	}
	return run, err
}
