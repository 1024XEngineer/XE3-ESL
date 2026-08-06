package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MemoryIndexComposition struct {
	processor memory.IndexProcessor
	searcher  memory.Searcher
}

func NewMemoryIndexComposition(
	database *pgxpool.Pool,
	embedder memory.Embedder,
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
	processor, err := memory.NewIndexProcessor(
		repository,
		embedder,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
	)
	if err != nil {
		return nil, err
	}
	searcher, err := memory.NewSearcher(
		repository,
		embedder,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
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
