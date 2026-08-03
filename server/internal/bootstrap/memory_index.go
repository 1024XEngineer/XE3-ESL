package bootstrap

import (
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	memoryEmbeddingPolicyVersion = "memory-embedding-v1"
	memoryRetrievalPolicyVersion = "memory-retrieval-v1"
	memoryIndexLease             = 2 * time.Minute
	memoryIndexRetries           = 3
	memorySearchCandidateLimit   = 20
	memoryMinimumSimilarity      = 0.25
)

type MemoryIndexComposition struct {
	processor memory.IndexProcessor
	searcher  memory.Searcher
}

func NewMemoryIndexComposition(
	database *pgxpool.Pool,
	embedder ai.Embedder,
	configuration config.EmbeddingConfig,
) (*MemoryIndexComposition, error) {
	if database == nil || embedder == nil {
		return nil, errors.New(
			"bootstrap: Memory index dependencies are required",
		)
	}
	repository, err := memory.NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		return nil, err
	}
	indexConfiguration := memory.IndexConfig{
		Provider:      configuration.Provider,
		Model:         configuration.Model,
		Dimensions:    configuration.Dimensions,
		PolicyVersion: memoryEmbeddingPolicyVersion,
		LeaseDuration: memoryIndexLease,
		MaxAttempts:   memoryIndexRetries,
	}
	processor, err := memory.NewIndexWorker(
		repository,
		embedder,
		indexConfiguration,
	)
	if err != nil {
		return nil, err
	}
	searcher, err := memory.NewSearchService(
		repository,
		embedder,
		memory.SearchConfig{
			Provider:               configuration.Provider,
			Model:                  configuration.Model,
			Dimensions:             configuration.Dimensions,
			EmbeddingPolicyVersion: memoryEmbeddingPolicyVersion,
			RetrievalPolicyVersion: memoryRetrievalPolicyVersion,
			CandidateLimit:         memorySearchCandidateLimit,
			MinimumSimilarity:      memoryMinimumSimilarity,
		},
		time.Now,
	)
	if err != nil {
		return nil, err
	}
	return &MemoryIndexComposition{
		processor: processor,
		searcher:  searcher,
	}, nil
}

func (composition *MemoryIndexComposition) Processor() memory.IndexProcessor {
	if composition == nil {
		return nil
	}
	return composition.processor
}

func (composition *MemoryIndexComposition) Searcher() memory.Searcher {
	if composition == nil {
		return nil
	}
	return composition.searcher
}
