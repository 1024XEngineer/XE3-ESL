package qianwen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestEmbeddingClientUsesCompatibleContractAndPreservesOrder(
	t *testing.T,
) {
	t.Parallel()

	vectorA := make([]float32, 1024)
	vectorB := make([]float32, 1024)
	vectorA[0] = 1
	vectorB[1] = 1
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() !=
			"https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings" {
			t.Fatalf("URL = %s", request.URL)
		}
		if request.Header.Get(authorizationHeaderName) != "Bearer test-key" {
			t.Fatal("missing authorization")
		}
		var payload embeddingRequest
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Model != "text-embedding-v4" ||
			payload.Dimensions != 1024 ||
			payload.EncodingFormat != "float" ||
			len(payload.Input) != 2 {
			t.Fatalf("payload = %#v", payload)
		}
		response, err := json.Marshal(map[string]any{
			"model": "text-embedding-v4",
			"data": []map[string]any{
				{"index": 1, "embedding": vectorB},
				{"index": 0, "embedding": vectorA},
			},
			"usage": map[string]int{
				"prompt_tokens": 4,
				"total_tokens":  4,
			},
		})
		if err != nil {
			t.Fatalf("encode response: %v", err)
		}
		return jsonResponse(http.StatusOK, string(response)), nil
	})
	client, err := newEmbeddingClientWithHTTP(
		EmbeddingConfig{
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "text-embedding-v4",
			Dimensions: 1024,
			Timeout:    time.Second,
		},
		"test-key",
		doer,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Embed(
		context.Background(),
		protocol.EmbeddingRequest{
			Inputs:     []string{"first", "second"},
			Dimensions: 1024,
		},
	)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Vectors[0][0] != 1 ||
		result.Vectors[1][1] != 1 ||
		result.Provider != "qianwen" ||
		result.Model != "text-embedding-v4" {
		t.Fatalf("result = %#v", result)
	}
}

func TestEmbeddingClientRejectsDuplicateProviderIndexes(t *testing.T) {
	t.Parallel()

	vector := make([]float32, 1024)
	vector[0] = 1
	response, _ := json.Marshal(map[string]any{
		"model": "text-embedding-v4",
		"data": []map[string]any{
			{"index": 0, "embedding": vector},
			{"index": 0, "embedding": vector},
		},
		"usage": map[string]int{"prompt_tokens": 2, "total_tokens": 2},
	})
	client, err := newEmbeddingClientWithHTTP(
		EmbeddingConfig{
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "text-embedding-v4",
			Dimensions: 1024,
			Timeout:    time.Second,
		},
		"test-key",
		doerFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, string(response)), nil
		}),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Embed(
		context.Background(),
		protocol.EmbeddingRequest{
			Inputs:     []string{"first", "second"},
			Dimensions: 1024,
		},
	); err == nil {
		t.Fatal("expected invalid response")
	}
}
