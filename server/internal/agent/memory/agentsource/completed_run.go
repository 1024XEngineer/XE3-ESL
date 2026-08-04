package agentsource

import (
	"context"
	"errors"
	"fmt"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

type runReader interface {
	Find(context.Context, string, string) (agentrun.Run, error)
}

type messageReader interface {
	FindMessage(
		context.Context,
		string,
		string,
		string,
	) (agentconversation.Message, error)
}

type manifestReader interface {
	FindManifest(
		context.Context,
		string,
		string,
	) (agentcontext.Manifest, error)
}

type CompletedRunReader struct {
	runs      runReader
	messages  messageReader
	manifests manifestReader
}

func NewCompletedRunReader(
	runs runReader,
	messages messageReader,
	manifests manifestReader,
) (*CompletedRunReader, error) {
	if runs == nil || messages == nil || manifests == nil {
		return nil, errors.New("memory Agent Run sources are required")
	}
	return &CompletedRunReader{
		runs:      runs,
		messages:  messages,
		manifests: manifests,
	}, nil
}

func (reader *CompletedRunReader) ReadCompletedRun(
	ctx context.Context,
	ownerID string,
	runID string,
) (memory.CompletedRunSource, error) {
	if ctx == nil || ownerID == "" || runID == "" {
		return memory.CompletedRunSource{}, memory.ErrInvalidArgument
	}
	run, err := reader.runs.Find(ctx, ownerID, runID)
	if err != nil {
		return memory.CompletedRunSource{}, mapSourceError(err)
	}
	if run.OwnerID != ownerID ||
		run.ID != runID ||
		run.Status != agentrun.StatusCompleted {
		return memory.CompletedRunSource{}, memory.ErrNotFound
	}
	input, err := reader.messages.FindMessage(
		ctx,
		ownerID,
		run.ThreadID,
		run.InputMessageID,
	)
	if err != nil {
		return memory.CompletedRunSource{}, mapSourceError(err)
	}
	assistant, err := reader.messages.FindMessage(
		ctx,
		ownerID,
		run.ThreadID,
		run.AssistantMessageID,
	)
	if err != nil {
		return memory.CompletedRunSource{}, mapSourceError(err)
	}
	manifest, err := reader.manifests.FindManifest(ctx, ownerID, runID)
	if err != nil {
		return memory.CompletedRunSource{}, mapSourceError(err)
	}
	source := memory.CompletedRunSource{
		OwnerID:            ownerID,
		RunID:              run.ID,
		ThreadID:           run.ThreadID,
		InputMessageID:     input.ID,
		AssistantMessageID: assistant.ID,
		GoalID:             manifest.ActiveGoalID,
		UserText:           input.Content,
		AssistantText:      assistant.Content,
		Attempt:            run.Attempt,
		CompletedAt:        run.CompletedAt,
	}
	if !source.Valid() {
		return memory.CompletedRunSource{}, memory.ErrRepository
	}
	return source, nil
}

func mapSourceError(err error) error {
	switch {
	case errors.Is(err, agentrun.ErrNotFound),
		errors.Is(err, agentconversation.ErrNotFound),
		errors.Is(err, agentcontext.ErrNotFound):
		return memory.ErrNotFound
	case errors.Is(err, agentrun.ErrInvalidRequest),
		errors.Is(err, agentconversation.ErrInvalidRequest):
		return memory.ErrInvalidArgument
	case errors.Is(err, agentrun.ErrConflict),
		errors.Is(err, agentconversation.ErrConflict),
		errors.Is(err, agentcontext.ErrConflict):
		return memory.ErrConflict
	default:
		return fmt.Errorf("read completed Agent Run source: %w", err)
	}
}

var _ memory.CompletedRunReader = (*CompletedRunReader)(nil)
