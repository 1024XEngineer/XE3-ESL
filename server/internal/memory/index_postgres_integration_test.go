package memory

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPostgresMemoryIndexLifecycleAndOwnerIsolation(t *testing.T) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	ctx := context.Background()
	actorA := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	actorB := requestcontext.Actor{
		UserID:    integrationUserB,
		SessionID: integrationSessionB,
	}
	item, err := repository.Create(
		ctx,
		actorA,
		createCommand("career.role", "Java backend engineer"),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	configuration := testIndexConfig()
	claim, acquired, err := repository.ClaimIndex(ctx, configuration)
	if err != nil {
		t.Fatalf("ClaimIndex: %v", err)
	}
	if !acquired ||
		claim.MemoryID != item.ID ||
		claim.MemoryVersion != item.Version {
		t.Fatalf("claim = %#v, acquired=%t", claim, acquired)
	}
	source, err := repository.ReadIndexSource(ctx, claim)
	if err != nil {
		t.Fatalf("ReadIndexSource: %v", err)
	}
	if source.Content != item.Content {
		t.Fatalf("source = %#v", source)
	}
	if _, err := repository.CompleteIndex(
		ctx,
		claim,
		validEmbeddingResult(),
	); err != nil {
		t.Fatalf("CompleteIndex: %v", err)
	}

	candidates, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		"",
		testSearchConfig(),
	)
	if err != nil {
		t.Fatalf("SearchCandidates: %v", err)
	}
	if len(candidates) != 1 ||
		candidates[0].Memory.ID != item.ID ||
		candidates[0].Similarity < 0.999 {
		t.Fatalf("candidates = %#v", candidates)
	}
	crossOwner, err := repository.SearchCandidates(
		ctx,
		actorB,
		validEmbeddingResult().Vectors[0],
		"",
		testSearchConfig(),
	)
	if err != nil {
		t.Fatalf("cross-owner SearchCandidates: %v", err)
	}
	if len(crossOwner) != 0 {
		t.Fatalf("cross-owner candidates = %#v", crossOwner)
	}

	updated, err := repository.Update(ctx, actorA, UpdateCommand{
		MemoryID:        item.ID,
		ExpectedVersion: item.Version,
		Content:         "Senior Java platform engineer",
		PolicyVersion:   "memory-policy-v1",
		Source: evidence(
			SourceAgentRun,
			"index-update-run",
			1,
			"updated-role",
		),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	stale, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		"",
		testSearchConfig(),
	)
	if err != nil {
		t.Fatalf("search stale vector: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale vector remained searchable: %#v", stale)
	}
	nextClaim, acquired, err := repository.ClaimIndex(ctx, configuration)
	if err != nil {
		t.Fatalf("ClaimIndex updated: %v", err)
	}
	if !acquired || nextClaim.MemoryVersion != updated.Version {
		t.Fatalf("updated claim = %#v, acquired=%t", nextClaim, acquired)
	}
	staleClaim := nextClaim
	staleClaim.LeaseToken = "f0000000-0000-4000-8000-000000000001"
	if _, err := repository.CompleteIndex(
		ctx,
		staleClaim,
		validEmbeddingResult(),
	); err != ErrConflict {
		t.Fatalf("stale worker CompleteIndex error = %v", err)
	}
	if _, err := repository.CompleteIndex(
		ctx,
		nextClaim,
		validEmbeddingResult(),
	); err != nil {
		t.Fatalf("CompleteIndex updated: %v", err)
	}
	if _, err := repository.Inactivate(ctx, actorA, InactivateCommand{
		MemoryID:        item.ID,
		ExpectedVersion: updated.Version,
		Source: evidence(
			SourceAgentRun,
			"index-inactivate-run",
			1,
			"forgot-role",
		),
	}); err != nil {
		t.Fatalf("Inactivate: %v", err)
	}
	inactive, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		"",
		testSearchConfig(),
	)
	if err != nil {
		t.Fatalf("search inactive: %v", err)
	}
	if len(inactive) != 0 {
		t.Fatalf("inactive Memory remained searchable: %#v", inactive)
	}
	if _, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		integrationMatterB,
		testSearchConfig(),
	); err != ErrNotFound {
		t.Fatalf("foreign Matter search error = %v", err)
	}
}
