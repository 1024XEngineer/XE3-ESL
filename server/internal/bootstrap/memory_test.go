package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
)

func TestAgentCompletedRunReaderProjectsOwnedSource(t *testing.T) {
	t.Parallel()

	completedAt := time.Now().UTC()
	repository := &fakeCompletedAgentRunRepository{
		run: agent.Run{
			ID:                 "10000000-0000-4000-8000-000000000001",
			OwnerID:            "20000000-0000-4000-8000-000000000001",
			ThreadID:           "30000000-0000-4000-8000-000000000001",
			InputMessageID:     "40000000-0000-4000-8000-000000000001",
			AssistantMessageID: "50000000-0000-4000-8000-000000000001",
			Attempt:            1,
			Status:             agent.RunStatusCompleted,
			CompletedAt:        completedAt,
		},
		messages: map[string]agent.Message{
			"40000000-0000-4000-8000-000000000001": {
				ID:      "40000000-0000-4000-8000-000000000001",
				Content: "I am a Java backend engineer.",
			},
			"50000000-0000-4000-8000-000000000001": {
				ID:      "50000000-0000-4000-8000-000000000001",
				Content: "I will tailor the practice.",
			},
		},
		manifest: agent.ContextManifest{
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
		run: agent.Run{
			ID:      "10000000-0000-4000-8000-000000000001",
			OwnerID: "20000000-0000-4000-8000-000000000001",
			Status:  agent.RunStatusRunning,
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

type fakeCompletedAgentRunRepository struct {
	run      agent.Run
	messages map[string]agent.Message
	manifest agent.ContextManifest
	err      error
}

func (repository *fakeCompletedAgentRunRepository) FindRun(
	context.Context,
	string,
	string,
) (agent.Run, error) {
	return repository.run, repository.err
}

func (repository *fakeCompletedAgentRunRepository) FindMessage(
	_ context.Context,
	_ string,
	_ string,
	messageID string,
) (agent.Message, error) {
	message, ok := repository.messages[messageID]
	if !ok {
		return agent.Message{}, agent.ErrNotFound
	}
	return message, nil
}

func (repository *fakeCompletedAgentRunRepository) FindContextManifest(
	context.Context,
	string,
	string,
) (agent.ContextManifest, error) {
	return repository.manifest, repository.err
}
