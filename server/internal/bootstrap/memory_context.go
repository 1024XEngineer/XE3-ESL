package bootstrap

import (
	"context"
	"errors"

	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
)

type agentMemoryContextSearcher struct {
	searcher memory.Searcher
}

func newAgentMemoryContextSearcher(
	searcher memory.Searcher,
) (*agentMemoryContextSearcher, error) {
	if searcher == nil {
		return nil, errors.New(
			"bootstrap: Memory Context search dependency is required",
		)
	}
	return &agentMemoryContextSearcher{searcher: searcher}, nil
}

func (searcher *agentMemoryContextSearcher) Search(
	ctx context.Context,
	request agentruntime.MemorySearchRequest,
) ([]agentruntime.MemorySearchHit, error) {
	if searcher == nil || searcher.searcher == nil {
		return nil, errors.New(
			"bootstrap: Memory Context search dependency is required",
		)
	}
	hits, err := searcher.searcher.Search(ctx, memory.SearchRequest{
		Actor:    request.Actor,
		Query:    request.Query,
		MatterID: request.MatterID,
		Limit:    request.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]agentruntime.MemorySearchHit, 0, len(hits))
	for _, hit := range hits {
		result = append(result, agentruntime.MemorySearchHit{
			MemoryID:               hit.MemoryID,
			MemoryVersion:          hit.MemoryVersion,
			Type:                   string(hit.Type),
			Content:                hit.Content,
			Scope:                  string(hit.Scope),
			MatterID:               hit.MatterID,
			Similarity:             hit.Similarity,
			Score:                  hit.Score,
			EmbeddingProvider:      hit.EmbeddingProvider,
			EmbeddingModel:         hit.EmbeddingModel,
			EmbeddingDimensions:    hit.EmbeddingDimensions,
			EmbeddingPolicyVersion: hit.EmbeddingPolicyVersion,
			RetrievalPolicyVersion: hit.RetrievalPolicyVersion,
		})
	}
	return result, nil
}

var _ agentruntime.MemorySearcher = (*agentMemoryContextSearcher)(nil)
