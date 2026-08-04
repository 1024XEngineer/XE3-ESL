package memory

import "time"

const (
	extractionPolicyVersion = "memory-policy-v2"
	extractionPromptVersion = "memory-extraction-v3"
	extractionLeaseDuration = 2 * time.Minute
	topicLifetime           = 30 * 24 * time.Hour
	extractionMaxAttempts   = 3

	embeddingPolicyVersion = "memory-embedding-v1"
	retrievalPolicyVersion = "memory-retrieval-v1"
	indexLeaseDuration     = 2 * time.Minute
	indexMaxAttempts       = 3
	searchCandidateLimit   = 20
	minimumSimilarity      = 0.25
	barrierMaximumWait     = 5 * time.Second
	barrierPollInterval    = 50 * time.Millisecond
)

// NewExtractionProcessor owns Memory's production extraction policy while
// accepting only the infrastructure and provider dependencies it needs.
func NewExtractionProcessor(
	repository ExtractionRepository,
	sources CompletedRunReader,
	generator Generator,
	provider string,
	model string,
) (ExtractionProcessor, error) {
	configuration := ExtractionConfig{
		Provider:      provider,
		Model:         model,
		PolicyVersion: extractionPolicyVersion,
		PromptVersion: extractionPromptVersion,
		LeaseDuration: extractionLeaseDuration,
		TopicTTL:      topicLifetime,
		MaxAttempts:   extractionMaxAttempts,
	}
	extractor, err := NewLLMExtractor(generator, configuration)
	if err != nil {
		return nil, err
	}
	policy, err := NewExtractionPolicy(
		configuration.PolicyVersion,
		configuration.TopicTTL,
		time.Now,
	)
	if err != nil {
		return nil, err
	}
	return NewWorker(
		repository,
		sources,
		extractor,
		policy,
		configuration,
	)
}

// NewExtractionBarrier owns the bounded consistency wait used before the
// first Agent response reads Memory.
func NewExtractionBarrier(
	reader ExtractionBarrierReader,
) (*ExtractionBarrierCoordinator, error) {
	return NewExtractionBarrierCoordinator(
		reader,
		SystemExtractionBarrierScheduler{},
		ExtractionBarrierWaitPolicy{
			MaximumWait:  barrierMaximumWait,
			PollInterval: barrierPollInterval,
		},
	)
}

// NewIndexProcessor owns Memory's production embedding lineage and retry
// policy. Provider identity remains an explicit startup dependency.
func NewIndexProcessor(
	repository IndexRepository,
	embedder Embedder,
	provider string,
	model string,
	dimensions int,
) (IndexProcessor, error) {
	return NewIndexWorker(repository, embedder, IndexConfig{
		Provider:      provider,
		Model:         model,
		Dimensions:    dimensions,
		PolicyVersion: embeddingPolicyVersion,
		LeaseDuration: indexLeaseDuration,
		MaxAttempts:   indexMaxAttempts,
	})
}

// NewSearcher owns Memory's production retrieval policy and audit lineage.
func NewSearcher(
	repository SearchCandidateRepository,
	embedder Embedder,
	provider string,
	model string,
	dimensions int,
) (Searcher, error) {
	return NewSearchService(repository, embedder, SearchConfig{
		Provider:               provider,
		Model:                  model,
		Dimensions:             dimensions,
		EmbeddingPolicyVersion: embeddingPolicyVersion,
		RetrievalPolicyVersion: retrievalPolicyVersion,
		CandidateLimit:         searchCandidateLimit,
		MinimumSimilarity:      minimumSimilarity,
	}, time.Now)
}
