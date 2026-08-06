package memory

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSearchServiceReranksDeterministicallyAndEnforcesGoal(
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
	goalMemory := validSearchMemory(
		"20000000-0000-4000-8000-000000000001",
		TypeGoal,
		ScopeGoal,
		integrationGoalA,
		now.Add(-7*24*time.Hour),
	)
	repository := &fakeSearchRepository{candidates: []SearchCandidate{
		{Memory: userMemory, Similarity: 0.90},
		{Memory: goalMemory, Similarity: 0.82},
	}}
	service, err := NewSearchService(
		repository,
		&searchEmbedderStub{result: validEmbeddingResult()},
		testSearchConfig(),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	hits, err := service.Search(context.Background(), SearchRequest{
		Actor:  actor,
		Query:  "Help with my interview goal",
		GoalID: integrationGoalA,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 ||
		hits[0].MemoryID != goalMemory.ID ||
		hits[0].RetrievalPolicyVersion != "memory-retrieval-v1" {
		t.Fatalf("hits = %#v", hits)
	}

	repository.candidates[1].Memory.GoalID = integrationGoalB
	if _, err := service.Search(context.Background(), SearchRequest{
		Actor:  actor,
		Query:  "Help with my interview goal",
		GoalID: integrationGoalA,
		Limit:  2,
	}); err != ErrRepository {
		t.Fatalf("cross-Goal error = %v", err)
	}
}

func TestSearchServiceDistinguishesNoMatchFromDependencyFailure(t *testing.T) {
	t.Parallel()

	service, err := NewSearchService(
		&fakeSearchRepository{},
		&searchEmbedderStub{result: validEmbeddingResult()},
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

func TestSearchServiceExcludesStableProfileBeforeCandidateRanking(
	t *testing.T,
) {
	t.Parallel()
	repository := &fakeSearchRepository{}
	service, err := NewSearchService(
		repository,
		&searchEmbedderStub{result: validEmbeddingResult()},
		testSearchConfig(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	excluded := []string{CanonicalProfilePreferredName}
	if _, err := service.Search(context.Background(), SearchRequest{
		Actor: requestcontext.Actor{
			UserID:    integrationUserA,
			SessionID: integrationSessionA,
		},
		Query:                 "Who am I?",
		ExcludedCanonicalKeys: excluded,
		Limit:                 3,
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(repository.excludedCanonicalKeys) != 1 ||
		repository.excludedCanonicalKeys[0] != excluded[0] {
		t.Fatalf(
			"repository exclusions = %#v",
			repository.excludedCanonicalKeys,
		)
	}
}

type searchEmbedderStub struct {
	result EmbeddingResult
	err    error
}

func (embedder *searchEmbedderStub) Embed(
	_ context.Context,
	_ EmbeddingRequest,
) (EmbeddingResult, error) {
	return embedder.result, embedder.err
}

type fakeSearchRepository struct {
	candidates            []SearchCandidate
	excludedCanonicalKeys []string
	err                   error
}

func (repository *fakeSearchRepository) SearchCandidates(
	_ context.Context,
	_ requestcontext.Actor,
	_ []float32,
	_ string,
	excludedCanonicalKeys []string,
	_ SearchConfig,
) ([]SearchCandidate, error) {
	repository.excludedCanonicalKeys = append(
		[]string(nil),
		excludedCanonicalKeys...,
	)
	return repository.candidates, repository.err
}

func validSearchMemory(
	id string,
	memoryType Type,
	scope ScopeType,
	goalID string,
	updatedAt time.Time,
) Memory {
	return Memory{
		ID:            id,
		OwnerID:       integrationUserA,
		Type:          memoryType,
		CanonicalKey:  "goal.current",
		Content:       "Prepare for product interview",
		Scope:         scope,
		GoalID:        goalID,
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
