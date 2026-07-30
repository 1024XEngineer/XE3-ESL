package bootstrap

import (
	"context"
	"errors"
	"testing"

	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAgentMemoryContextSearcherPreservesSearchAuditFields(t *testing.T) {
	t.Parallel()
	delegate := &recordingDomainMemorySearcher{
		hits: []memory.SearchHit{{
			MemoryID:               "10000000-0000-4000-8000-000000000001",
			MemoryVersion:          3,
			CanonicalKey:           "goal.current",
			Type:                   memory.TypeGoal,
			Content:                "Prepare for a product interview",
			Scope:                  memory.ScopeMatter,
			MatterID:               "20000000-0000-4000-8000-000000000001",
			Similarity:             0.91,
			Score:                  0.87,
			EmbeddingProvider:      "qianwen",
			EmbeddingModel:         "text-embedding-v4",
			EmbeddingDimensions:    memory.MemoryEmbeddingDimensions,
			EmbeddingPolicyVersion: "memory-embedding-v1",
			RetrievalPolicyVersion: "memory-retrieval-v1",
		}},
	}
	adapter, err := newAgentMemoryContextSearcher(delegate)
	if err != nil {
		t.Fatalf("newAgentMemoryContextSearcher: %v", err)
	}
	request := agentruntime.MemorySearchRequest{
		Actor: requestcontext.Actor{
			UserID:    "30000000-0000-4000-8000-000000000001",
			SessionID: "40000000-0000-4000-8000-000000000001",
		},
		Query:    "Help me prepare",
		MatterID: delegate.hits[0].MatterID,
		ExcludedCanonicalKeys: []string{
			memory.CanonicalProfilePreferredName,
		},
		Limit: 6,
	}
	hits, err := adapter.Search(context.Background(), request)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if delegate.request.Actor != request.Actor ||
		delegate.request.Query != request.Query ||
		delegate.request.MatterID != request.MatterID ||
		len(delegate.request.ExcludedCanonicalKeys) != 1 ||
		delegate.request.ExcludedCanonicalKeys[0] !=
			request.ExcludedCanonicalKeys[0] ||
		delegate.request.Limit != request.Limit {
		t.Fatalf("domain request = %#v", delegate.request)
	}
	if len(hits) != 1 ||
		hits[0].MemoryID != delegate.hits[0].MemoryID ||
		hits[0].MemoryVersion != delegate.hits[0].MemoryVersion ||
		hits[0].CanonicalKey != delegate.hits[0].CanonicalKey ||
		hits[0].Type != string(delegate.hits[0].Type) ||
		hits[0].Scope != string(delegate.hits[0].Scope) ||
		hits[0].EmbeddingPolicyVersion !=
			delegate.hits[0].EmbeddingPolicyVersion ||
		hits[0].RetrievalPolicyVersion !=
			delegate.hits[0].RetrievalPolicyVersion {
		t.Fatalf("Agent Memory hits = %#v", hits)
	}
}

func TestAgentMemoryContextSearcherRequiresDependencyAndPropagatesFailure(
	t *testing.T,
) {
	t.Parallel()
	if adapter, err := newAgentMemoryContextSearcher(nil); err == nil ||
		adapter != nil {
		t.Fatalf("nil adapter = %#v, %v", adapter, err)
	}
	dependencyError := errors.New("embedding unavailable")
	adapter, err := newAgentMemoryContextSearcher(
		&recordingDomainMemorySearcher{err: dependencyError},
	)
	if err != nil {
		t.Fatalf("newAgentMemoryContextSearcher: %v", err)
	}
	if _, err := adapter.Search(
		context.Background(),
		agentruntime.MemorySearchRequest{},
	); !errors.Is(err, dependencyError) {
		t.Fatalf("Search error = %v", err)
	}
}

type recordingDomainMemorySearcher struct {
	request memory.SearchRequest
	hits    []memory.SearchHit
	err     error
}

func (searcher *recordingDomainMemorySearcher) Search(
	_ context.Context,
	request memory.SearchRequest,
) ([]memory.SearchHit, error) {
	searcher.request = request
	return append([]memory.SearchHit(nil), searcher.hits...), searcher.err
}
