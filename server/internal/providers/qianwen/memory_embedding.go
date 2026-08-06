package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type MemoryEmbedder struct {
	client *embeddingClient
}

func NewMemoryEmbedder(
	configuration EmbeddingConfig,
	apiKey string,
) (*MemoryEmbedder, error) {
	client, err := newEmbeddingClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &MemoryEmbedder{client: client}, nil
}

func (embedder *MemoryEmbedder) Embed(
	ctx context.Context,
	request memory.EmbeddingRequest,
) (memory.EmbeddingResult, error) {
	if embedder == nil || embedder.client == nil {
		return memory.EmbeddingResult{}, protocol.NewEmbeddingError(
			protocol.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: Memory embedder is required"),
		)
	}
	if err := memory.ValidateEmbeddingRequest(request); err != nil {
		return memory.EmbeddingResult{}, protocol.NewEmbeddingError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	result, err := embedder.client.Embed(ctx, protocol.EmbeddingRequest{
		Inputs:     []string{request.Input},
		Dimensions: request.Dimensions,
	})
	if err != nil {
		return memory.EmbeddingResult{}, err
	}
	if len(result.Vectors) != 1 {
		return memory.EmbeddingResult{}, protocol.NewEmbeddingError(
			protocol.ErrorInvalidResponse,
			0,
			"",
			"",
			errors.New("qianwen: Memory embedding result has invalid vector count"),
		)
	}
	mapped := memory.EmbeddingResult{
		Provider:    result.Provider,
		Model:       result.Model,
		Dimensions:  result.Dimensions,
		Vector:      result.Vectors[0],
		InputTokens: result.InputTokens,
		TotalTokens: result.TotalTokens,
	}
	if err := memory.ValidateEmbeddingResult(request.Dimensions, mapped); err != nil {
		return memory.EmbeddingResult{}, protocol.NewEmbeddingError(
			protocol.ErrorInvalidResponse,
			0,
			"",
			"",
			err,
		)
	}
	return mapped, nil
}

var _ memory.Embedder = (*MemoryEmbedder)(nil)
