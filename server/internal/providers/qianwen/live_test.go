package qianwen

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestLiveTextGeneration(t *testing.T) {
	cfg := liveTextConfiguration(t)
	generator := liveTextGenerator(t, cfg, cfg.Model)

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

func TestLiveTextToolRoundTrip(t *testing.T) {
	cfg := liveTextConfiguration(t)
	generator := liveTextGenerator(t, cfg, cfg.Model)
	tool := protocol.ToolDefinition{
		Name:        "practice.preview.v3",
		Description: "Create a practice preview.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type": "string", "format": "non-empty-text", "minLength": 1,
				},
			},
			"required":             []any{"title"},
			"additionalProperties": false,
		},
	}
	user := protocol.TextMessage{
		Role:    protocol.TextRoleUser,
		Content: "Create an IELTS speaking practice scene about travel.",
	}
	first, err := generator.Generate(context.Background(), protocol.TextRequest{
		Messages: []protocol.TextMessage{user},
		Tools:    []protocol.ToolDefinition{tool},
		ToolChoice: protocol.ToolChoice{
			Mode: protocol.ToolChoiceSpecific,
			Name: tool.Name,
		},
	})
	if err != nil {
		t.Fatalf("live tool selection failed: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != tool.Name {
		t.Fatalf("live tool selection = %#v", first)
	}
	second, err := generator.Generate(context.Background(), protocol.TextRequest{
		Messages: []protocol.TextMessage{
			user,
			{Role: protocol.TextRoleAssistant, ToolCalls: first.ToolCalls},
			{
				Role: protocol.TextRoleTool, ToolCallID: first.ToolCalls[0].ID,
				Content: `{"scene_id":"scene-live","status":"ready"}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("live tool result continuation failed: %v", err)
	}
	if second.Content == "" {
		t.Fatal("live tool result continuation returned empty content")
	}
}

func TestLiveStrictJSONSchemaGeneration(t *testing.T) {
	cfg := liveTextConfiguration(t)
	generator := liveTextGenerator(t, cfg, cfg.EvaluationModel)
	result, err := generator.Generate(context.Background(), protocol.TextRequest{
		Messages: []protocol.TextMessage{{
			Role: protocol.TextRoleUser,
			Content: "Return a score from 0 to 9 and a short summary for: " +
				"I enjoy learning English through conversation.",
		}},
		ResponseFormat: protocol.TextResponseFormatJSONSchema,
		ResponseSchema: &protocol.JSONSchemaDefinition{
			Name:   "live_evaluation",
			Strict: true,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"score": map[string]any{
						"type": "number", "minimum": 0, "maximum": 9,
					},
					"summary": map[string]any{"type": "string"},
				},
				"required":             []any{"score", "summary"},
				"additionalProperties": false,
			},
		},
	})
	if err != nil {
		t.Fatalf("live strict JSON Schema generation failed: %v", err)
	}
	var payload struct {
		Score   float64 `json:"score"`
		Summary string  `json:"summary"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil ||
		payload.Score < 0 || payload.Score > 9 || payload.Summary == "" {
		t.Fatalf("live strict JSON payload is invalid: payload=%#v err=%v", payload, err)
	}
}

func liveTextConfiguration(t *testing.T) platformconfig.TextGenerationConfig {
	t.Helper()
	if os.Getenv("QIANWEN_LIVE_TEST") != "1" {
		t.Skip(
			"set QIANWEN_LIVE_TEST=1 and the text provider environment variables " +
				"to run; the real request may incur charges",
		)
	}
	cfg, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	return cfg
}

func liveTextGenerator(
	t *testing.T,
	cfg platformconfig.TextGenerationConfig,
	model string,
) *textClient {
	t.Helper()
	generator, err := newTextClient(TextConfig{
		Provider:        cfg.Provider,
		BaseURL:         cfg.BaseURL,
		Model:           model,
		Timeout:         cfg.Timeout,
		MaxOutputTokens: cfg.MaxOutputTokens,
	}, cfg.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create live text generator: %v", err)
	}
	return generator
}
