package bootstrap

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

type runCompletionNotifyingRepository struct {
	core.RunRepository
	notifiers []interface{ Notify() }
}

func (repository *runCompletionNotifyingRepository) CompleteRun(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	content string,
	result ai.TextResult,
) (core.Run, error) {
	run, err := repository.RunRepository.CompleteRun(
		ctx,
		ownerID,
		runID,
		workerLeaseToken,
		content,
		result,
	)
	if err == nil && run.Status == core.RunStatusCompleted {
		for _, notifier := range repository.notifiers {
			if notifier != nil {
				notifier.Notify()
			}
		}
	}
	return run, err
}
