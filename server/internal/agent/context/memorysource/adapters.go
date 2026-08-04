package memorysource

import (
	"context"
	"errors"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
)

type Searcher struct {
	searcher memory.Searcher
}

func NewSearcher(searcher memory.Searcher) (*Searcher, error) {
	if searcher == nil {
		return nil, errors.New("Memory Context search dependency is required")
	}
	return &Searcher{searcher: searcher}, nil
}

func (searcher *Searcher) Search(
	ctx context.Context,
	request agentcontext.MemorySearchRequest,
) ([]agentcontext.MemorySearchHit, error) {
	if searcher == nil || searcher.searcher == nil {
		return nil, errors.New("Memory Context search dependency is required")
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

type extractionBarrier interface {
	Await(
		context.Context,
		memory.ExtractionBarrierRequest,
	) (memory.ExtractionBarrierResult, error)
}

type ExtractionBarrier struct {
	barrier extractionBarrier
}

func NewExtractionBarrier(
	barrier extractionBarrier,
) (*ExtractionBarrier, error) {
	if barrier == nil {
		return nil, errors.New("Memory Extraction Barrier is required")
	}
	return &ExtractionBarrier{barrier: barrier}, nil
}

func (barrier *ExtractionBarrier) Await(
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

type StableProfileReader struct {
	reader memory.StableProfileReader
}

func NewStableProfileReader(
	reader memory.StableProfileReader,
) (*StableProfileReader, error) {
	if reader == nil {
		return nil, errors.New("Stable Profile read dependency is required")
	}
	return &StableProfileReader{reader: reader}, nil
}

func (reader *StableProfileReader) ReadStableProfile(
	ctx context.Context,
	request agentcontext.StableProfileReadRequest,
) ([]agentcontext.StableProfileMemory, error) {
	if reader == nil || reader.reader == nil {
		return nil, errors.New("Stable Profile read dependency is required")
	}
	if ctx == nil || !request.Valid() {
		return nil, memory.ErrInvalidArgument
	}
	items, err := reader.reader.ListStableProfile(ctx, request.Actor)
	if err != nil {
		return nil, err
	}
	if !memory.ValidStableProfileMemories(items, request.Actor.UserID) {
		return nil, memory.ErrRepository
	}
	result := make([]agentcontext.StableProfileMemory, 0, len(items))
	for _, item := range items {
		mapped := agentcontext.StableProfileMemory{
			MemoryID:      item.ID,
			MemoryVersion: item.Version,
			CanonicalKey:  item.CanonicalKey,
			Type:          string(item.Type),
			Content:       item.Content,
			Scope:         string(item.Scope),
		}
		if !mapped.Valid() {
			return nil, memory.ErrRepository
		}
		result = append(result, mapped)
	}
	return result, nil
}

var (
	_ agentcontext.MemorySearcher          = (*Searcher)(nil)
	_ agentcontext.MemoryExtractionBarrier = (*ExtractionBarrier)(nil)
	_ agentcontext.StableProfileReader     = (*StableProfileReader)(nil)
)
