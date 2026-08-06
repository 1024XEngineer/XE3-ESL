package qianwen

import (
	"context"
	"os"
	"testing"

	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestLiveTextGeneration(t *testing.T) {
	if os.Getenv("QIANWEN_LIVE_TEST") != "1" {
		t.Skip(
			"set QIANWEN_LIVE_TEST=1 and the Qianwen environment variables " +
				"to run; the real request may incur charges",
		)
	}

	cfg, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	generator, err := newTextClient(TextConfig{
		BaseURL:         cfg.BaseURL,
		Model:           cfg.Model,
		Timeout:         cfg.Timeout,
		MaxOutputTokens: cfg.MaxOutputTokens,
	}, cfg.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qianwen generator: %v", err)
	}

	result, err := generator.Generate(context.Background(), protocol.TextRequest{
		Messages: []protocol.TextMessage{{
			Role:    protocol.TextRoleUser,
			Content: "Reply with one short English greeting.",
		}},
	})
	if err != nil {
		t.Fatalf("live Qianwen generation failed: %v", err)
	}
	if result.Content == "" {
		t.Fatal("live Qianwen generation returned empty content")
	}
}
