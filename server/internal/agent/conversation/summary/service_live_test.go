package summary

import (
	"context"
	"os"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
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
	generator, err := qianwen.New(
		qianwen.Config{
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
	repository := &repositoryStub{
		latestErr: conversation.ErrNotFound,
		messages: []conversation.Message{
			sourceMessageFixture(
				1,
				conversation.MessageRoleUser,
				"My goal is to prepare for a product manager interview.",
			),
			sourceMessageFixture(
				2,
				conversation.MessageRoleAssistant,
				"Let's practice a quantified STAR answer first.",
			),
		},
	}
	service, err := NewService(
		repository,
		generator,
		Configuration{
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
		testGenerateCommand(2),
	)
	if err != nil {
		t.Fatalf("live summary generation: %v", err)
	}
	if !checkpoint.Valid() ||
		len(checkpoint.Content.Goals) == 0 {
		t.Fatalf("invalid live checkpoint: %#v", checkpoint)
	}
}
