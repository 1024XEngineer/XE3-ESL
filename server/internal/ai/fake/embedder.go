package fake

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

// Embedder is an explicit deterministic provider for tests.
type Embedder struct {
	Result ai.EmbeddingResult
	Err    error
}

func (embedder *Embedder) Embed(
	ctx context.Context,
	request ai.EmbeddingRequest,
) (ai.EmbeddingResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.EmbeddingResult{}, err
	}
	if err := ai.ValidateEmbeddingRequest(request); err != nil {
		return ai.EmbeddingResult{}, err
	}
	if embedder == nil {
		return ai.EmbeddingResult{}, ai.NewEmbeddingError(
			ai.ErrorConfiguration, 0, "", "", nil,
		)
	}
	if embedder.Err != nil {
		return ai.EmbeddingResult{}, embedder.Err
	}
	return embedder.Result, nil
}

var _ ai.Embedder = (*Embedder)(nil)
