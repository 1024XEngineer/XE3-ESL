package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

func TestAgentCompletedRunReaderProjectsOwnedSource(t *testing.T) {
	t.Parallel()

	completedAt := time.Now().UTC()
	repository := &fakeCompletedAgentRunRepository{
		run: agentrun.Run{
			ID:                 "10000000-0000-4000-8000-000000000001",
			OwnerID:            "20000000-0000-4000-8000-000000000001",
			ThreadID:           "30000000-0000-4000-8000-000000000001",
			InputMessageID:     "40000000-0000-4000-8000-000000000001",
			AssistantMessageID: "50000000-0000-4000-8000-000000000001",
			Attempt:            1,
			Status:             agentrun.StatusCompleted,
			CompletedAt:        completedAt,
		},
		messages: map[string]agentconversation.Message{
			"40000000-0000-4000-8000-000000000001": {
				ID:      "40000000-0000-4000-8000-000000000001",
				Content: "I am a Java backend engineer.",
			},
			"50000000-0000-4000-8000-000000000001": {
				ID:      "50000000-0000-4000-8000-000000000001",
				Content: "I will tailor the practice.",
			},
		},
		manifest: agentcontext.Manifest{
			ActiveMatterID: "60000000-0000-4000-8000-000000000001",
		},
	}
	reader, err := newAgentCompletedRunReader(repository)
	if err != nil {
		t.Fatalf("newAgentCompletedRunReader: %v", err)
	}
	source, err := reader.ReadCompletedRun(
		context.Background(),
		repository.run.OwnerID,
		repository.run.ID,
	)
	if err != nil {
		t.Fatalf("ReadCompletedRun: %v", err)
	}
	if !source.Valid() ||
		source.UserText != "I am a Java backend engineer." ||
		source.AssistantText != "I will tailor the practice." ||
		source.MatterID != repository.manifest.ActiveMatterID {
		t.Fatalf("source = %#v", source)
	}
}

func TestAgentCompletedRunReaderRejectsNonCompletedRun(t *testing.T) {
	t.Parallel()

	repository := &fakeCompletedAgentRunRepository{
		run: agentrun.Run{
			ID:      "10000000-0000-4000-8000-000000000001",
			OwnerID: "20000000-0000-4000-8000-000000000001",
			Status:  agentrun.StatusRunning,
		},
	}
	reader, _ := newAgentCompletedRunReader(repository)
	if _, err := reader.ReadCompletedRun(
		context.Background(),
		repository.run.OwnerID,
		repository.run.ID,
	); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("ReadCompletedRun error = %v", err)
	}
}

func TestAgentCompletedRunReaderTreatsInvalidStoredManifestAsDependencyFailure(
	t *testing.T,
) {
	t.Parallel()

	err := mapAgentMemorySourceError(agentcontext.ErrInvalidContext)
	if errors.Is(err, memory.ErrInvalidArgument) ||
		!errors.Is(err, agentcontext.ErrInvalidContext) {
		t.Fatalf("mapped invalid stored Manifest error = %v", err)
	}
}

type fakeCompletedAgentRunRepository struct {
	run      agentrun.Run
	messages map[string]agentconversation.Message
	manifest agentcontext.Manifest
	err      error
}

func (repository *fakeCompletedAgentRunRepository) Find(
	context.Context,
	string,
	string,
) (agentrun.Run, error) {
	return repository.run, repository.err
}

func (repository *fakeCompletedAgentRunRepository) FindMessage(
	_ context.Context,
	_ string,
	_ string,
	messageID string,
) (agentconversation.Message, error) {
	message, ok := repository.messages[messageID]
	if !ok {
		return agentconversation.Message{}, agentconversation.ErrNotFound
	}
	return message, nil
}

func (repository *fakeCompletedAgentRunRepository) FindManifest(
	context.Context,
	string,
	string,
) (agentcontext.Manifest, error) {
	return repository.manifest, repository.err
}
