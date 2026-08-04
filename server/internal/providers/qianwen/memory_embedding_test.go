package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestMemoryEmbedderMapsSingleOwnedInput(t *testing.T) {
	t.Parallel()

	const dimensions = 64
	vector := make([]float32, dimensions)
	vector[0] = 1
	var received embeddingRequest
	client, err := newEmbeddingClientWithHTTP(
		EmbeddingConfig{
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "text-embedding-v4",
			Dimensions: dimensions,
			Timeout:    time.Second,
		},
		"test-key",
		doerFunc(func(request *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			body, err := json.Marshal(map[string]any{
				"model": "text-embedding-v4",
				"data": []map[string]any{{
					"index":     0,
					"embedding": vector,
				}},
				"usage": map[string]int{
					"prompt_tokens": 3,
					"total_tokens":  3,
				},
			})
			if err != nil {
				t.Fatalf("encode response: %v", err)
			}
			return jsonResponse(http.StatusOK, string(body)), nil
		}),
	)
	if err != nil {
		t.Fatalf("new embedding client: %v", err)
	}
	result, err := (&MemoryEmbedder{client: client}).Embed(
		context.Background(),
		memory.EmbeddingRequest{
			Input:      "durable user fact",
			Dimensions: dimensions,
		},
	)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(received.Input) != 1 ||
		received.Input[0] != "durable user fact" ||
		received.Dimensions != dimensions ||
		result.Provider != providerName ||
		result.Model != "text-embedding-v4" ||
		result.Dimensions != dimensions ||
		len(result.Vector) != dimensions ||
		result.Vector[0] != 1 ||
		result.InputTokens != 3 || result.TotalTokens != 3 {
		t.Fatalf("request/result = %#v / %#v", received, result)
	}
}

func TestMemoryEmbedderRejectsInvalidOwnedRequestBeforeProviderCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, err := newEmbeddingClientWithHTTP(
		EmbeddingConfig{
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "text-embedding-v4",
			Dimensions: 64,
			Timeout:    time.Second,
		},
		"test-key",
		doerFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, nil
		}),
	)
	if err != nil {
		t.Fatalf("new embedding client: %v", err)
	}
	_, err = (&MemoryEmbedder{client: client}).Embed(
		context.Background(),
		memory.EmbeddingRequest{Input: " untrimmed ", Dimensions: 64},
	)
	var failure memory.ProviderFailure
	if !errors.As(err, &failure) ||
		failure.StableCategory() != string(protocol.ErrorInvalidRequest) ||
		failure.Retryable() {
		t.Fatalf("provider failure = %#v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want zero", calls.Load())
	}
}
