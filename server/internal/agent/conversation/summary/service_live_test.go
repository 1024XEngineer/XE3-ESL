//go:build live

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
		t.Skip("set QIANWEN_LIVE_TEST=1 to run the billable provider check")
	}
	configuration, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatal(err)
	}
	generator, err := qianwen.NewSummaryGenerator(qianwen.TextConfig{
		Provider:        configuration.Provider,
		BaseURL:         configuration.BaseURL,
		Model:           configuration.Model,
		Timeout:         configuration.Timeout,
		MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	service, err := summary.NewGeneratorService(generator, summary.Configuration{
		Provider: configuration.Provider,
		Model:    configuration.Model,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Generate(context.Background(), summary.GenerateCommand{
		MaxInputCharacters: 12000,
		Messages: []conversation.Message{
			liveMessage(1, conversation.MessageRoleUser, "I am preparing for a product interview."),
			liveMessage(2, conversation.MessageRoleAssistant, "Let's practice a STAR answer."),
		},
	})
	if err != nil || !content.Valid() {
		t.Fatalf("content=%#v error=%v", content, err)
	}
}

func liveMessage(sequence int64, role conversation.MessageRole, content string) conversation.Message {
	return conversation.Message{
		ID:        "30000000-0000-4000-8000-000000000001",
		OwnerID:   "10000000-0000-4000-8000-000000000001",
		ThreadID:  "20000000-0000-4000-8000-000000000001",
		Sequence:  sequence,
		Role:      role,
		Modality:  conversation.MessageModalityText,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}
