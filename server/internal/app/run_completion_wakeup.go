package app

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
	output agentrun.AssistantOutput,
	result agentrun.TextResult,
) (agentrun.Run, error) {
	run, err := repository.Repository.Complete(
		ctx,
		ownerID,
		runID,
		workerLeaseToken,
		output,
		result,
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
