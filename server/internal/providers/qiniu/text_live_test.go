package qiniu

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

const liveQiniuContractRequestInterval = 5 * time.Second

func TestLiveQiniuTextGeneration(t *testing.T) {
	generator, model := liveQiniuAgentGenerator(t)

	request := agentrun.TextRequest{Messages: []agentrun.TextMessage{{
		Role: agentrun.TextRoleUser,
		Content: "You are a friendly English speaking coach. The learner says: " +
			"I want to improve my spoken English for travel. Reply naturally in " +
			"exactly three complete English sentences, 45 to 60 words total. " +
			"Ask one useful follow-up question. Do not use markdown.",
	}}}
	var streamed strings.Builder
	var firstSentenceAt time.Duration
	firstSentenceDelta := 0
	nonEmptyDeltas := 0
	streamStarted := time.Now()
	streamResult, err := generator.GenerateStream(
		context.Background(),
		request,
		agentrun.TextDeltaObserverFunc(func(_ context.Context, delta string) error {
			if strings.TrimSpace(delta) != "" {
				nonEmptyDeltas++
			}
			streamed.WriteString(delta)
			if firstSentenceAt == 0 && strings.ContainsAny(streamed.String(), ".!?") {
				firstSentenceAt = time.Since(streamStarted)
				firstSentenceDelta = nonEmptyDeltas
			}
			return nil
		}),
	)
	streamDuration := time.Since(streamStarted)
	if err != nil {
		t.Fatalf("live Qiniu streaming generation failed: %v", err)
	}
	if streamResult.Provider != TextProviderName ||
		streamResult.Model != model ||
		streamResult.Content == "" || streamResult.Content != streamed.String() ||
		streamResult.Usage.TotalTokens <= 0 {
		t.Fatalf("invalid live Qiniu stream result: %#v", streamResult)
	}
	if nonEmptyDeltas <= 1 {
		t.Fatalf("Qiniu stream emitted %d non-empty visible delta(s), want more than one", nonEmptyDeltas)
	}
	if firstSentenceAt == 0 || firstSentenceDelta >= nonEmptyDeltas {
		t.Fatalf(
			"first complete sentence delta = %d/%d, time = %s, stream duration = %s",
			firstSentenceDelta,
			nonEmptyDeltas,
			firstSentenceAt,
			streamDuration,
		)
	}
	t.Logf(
		"Qiniu model=%s visible_deltas=%d first_sentence=%s total=%s",
		model,
		nonEmptyDeltas,
		firstSentenceAt.Round(time.Millisecond),
		streamDuration.Round(time.Millisecond),
	)
}

func TestLiveQiniuAgentContracts(t *testing.T) {
	generator, model := liveQiniuAgentGenerator(t)
	ctx := context.Background()

	toolResult, err := generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{{
			Role:    agentrun.TextRoleUser,
			Content: "Save that I prefer short daily English practice.",
		}},
		Tools: []agentrun.ToolDefinition{{
			Name:        "preference.save.v1",
			Description: "Save one English practice preference.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preference": map[string]any{"type": "string"},
				},
				"required": []string{"preference"},
			},
		}},
		ToolChoice: agentrun.ToolChoice{Mode: agentrun.ToolChoiceRequired},
	})
	if err != nil {
		t.Fatalf("live Qiniu tool generation failed: %v", err)
	}
	if toolResult.Model != model || len(toolResult.ToolCalls) != 1 ||
		toolResult.ToolCalls[0].Name != "preference.save.v1" {
		t.Fatalf("invalid live Qiniu tool result: %#v", toolResult)
	}
	var toolArguments map[string]any
	if err := json.Unmarshal(toolResult.ToolCalls[0].Arguments, &toolArguments); err != nil ||
		strings.TrimSpace(valueString(toolArguments["preference"])) == "" {
		t.Fatalf("invalid live Qiniu tool arguments: %#v, %v", toolArguments, err)
	}
	time.Sleep(liveQiniuContractRequestInterval)

	imageResult, err := generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{{
			Role: agentrun.TextRoleUser,
			ContentParts: []agentrun.ContentPart{
				{
					Kind: agentrun.ContentPartText,
					Text: "Return only a JSON object with keys company, tip, and " +
						"example. Identify the company in this image, then give one " +
						"short English-learning tip and example.",
				},
				{
					Kind:     agentrun.ContentPartImageURL,
					ImageURL: "https://www.qiniu.com/qiniu_ai_token_snapshot.png",
				},
			},
		}},
		ResponseFormat: agentrun.TextResponseFormatJSON,
	})
	if err != nil {
		t.Fatalf("live Qiniu multimodal JSON generation failed: %v", err)
	}
	var structured map[string]any
	if err := json.Unmarshal([]byte(imageResult.Content), &structured); err != nil ||
		strings.TrimSpace(valueString(structured["tip"])) == "" ||
		strings.TrimSpace(valueString(structured["example"])) == "" {
		t.Fatalf("invalid live Qiniu multimodal JSON result: %#v, %v", structured, err)
	}
	company := valueString(structured["company"])
	if !strings.Contains(strings.ToLower(company), "qiniu") &&
		!strings.Contains(company, "七牛") {
		t.Fatalf("live Qiniu image answer did not identify Qiniu: %q", company)
	}
}

func liveQiniuAgentGenerator(t *testing.T) (*AgentRunGenerator, string) {
	t.Helper()
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
	return generator, configuration.Model
}

func valueString(value any) string {
	text, _ := value.(string)
	return text
}
