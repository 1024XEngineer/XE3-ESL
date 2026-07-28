package memory

import (
	"context"
	"testing"
	"time"

	aifake "github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSearchServiceReranksDeterministicallyAndEnforcesMatter(
	t *testing.T,
) {
	t.Parallel()

	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	actor := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	userMemory := validSearchMemory(
		"10000000-0000-4000-8000-000000000001",
		TypeInterest,
		ScopeUser,
		"",
		now.Add(-24*time.Hour),
	)
	matterMemory := validSearchMemory(
		"20000000-0000-4000-8000-000000000001",
		TypeGoal,
		ScopeMatter,
		integrationMatterA,
		now.Add(-7*24*time.Hour),
	)
	repository := &fakeSearchRepository{candidates: []SearchCandidate{
		{Memory: userMemory, Similarity: 0.90},
		{Memory: matterMemory, Similarity: 0.82},
	}}
	service, err := NewSearchService(
		repository,
		&aifake.Embedder{Result: validEmbeddingResult()},
		testSearchConfig(),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	hits, err := service.Search(context.Background(), SearchRequest{
		Actor:    actor,
		Query:    "Help with my interview goal",
		MatterID: integrationMatterA,
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 ||
		hits[0].MemoryID != matterMemory.ID ||
		hits[0].RetrievalPolicyVersion != "memory-retrieval-v1" {
		t.Fatalf("hits = %#v", hits)
	}

	repository.candidates[1].Memory.MatterID = integrationMatterB
	if _, err := service.Search(context.Background(), SearchRequest{
		Actor:    actor,
		Query:    "Help with my interview goal",
		MatterID: integrationMatterA,
		Limit:    2,
	}); err != ErrRepository {
		t.Fatalf("cross-Matter error = %v", err)
	}
}

func TestSearchServiceDistinguishesNoMatchFromDependencyFailure(t *testing.T) {
	t.Parallel()

	service, err := NewSearchService(
		&fakeSearchRepository{},
		&aifake.Embedder{Result: validEmbeddingResult()},
		testSearchConfig(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	request := SearchRequest{
		Actor: requestcontext.Actor{
			UserID:    integrationUserA,
			SessionID: integrationSessionA,
		},
		Query: "unrelated query",
		Limit: 3,
	}
	hits, err := service.Search(context.Background(), request)
	if err != nil || len(hits) != 0 {
		t.Fatalf("legitimate empty result = %#v, err=%v", hits, err)
	}

	expected := ErrRepository
	service.repository = &fakeSearchRepository{err: expected}
	if _, err := service.Search(
		context.Background(),
		request,
	); err != expected {
		t.Fatalf("dependency error = %v", err)
	}
}

type fakeSearchRepository struct {
	candidates []SearchCandidate
	err        error
}

func (repository *fakeSearchRepository) SearchCandidates(
	context.Context,
	requestcontext.Actor,
	[]float32,
	string,
	SearchConfig,
) ([]SearchCandidate, error) {
	return repository.candidates, repository.err
}

func validSearchMemory(
	id string,
	memoryType Type,
	scope ScopeType,
	matterID string,
	updatedAt time.Time,
) Memory {
	return Memory{
		ID:            id,
		OwnerID:       integrationUserA,
		Type:          memoryType,
		CanonicalKey:  "goal.current",
		Content:       "Prepare for product interview",
		Scope:         scope,
		MatterID:      matterID,
		Status:        StatusActive,
		Version:       1,
		PolicyVersion: "memory-policy-v1",
		CreatedAt:     updatedAt.Add(-time.Hour),
		UpdatedAt:     updatedAt,
	}
}

func testSearchConfig() SearchConfig {
	return SearchConfig{
		Provider:               "qianwen",
		Model:                  "text-embedding-v4",
		Dimensions:             MemoryEmbeddingDimensions,
		EmbeddingPolicyVersion: "memory-embedding-v1",
		RetrievalPolicyVersion: "memory-retrieval-v1",
		CandidateLimit:         20,
		MinimumSimilarity:      0.25,
	}
}
