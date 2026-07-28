package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerCompletesAndClassifiesFailures(t *testing.T) {
	t.Parallel()

	source := validCompletedRunSource()
	claim := validExtractionClaim(source)
	repository := &fakeExtractionRepository{
		claims: []ExtractionClaim{claim},
	}
	extractor := &fakeCandidateExtractor{
		output: ExtractionOutput{Candidates: []ExtractedCandidate{{
			Action:       CandidateUpsert,
			Type:         TypeProfile,
			CanonicalKey: "career.role",
			Content:      "Java backend engineer",
			Scope:        ScopeUser,
			Evidence:     "Java backend engineer",
		}}},
	}
	policy, _ := NewExtractionPolicy(
		"memory-policy-v1",
		testExtractionConfig().TopicTTL,
		func() time.Time { return source.CompletedAt },
	)
	worker, err := NewWorker(
		repository,
		fakeCompletedRunReader{source: source},
		extractor,
		policy,
		testExtractionConfig(),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	result, err := worker.ProcessPending(context.Background(), 4)
	if err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 ||
		len(repository.completed) != 1 {
		t.Fatalf("result = %#v, repository = %#v", result, repository)
	}
}

func TestWorkerRetriesProviderFailureAndDiscardsMissingSource(t *testing.T) {
	t.Parallel()

	source := validCompletedRunSource()
	claim := validExtractionClaim(source)
	policy, _ := NewExtractionPolicy(
		"memory-policy-v1",
		testExtractionConfig().TopicTTL,
		func() time.Time { return source.CompletedAt },
	)
	t.Run("retry provider", func(t *testing.T) {
		repository := &fakeExtractionRepository{
			claims: []ExtractionClaim{claim},
		}
		worker, _ := NewWorker(
			repository,
			fakeCompletedRunReader{source: source},
			&fakeCandidateExtractor{err: errors.New("temporary")},
			policy,
			testExtractionConfig(),
		)
		result, err := worker.ProcessPending(context.Background(), 1)
		if err != nil {
			t.Fatalf("ProcessPending: %v", err)
		}
		if result.Retried != 1 ||
			repository.failedKind != "dependency" ||
			!repository.failedRetryable {
			t.Fatalf("result = %#v repo = %#v", result, repository)
		}
	})
	t.Run("discard missing source", func(t *testing.T) {
		repository := &fakeExtractionRepository{
			claims: []ExtractionClaim{claim},
		}
		worker, _ := NewWorker(
			repository,
			fakeCompletedRunReader{err: ErrNotFound},
			&fakeCandidateExtractor{},
			policy,
			testExtractionConfig(),
		)
		result, err := worker.ProcessPending(context.Background(), 1)
		if err != nil {
			t.Fatalf("ProcessPending: %v", err)
		}
		if result.Discarded != 1 ||
			repository.discardedKind != "source_not_found" {
			t.Fatalf("result = %#v repo = %#v", result, repository)
		}
	})
}

type fakeCompletedRunReader struct {
	source CompletedRunSource
	err    error
}

func (reader fakeCompletedRunReader) ReadCompletedRun(
	context.Context,
	string,
	string,
) (CompletedRunSource, error) {
	return reader.source, reader.err
}

type fakeCandidateExtractor struct {
	output ExtractionOutput
	err    error
}

func (extractor *fakeCandidateExtractor) Extract(
	context.Context,
	CompletedRunSource,
) (ExtractionOutput, error) {
	return extractor.output, extractor.err
}

type fakeExtractionRepository struct {
	claims          []ExtractionClaim
	completed       []ExtractionBatch
	failedKind      string
	failedRetryable bool
	discardedKind   string
}

func (repository *fakeExtractionRepository) ClaimExtraction(
	context.Context,
	ExtractionConfig,
) (ExtractionClaim, bool, error) {
	if len(repository.claims) == 0 {
		return ExtractionClaim{}, false, nil
	}
	claim := repository.claims[0]
	repository.claims = repository.claims[1:]
	return claim, true, nil
}

func (repository *fakeExtractionRepository) CompleteExtraction(
	_ context.Context,
	claim ExtractionClaim,
	batch ExtractionBatch,
) (ExtractionJob, error) {
	repository.completed = append(repository.completed, batch)
	job := claim.ExtractionJob
	job.Status = ExtractionCompleted
	return job, nil
}

func (repository *fakeExtractionRepository) FailExtraction(
	_ context.Context,
	claim ExtractionClaim,
	kind string,
	retryable bool,
	_ ExtractionConfig,
) (ExtractionJob, error) {
	repository.failedKind = kind
	repository.failedRetryable = retryable
	job := claim.ExtractionJob
	job.Status = ExtractionPending
	return job, nil
}

func (repository *fakeExtractionRepository) DiscardExtraction(
	_ context.Context,
	claim ExtractionClaim,
	kind string,
) (ExtractionJob, error) {
	repository.discardedKind = kind
	job := claim.ExtractionJob
	job.Status = ExtractionDiscarded
	return job, nil
}

func validExtractionClaim(source CompletedRunSource) ExtractionClaim {
	configuration := testExtractionConfig()
	return ExtractionClaim{ExtractionJob: ExtractionJob{
		RunID:              source.RunID,
		OwnerID:            source.OwnerID,
		ThreadID:           source.ThreadID,
		InputMessageID:     source.InputMessageID,
		AssistantMessageID: source.AssistantMessageID,
		SourceAttempt:      source.Attempt,
		SourceCompletedAt:  source.CompletedAt,
		Status:             ExtractionRunning,
		AttemptCount:       1,
		LeaseToken:         "a7000000-0000-4000-8000-000000000001",
		LeaseExpiresAt:     source.CompletedAt.Add(configuration.LeaseDuration),
		PolicyVersion:      configuration.PolicyVersion,
		PromptVersion:      configuration.PromptVersion,
		Provider:           configuration.Provider,
		Model:              configuration.Model,
	}}
}
