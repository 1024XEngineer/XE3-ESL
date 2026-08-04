package summary_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

func TestLiveSummaryGeneration(t *testing.T) {
	if os.Getenv("QIANWEN_LIVE_TEST") != "1" {
		t.Skip(
			"set QIANWEN_LIVE_TEST=1 and the Qianwen environment " +
				"variables to run; the real request may incur charges",
		)
	}
	configuration, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	generator, err := qianwen.NewSummaryGenerator(
		qianwen.TextConfig{
			BaseURL:         configuration.BaseURL,
			Model:           configuration.Model,
			Timeout:         configuration.Timeout,
			MaxOutputTokens: configuration.MaxOutputTokens,
		},
		configuration.APIKey.Reveal(),
	)
	if err != nil {
		t.Fatalf("new Qianwen generator: %v", err)
	}
	repository := &liveSummaryRepository{
		messages: []conversation.Message{
			liveSummaryMessage(1, conversation.MessageRoleUser,
				"My goal is to prepare for a product manager interview."),
			liveSummaryMessage(2, conversation.MessageRoleAssistant,
				"Let's practice a quantified STAR answer first."),
		},
	}
	service, err := summary.NewService(
		repository,
		generator,
		summary.Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      configuration.Provider,
			Model:         configuration.Model,
		},
	)
	if err != nil {
		t.Fatalf("new summary service: %v", err)
	}
	checkpoint, err := service.GenerateCheckpoint(
		context.Background(),
		summary.GenerateCheckpointCommand{
			OwnerID:                liveSummaryOwnerID,
			ThreadID:               liveSummaryThreadID,
			CoveredThroughSequence: 2,
		},
	)
	if err != nil {
		t.Fatalf("live summary generation: %v", err)
	}
	if !checkpoint.Valid() ||
		len(checkpoint.Content.Goals) == 0 {
		t.Fatalf("invalid live checkpoint: %#v", checkpoint)
	}
}

const (
	liveSummaryOwnerID  = "10000000-0000-4000-8000-000000000001"
	liveSummaryThreadID = "20000000-0000-4000-8000-000000000001"
)

type liveSummaryRepository struct {
	messages []conversation.Message
}

func (*liveSummaryRepository) FindLatestCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (summary.Checkpoint, error) {
	return summary.Checkpoint{}, conversation.ErrNotFound
}

func (repository *liveSummaryRepository) ListMessagesForSummary(
	context.Context,
	string,
	string,
	int64,
	int64,
) ([]conversation.Message, error) {
	return append([]conversation.Message(nil), repository.messages...), nil
}

func (*liveSummaryRepository) CreateCheckpoint(
	_ context.Context,
	command summary.CreateCheckpointCommand,
) (summary.Checkpoint, error) {
	return summary.Checkpoint{
		ID:                     "40000000-0000-4000-8000-000000000001",
		OwnerID:                command.OwnerID,
		ThreadID:               command.ThreadID,
		PreviousCheckpointID:   command.PreviousCheckpointID,
		SourceFromSequence:     command.SourceFromSequence,
		CoveredThroughSequence: command.CoveredThroughSequence,
		Content:                command.Content,
		PolicyVersion:          command.PolicyVersion,
		PromptVersion:          command.PromptVersion,
		Provider:               command.Provider,
		Model:                  command.Model,
		SourceChecksum:         command.SourceChecksum,
		CreatedAt:              time.Now().UTC(),
	}, nil
}

func liveSummaryMessage(
	sequence int64,
	role conversation.MessageRole,
	content string,
) conversation.Message {
	return conversation.Message{
		ID:        "30000000-0000-4000-8000-000000000001",
		OwnerID:   liveSummaryOwnerID,
		ThreadID:  liveSummaryThreadID,
		Sequence:  sequence,
		Role:      role,
		Modality:  conversation.MessageModalityText,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}
