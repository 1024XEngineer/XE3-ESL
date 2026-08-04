package bootstrap

import (
	"context"
	"errors"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
)

type agentMemoryContextSearcher struct {
	searcher memory.Searcher
}

type memoryExtractionBarrier interface {
	Await(
		context.Context,
		memory.ExtractionBarrierRequest,
	) (memory.ExtractionBarrierResult, error)
}

type agentMemoryExtractionBarrier struct {
	barrier memoryExtractionBarrier
}

func newAgentMemoryExtractionBarrier(
	barrier memoryExtractionBarrier,
) (*agentMemoryExtractionBarrier, error) {
	if barrier == nil {
		return nil, errors.New(
			"bootstrap: Memory Extraction Barrier is required",
		)
	}
	return &agentMemoryExtractionBarrier{barrier: barrier}, nil
}

func (barrier *agentMemoryExtractionBarrier) Await(
	ctx context.Context,
	request agentcontext.MemoryExtractionBarrierRequest,
) (agentcontext.MemoryExtractionBarrierResult, error) {
	result, err := barrier.barrier.Await(
		ctx,
		memory.ExtractionBarrierRequest{
			Actor:  request.Actor,
			Cutoff: request.Cutoff,
		},
	)
	if err != nil {
		if errors.Is(err, memory.ErrExtractionBarrierRejected) {
			return agentcontext.MemoryExtractionBarrierResult{},
				agentcontext.ErrMemoryConsistencyRejected
		}
		return agentcontext.MemoryExtractionBarrierResult{},
			agentcontext.ErrMemoryConsistencyUnavailable
	}
	return agentcontext.MemoryExtractionBarrierResult{
		PolicyVersion: result.PolicyVersion,
		Cutoff:        result.Cutoff,
		Status: agentcontext.MemoryExtractionBarrierStatus(
			result.Status,
		),
		Waited:         result.Waited,
		CoveredThrough: result.CoveredThrough,
	}, nil
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
	request agentcontext.MemorySearchRequest,
) ([]agentcontext.MemorySearchHit, error) {
	if searcher == nil || searcher.searcher == nil {
		return nil, errors.New(
			"bootstrap: Memory Context search dependency is required",
		)
	}
	hits, err := searcher.searcher.Search(ctx, memory.SearchRequest{
		Actor:                 request.Actor,
		Query:                 request.Query,
		GoalID:                request.GoalID,
		ExcludedCanonicalKeys: request.ExcludedCanonicalKeys,
		Limit:                 request.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]agentcontext.MemorySearchHit, 0, len(hits))
	for _, hit := range hits {
		result = append(result, agentcontext.MemorySearchHit{
			MemoryID:               hit.MemoryID,
			MemoryVersion:          hit.MemoryVersion,
			CanonicalKey:           hit.CanonicalKey,
			Type:                   string(hit.Type),
			Content:                hit.Content,
			Scope:                  string(hit.Scope),
			GoalID:                 hit.GoalID,
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

var _ agentcontext.MemorySearcher = (*agentMemoryContextSearcher)(nil)
var _ agentcontext.MemoryExtractionBarrier = (*agentMemoryExtractionBarrier)(nil)
