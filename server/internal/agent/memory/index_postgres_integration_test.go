package memory

import (
	"context"
	"fmt"
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

	searchConfiguration := testSearchConfig()
	candidates, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		"",
		nil,
		searchConfiguration,
	)
	if err != nil {
		t.Fatalf("SearchCandidates: %v", err)
	}
	if len(candidates) != 1 ||
		candidates[0].Memory.ID != item.ID ||
		candidates[0].Similarity < 0.999 {
		t.Fatalf("candidates = %#v", candidates)
	}
	rolloutConfiguration := configuration
	rolloutConfiguration.PolicyVersion = "memory-embedding-v2"
	rolloutClaim, acquired, err := repository.ClaimIndex(
		ctx,
		rolloutConfiguration,
	)
	if err != nil {
		t.Fatalf("ClaimIndex rollout: %v", err)
	}
	if !acquired ||
		rolloutClaim.MemoryID != item.ID ||
		rolloutClaim.MemoryVersion != item.Version ||
		rolloutClaim.PolicyVersion != rolloutConfiguration.PolicyVersion ||
		rolloutClaim.AttemptCount != 1 {
		t.Fatalf("rollout claim = %#v, acquired=%t", rolloutClaim, acquired)
	}
	if _, err := repository.CompleteIndex(
		ctx,
		rolloutClaim,
		validEmbeddingResult(),
	); err != nil {
		t.Fatalf("CompleteIndex rollout: %v", err)
	}
	searchConfiguration.EmbeddingPolicyVersion = rolloutConfiguration.PolicyVersion
	rolledOut, err := repository.SearchCandidates(
		ctx,
		actorA,
		validEmbeddingResult().Vectors[0],
		"",
		nil,
		searchConfiguration,
	)
	if err != nil {
		t.Fatalf("SearchCandidates rollout: %v", err)
	}
	if len(rolledOut) != 1 || rolledOut[0].Memory.ID != item.ID {
		t.Fatalf("rolled-out candidates = %#v", rolledOut)
	}
	if extra, acquired, err := repository.ClaimIndex(
		ctx,
		rolloutConfiguration,
	); err != nil || acquired {
		t.Fatalf(
			"idempotent rollout claim = %#v, acquired=%t, err=%v",
			extra,
			acquired,
			err,
		)
	}
	crossOwner, err := repository.SearchCandidates(
		ctx,
		actorB,
		validEmbeddingResult().Vectors[0],
		"",
		nil,
		searchConfiguration,
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
		nil,
		searchConfiguration,
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
		nil,
		searchConfiguration,
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
		integrationGoalB,
		nil,
		searchConfiguration,
	); err != ErrNotFound {
		t.Fatalf("foreign Goal search error = %v", err)
	}
}

func TestPostgresSearchCandidatesExcludesCanonicalKeysBeforeLimit(
	t *testing.T,
) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	ctx := context.Background()
	actor := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	item, err := repository.Create(
		ctx,
		actor,
		createCommand(CanonicalProfilePreferredName, "小花"),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claim, acquired, err := repository.ClaimIndex(ctx, testIndexConfig())
	if err != nil {
		t.Fatalf("ClaimIndex: %v", err)
	}
	if !acquired || claim.MemoryID != item.ID {
		t.Fatalf("claim = %#v, acquired=%t", claim, acquired)
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
		actor,
		validEmbeddingResult().Vectors[0],
		"",
		[]string{CanonicalProfilePreferredName},
		testSearchConfig(),
	)
	if err != nil {
		t.Fatalf("SearchCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("excluded candidates = %#v", candidates)
	}
}

func TestPostgresMemorySearchPrefiltersOwnerBeforeVectorRanking(t *testing.T) {
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
	target, err := repository.Create(
		ctx,
		actorA,
		createCommand("career.target", "Product manager interview"),
	)
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}
	targetVector := make([]float32, MemoryEmbeddingDimensions)
	targetVector[0] = 1
	foreignVector := make([]float32, MemoryEmbeddingDimensions)
	foreignVector[0] = 1
	foreignVector[1] = 0.001
	targetLiteral, err := vectorLiteral(targetVector, MemoryEmbeddingDimensions)
	if err != nil {
		t.Fatalf("target vector: %v", err)
	}
	foreignLiteral, err := vectorLiteral(foreignVector, MemoryEmbeddingDimensions)
	if err != nil {
		t.Fatalf("foreign vector: %v", err)
	}
	configuration := testSearchConfig()
	if _, err := database.Exec(ctx, `
INSERT INTO agent_memory_vectors (
    memory_id,
    owner_user_id,
    memory_version,
    provider,
    model,
    dimension,
    embedding_policy_version,
    embedding
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::public.vector)`,
		target.ID,
		target.OwnerID,
		target.Version,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
		configuration.EmbeddingPolicyVersion,
		targetLiteral,
	); err != nil {
		t.Fatalf("insert target vector: %v", err)
	}
	// Exceed pgvector's default HNSW ef_search candidate budget with
	// cross-owner vectors that are almost identical to the query.
	for index := 0; index < 64; index++ {
		memoryID := fmt.Sprintf(
			"c0000000-0000-4000-8000-%012x",
			index+1,
		)
		if _, err := database.Exec(ctx, `
WITH inserted_memory AS (
    INSERT INTO agent_memories (
        id,
        owner_user_id,
        memory_type,
        canonical_key,
        content,
        scope_type,
        status,
        version,
        policy_version
    ) VALUES (
        $1,
        $2,
        'interest',
        $3,
        $4,
        'user',
        'active',
        1,
        'memory-policy-v1'
    )
    RETURNING id, owner_user_id, version
)
INSERT INTO agent_memory_vectors (
    memory_id,
    owner_user_id,
    memory_version,
    provider,
    model,
    dimension,
    embedding_policy_version,
    embedding
)
SELECT
    inserted_memory.id,
    inserted_memory.owner_user_id,
    inserted_memory.version,
    $5,
    $6,
    $7,
    $8,
    $9::public.vector
FROM inserted_memory`,
			memoryID,
			integrationUserB,
			fmt.Sprintf("foreign.interest.%d", index),
			fmt.Sprintf("Foreign interest %d", index),
			configuration.Provider,
			configuration.Model,
			configuration.Dimensions,
			configuration.EmbeddingPolicyVersion,
			foreignLiteral,
		); err != nil {
			t.Fatalf("insert foreign vector %d: %v", index, err)
		}
	}
	configuration.CandidateLimit = 1
	candidates, err := repository.SearchCandidates(
		ctx,
		actorA,
		targetVector,
		"",
		nil,
		configuration,
	)
	if err != nil {
		t.Fatalf("SearchCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Memory.ID != target.ID {
		t.Fatalf("owner-prefiltered candidates = %#v", candidates)
	}
}
