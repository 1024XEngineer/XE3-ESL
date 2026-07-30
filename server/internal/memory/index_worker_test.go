package memory

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	aifake "github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
)

func TestIndexWorkerCompletesAndRetriesExplicitly(t *testing.T) {
	t.Parallel()

	claim := validIndexClaim()
	t.Run("complete", func(t *testing.T) {
		repository := &fakeIndexRepository{claims: []IndexClaim{claim}}
		worker, err := NewIndexWorker(
			repository,
			&aifake.Embedder{Result: validEmbeddingResult()},
			testIndexConfig(),
		)
		if err != nil {
			t.Fatalf("NewIndexWorker: %v", err)
		}
		result, err := worker.ProcessPendingIndexes(
			context.Background(),
			4,
		)
		if err != nil {
			t.Fatalf("ProcessPendingIndexes: %v", err)
		}
		if result.Completed != 1 || repository.completed != 1 {
			t.Fatalf("result=%#v repository=%#v", result, repository)
		}
	})

	t.Run("retry provider", func(t *testing.T) {
		repository := &fakeIndexRepository{claims: []IndexClaim{claim}}
		worker, _ := NewIndexWorker(
			repository,
			&aifake.Embedder{Err: ai.NewEmbeddingError(
				ai.ErrorRateLimited,
				429,
				"",
				"",
				nil,
			)},
			testIndexConfig(),
		)
		result, err := worker.ProcessPendingIndexes(
			context.Background(),
			1,
		)
		if err != nil {
			t.Fatalf("ProcessPendingIndexes: %v", err)
		}
		if result.Retried != 1 ||
			repository.failureKind != "rate_limited" ||
			!repository.retryable {
			t.Fatalf("result=%#v repository=%#v", result, repository)
		}
	})

	t.Run("discard superseded", func(t *testing.T) {
		repository := &fakeIndexRepository{
			claims:    []IndexClaim{claim},
			sourceErr: ErrNotFound,
		}
		worker, _ := NewIndexWorker(
			repository,
			&aifake.Embedder{Result: validEmbeddingResult()},
			testIndexConfig(),
		)
		result, err := worker.ProcessPendingIndexes(
			context.Background(),
			1,
		)
		if err != nil {
			t.Fatalf("ProcessPendingIndexes: %v", err)
		}
		if result.Discarded != 1 ||
			repository.discardKind != "superseded" {
			t.Fatalf("result=%#v repository=%#v", result, repository)
		}
	})
}

type fakeIndexRepository struct {
	claims      []IndexClaim
	sourceErr   error
	completed   int
	failureKind string
	retryable   bool
	discardKind string
}

func (repository *fakeIndexRepository) ClaimIndex(
	context.Context,
	IndexConfig,
) (IndexClaim, bool, error) {
	if len(repository.claims) == 0 {
		return IndexClaim{}, false, nil
	}
	claim := repository.claims[0]
	repository.claims = repository.claims[1:]
	return claim, true, nil
}

func (repository *fakeIndexRepository) ReadIndexSource(
	context.Context,
	IndexClaim,
) (IndexSource, error) {
	if repository.sourceErr != nil {
		return IndexSource{}, repository.sourceErr
	}
	return IndexSource{
		MemoryID: validIndexClaim().MemoryID,
		OwnerID:  validIndexClaim().OwnerID,
		Version:  1,
		Content:  "Java backend engineer",
	}, nil
}

func (repository *fakeIndexRepository) CompleteIndex(
	_ context.Context,
	claim IndexClaim,
	_ ai.EmbeddingResult,
) (IndexJob, error) {
	repository.completed++
	job := claim.IndexJob
	job.Status = IndexCompleted
	return job, nil
}

func (repository *fakeIndexRepository) FailIndex(
	_ context.Context,
	claim IndexClaim,
	kind string,
	retryable bool,
	_ IndexConfig,
) (IndexJob, error) {
	repository.failureKind = kind
	repository.retryable = retryable
	job := claim.IndexJob
	job.Status = IndexPending
	return job, nil
}

func (repository *fakeIndexRepository) DiscardIndex(
	_ context.Context,
	claim IndexClaim,
	kind string,
) (IndexJob, error) {
	repository.discardKind = kind
	job := claim.IndexJob
	job.Status = IndexDiscarded
	return job, nil
}

func testIndexConfig() IndexConfig {
	return IndexConfig{
		Provider:      "qianwen",
		Model:         "text-embedding-v4",
		Dimensions:    MemoryEmbeddingDimensions,
		PolicyVersion: "memory-embedding-v1",
		LeaseDuration: 2 * time.Minute,
		MaxAttempts:   3,
	}
}

func validIndexClaim() IndexClaim {
	now := time.Now().UTC()
	return IndexClaim{IndexJob: IndexJob{
		MemoryID:       "10000000-0000-4000-8000-000000000001",
		OwnerID:        integrationUserA,
		MemoryVersion:  1,
		Status:         IndexRunning,
		AttemptCount:   1,
		LeaseToken:     "20000000-0000-4000-8000-000000000001",
		LeaseExpiresAt: now.Add(time.Minute),
		NextAttemptAt:  now,
		PolicyVersion:  "memory-embedding-v1",
		Provider:       "qianwen",
		Model:          "text-embedding-v4",
		Dimensions:     MemoryEmbeddingDimensions,
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
}

func validEmbeddingResult() ai.EmbeddingResult {
	vector := make([]float32, MemoryEmbeddingDimensions)
	vector[0] = 1
	return ai.EmbeddingResult{
		Provider:    "qianwen",
		Model:       "text-embedding-v4",
		Dimensions:  MemoryEmbeddingDimensions,
		Vectors:     [][]float32{vector},
		InputTokens: 3,
		TotalTokens: 3,
	}
}
