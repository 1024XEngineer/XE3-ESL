package qianwen

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestAgentRunAdapterMapsOwnedContract(t *testing.T) {
	t.Parallel()

	request := agentrun.TextRequest{
		Messages: []agentrun.TextMessage{
			{Role: agentrun.TextRoleSystem, Content: "system"},
			{
				Role: agentrun.TextRoleUser,
				ContentParts: []agentrun.ContentPart{
					{Kind: agentrun.ContentPartText, Text: "question"},
					{Kind: agentrun.ContentPartImageURL, ImageURL: "https://media.example/image"},
				},
			},
		},
		Tools: []agentrun.ToolDefinition{{
			Name:        "create_practice",
			Description: "Create a practice session",
			InputSchema: map[string]any{"type": "object"},
		}},
		ToolChoice: agentrun.ToolChoice{Mode: agentrun.ToolChoiceAuto},
	}
	mapped, err := agentRunRequest(request)
	if err != nil {
		t.Fatalf("map request: %v", err)
	}
	if len(mapped.Messages) != 2 ||
		mapped.Messages[1].ContentParts[1].ImageURL !=
			"https://media.example/image" ||
		len(mapped.Tools) != 1 ||
		mapped.Tools[0].Name != "create_practice" ||
		mapped.ToolChoice.Mode != protocol.ToolChoiceAuto {
		t.Fatalf("mapped request = %#v", mapped)
	}

	result := agentRunResult(protocol.TextResult{
		ID: "completion-1", Provider: "qianwen", Model: "qwen",
		ToolCalls: []protocol.ToolCall{{
			ID: "call-1", Name: "create_practice",
			Arguments: json.RawMessage(`{"scene_id":"scene-1"}`),
		}},
		FinishReason: "tool_calls",
		Usage: protocol.TokenUsage{
			InputTokens: 10, OutputTokens: 4, TotalTokens: 14,
		},
	})
	if len(result.ToolCalls) != 1 ||
		result.ToolCalls[0].Name != "create_practice" ||
		result.Usage.TotalTokens != 14 {
		t.Fatalf("mapped result = %#v", result)
	}
}

func TestAgentAdaptersMapStableProviderFailures(t *testing.T) {
	t.Parallel()

	generationCause := errors.New("provider detail")
	generationErr := mapAgentRunError(protocol.NewGenerationError(
		protocol.ErrorRateLimited,
		429,
		"Throttled",
		"request-1",
		generationCause,
	))
	var runFailure *agentrun.GenerationError
	if !errors.As(generationErr, &runFailure) ||
		runFailure.Kind != agentrun.ErrorRateLimited ||
		runFailure.StatusCode != 429 ||
		runFailure.ProviderCode != "Throttled" ||
		runFailure.RequestID != "request-1" ||
		!runFailure.Retryable() ||
		!errors.Is(generationErr, generationCause) {
		t.Fatalf("mapped Run failure = %#v", generationErr)
	}

	speechCause := errors.New("provider detail")
	speechErr := mapAgentVoiceError(
		protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorQuotaExhausted,
			402,
			"QuotaExceeded",
			"request-2",
			speechCause,
		),
		agentvoice.SpeechOperationTranscription,
	)
	var voiceFailure *agentvoice.SpeechError
	if !errors.As(speechErr, &voiceFailure) ||
		voiceFailure.Kind != agentvoice.ErrorQuotaExhausted ||
		voiceFailure.StatusCode != 402 ||
		voiceFailure.ProviderCode != "QuotaExceeded" ||
		voiceFailure.RequestID != "request-2" ||
		voiceFailure.Retryable() ||
		!errors.Is(speechErr, speechCause) {
		t.Fatalf("mapped Voice failure = %#v", speechErr)
	}
}

func TestAgentVoiceAdapterMapsTranscriptionResult(t *testing.T) {
	t.Parallel()

	mapped := agentVoiceTranscriptionResult(protocol.TranscriptionResult{
		ID: "transcription-1", Provider: "qianwen", Model: "fun-asr",
		Transcript: "hello", Language: "en", Emotion: "neutral",
		FinishReason: "stop",
		Usage:        protocol.SpeechUsage{InputTokens: 3, TotalTokens: 3, AudioSeconds: 2},
	})
	want := agentvoice.TranscriptionResult{
		ID: "transcription-1", Provider: "qianwen", Model: "fun-asr",
		Transcript: "hello", Language: "en", Emotion: "neutral",
		FinishReason: "stop",
		Usage:        agentvoice.SpeechUsage{InputTokens: 3, TotalTokens: 3, AudioSeconds: 2},
	}
	if !reflect.DeepEqual(mapped, want) {
		t.Fatalf("mapped transcription = %#v, want %#v", mapped, want)
	}
}
