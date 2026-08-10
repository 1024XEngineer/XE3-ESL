package qiniu

import (
	"context"
	"os"
	"strings"
	"testing"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestLiveQiniuTextGeneration(t *testing.T) {
	if os.Getenv("QINIU_LLM_LIVE_TEST") != "1" {
		t.Skip("set QINIU_LLM_LIVE_TEST=1 with the Qiniu AI environment variables; the real request may incur charges")
	}
	configuration, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load Qiniu text generation config: %v", err)
	}
	if configuration.Provider != platformconfig.TextProviderQiniu {
		t.Fatalf("TEXT_GENERATION_PROVIDER = %q, want qiniu", configuration.Provider)
	}
	generator, err := NewAgentRunGenerator(TextConfig{
		BaseURL: configuration.BaseURL, Model: configuration.Model,
		Timeout: configuration.Timeout, MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qiniu text generator: %v", err)
	}

	request := agentrun.TextRequest{Messages: []agentrun.TextMessage{{
		Role: agentrun.TextRoleUser, Content: "Reply with one short English greeting.",
	}}}
	result, err := generator.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("live Qiniu text generation failed: %v", err)
	}
	if result.Provider != TextProviderName || result.Model != configuration.Model ||
		strings.TrimSpace(result.Content) == "" || result.ID == "" ||
		result.Usage.TotalTokens <= 0 {
		t.Fatalf("invalid live Qiniu result: %#v", result)
	}

	var streamed strings.Builder
	streamResult, err := generator.GenerateStream(
		context.Background(),
		request,
		agentrun.TextDeltaObserverFunc(func(_ context.Context, delta string) error {
			streamed.WriteString(delta)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("live Qiniu streaming generation failed: %v", err)
	}
	if streamResult.Provider != TextProviderName ||
		streamResult.Model != configuration.Model ||
		streamResult.Content == "" || streamResult.Content != streamed.String() ||
		streamResult.Usage.TotalTokens <= 0 {
		t.Fatalf("invalid live Qiniu stream result: %#v", streamResult)
	}
}
