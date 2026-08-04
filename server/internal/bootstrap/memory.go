package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	memoryPolicyVersion     = "memory-policy-v2"
	memoryPromptVersion     = "memory-extraction-v3"
	memoryExtractionLease   = 2 * time.Minute
	memoryTopicTTL          = 30 * 24 * time.Hour
	memoryExtractionRetries = 3
)

type completedAgentRunReader interface {
	Find(context.Context, string, string) (agentrun.Run, error)
}

type completedAgentMessageReader interface {
	FindMessage(
		context.Context,
		string,
		string,
		string,
	) (agentconversation.Message, error)
}

type completedAgentManifestReader interface {
	FindManifest(
		context.Context,
		string,
		string,
	) (agentcontext.Manifest, error)
}

type agentCompletedRunReader struct {
	runs      completedAgentRunReader
	messages  completedAgentMessageReader
	manifests completedAgentManifestReader
}

func newAgentCompletedRunReader(
	runs completedAgentRunReader,
	messages completedAgentMessageReader,
	manifests completedAgentManifestReader,
) (*agentCompletedRunReader, error) {
	if runs == nil || messages == nil || manifests == nil {
		return nil, errors.New(
			"bootstrap: completed Agent Run sources are required",
		)
	}
	return &agentCompletedRunReader{
		runs:      runs,
		messages:  messages,
		manifests: manifests,
	}, nil
}

func (reader *agentCompletedRunReader) ReadCompletedRun(
	ctx context.Context,
	ownerID string,
	runID string,
) (memory.CompletedRunSource, error) {
	if ctx == nil || ownerID == "" || runID == "" {
		return memory.CompletedRunSource{}, memory.ErrInvalidArgument
	}
	run, err := reader.runs.Find(ctx, ownerID, runID)
	if err != nil {
		return memory.CompletedRunSource{}, mapAgentMemorySourceError(err)
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
		return memory.CompletedRunSource{}, mapAgentMemorySourceError(err)
	}
	assistant, err := reader.messages.FindMessage(
		ctx,
		ownerID,
		run.ThreadID,
		run.AssistantMessageID,
	)
	if err != nil {
		return memory.CompletedRunSource{}, mapAgentMemorySourceError(err)
	}
	manifest, err := reader.manifests.FindManifest(ctx, ownerID, runID)
	if err != nil {
		return memory.CompletedRunSource{}, mapAgentMemorySourceError(err)
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
		return memory.CompletedRunSource{}, fmt.Errorf(
			"bootstrap: completed AgentRun projection is invalid",
		)
	}
	return source, nil
}

func buildMemoryExtractionProcessor(
	database memory.PostgreSQL,
	ids memory.IDGenerator,
	runs completedAgentRunReader,
	messages completedAgentMessageReader,
	manifests completedAgentManifestReader,
	generator ai.TextGenerator,
	runConfiguration agentrun.Configuration,
) (memory.ExtractionProcessor, error) {
	configuration := memory.ExtractionConfig{
		Provider:      runConfiguration.Provider,
		Model:         runConfiguration.Model,
		PolicyVersion: memoryPolicyVersion,
		PromptVersion: memoryPromptVersion,
		LeaseDuration: memoryExtractionLease,
		TopicTTL:      memoryTopicTTL,
		MaxAttempts:   memoryExtractionRetries,
	}
	repository, err := memory.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	sources, err := newAgentCompletedRunReader(runs, messages, manifests)
	if err != nil {
		return nil, err
	}
	extractor, err := memory.NewLLMExtractor(generator, configuration)
	if err != nil {
		return nil, err
	}
	policy, err := memory.NewExtractionPolicy(
		configuration.PolicyVersion,
		configuration.TopicTTL,
		time.Now,
	)
	if err != nil {
		return nil, err
	}
	return memory.NewWorker(
		repository,
		sources,
		extractor,
		policy,
		configuration,
	)
}

func mapAgentMemorySourceError(err error) error {
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
		return fmt.Errorf(
			"bootstrap: read completed AgentRun source: %w",
			err,
		)
	}
}

var _ memory.CompletedRunReader = (*agentCompletedRunReader)(nil)
