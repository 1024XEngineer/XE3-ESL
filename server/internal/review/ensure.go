package review

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	defaultGenerationLease           = 30 * time.Second
	defaultGenerationFinalizeTimeout = 5 * time.Second
	defaultPollInterval              = 20 * time.Millisecond
	defaultMaxPollInterval           = 500 * time.Millisecond
)

type EnsureService struct {
	repository      ReviewRepository
	sourceReader    ReviewSourceReader
	generator       ReviewGenerator
	lease           time.Duration
	finalizeTimeout time.Duration
	pollInterval    time.Duration
	maxPollInterval time.Duration
}

func NewEnsureService(
	repository ReviewRepository,
	sourceReader ReviewSourceReader,
	generator ReviewGenerator,
) *EnsureService {
	return &EnsureService{
		repository:      repository,
		sourceReader:    sourceReader,
		generator:       generator,
		lease:           defaultGenerationLease,
		finalizeTimeout: defaultGenerationFinalizeTimeout,
		pollInterval:    defaultPollInterval,
		maxPollInterval: defaultMaxPollInterval,
	}
}

// EnsureReview returns the one formal Review for actor, Practice Session and
// implementation version. Concurrent callers either own the persisted attempt
// claim or recover the result written by that owner.
func (s *EnsureService) EnsureReview(
	ctx context.Context,
	command EnsureReviewCommand,
) (FormalReview, error) {
	if s == nil || s.repository == nil || s.sourceReader == nil ||
		s.generator == nil {
		return FormalReview{}, ErrInvalidReview
	}
	if err := command.validate(); err != nil {
		return FormalReview{}, err
	}

	current, err := s.repository.EnsurePending(ctx, command)
	if err != nil {
		return FormalReview{}, err
	}
	pollAttempt := 0
	for {
		if current.Status == FormalReviewCompleted {
			return current, nil
		}
		if current.Status == FormalReviewFailed &&
			terminalGenerationCategory(current.StableErrorCategory) {
			return current, failedGenerationError(
				current.StableErrorCategory,
			)
		}

		current, claim, claimed, err := s.repository.ClaimGeneration(
			ctx,
			command.Actor,
			current.ID,
			s.lease,
		)
		if err != nil {
			return FormalReview{}, err
		}
		if claimed {
			return s.generate(ctx, command, claim)
		}
		if current.Status == FormalReviewCompleted {
			return current, nil
		}
		if current.Status == FormalReviewFailed {
			return current, failedGenerationError(
				current.StableErrorCategory,
			)
		}

		timer := time.NewTimer(s.pollDelay(pollAttempt))
		pollAttempt++
		select {
		case <-ctx.Done():
			timer.Stop()
			return FormalReview{}, ctx.Err()
		case <-timer.C:
		}
		current, err = s.repository.Get(ctx, command.Actor, current.ID)
		if err != nil {
			return FormalReview{}, err
		}
	}
}

func (s *EnsureService) pollDelay(attempt int) time.Duration {
	base := s.pollInterval
	if base <= 0 {
		base = defaultPollInterval
	}
	maximum := s.maxPollInterval
	if maximum <= 0 {
		maximum = defaultMaxPollInterval
	}
	if maximum < base {
		maximum = base
	}

	delay := base
	for step := 0; step < attempt && delay < maximum; step++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}

	// Spread concurrent waiters across the final quarter of each backoff
	// window so they do not repeatedly contend for the same advisory lock.
	jitterWindow := delay / 4
	if jitterWindow <= 0 {
		return delay
	}
	return delay - jitterWindow + rand.N(jitterWindow+1)
}

func (s *EnsureService) generate(
	ctx context.Context,
	command EnsureReviewCommand,
	job GenerationJobContext,
) (FormalReview, error) {
	source, err := s.sourceReader.ReadReviewSource(
		ctx,
		command.Actor,
		command.PracticeSessionID,
	)
	if err != nil {
		return FormalReview{}, s.fail(ctx, job, "source_unavailable", err)
	}
	if err := validateSource(command, source); err != nil {
		return FormalReview{}, s.fail(ctx, job, "invalid_source", err)
	}

	generated, err := s.generator.GenerateReview(ctx, ReviewGenerationInput{
		ReviewID:              job.ReviewID,
		ImplementationVersion: command.ImplementationVersion,
		Source:                source,
	})
	if err != nil {
		return FormalReview{}, s.finalizeFailure(
			ctx,
			job,
			stableCategory(err, "generation_failed"),
			err,
		)
	}

	evidence, err := validateGenerated(source, generated)
	if err != nil {
		return FormalReview{}, s.finalizeFailure(
			ctx,
			job,
			"invalid_result",
			err,
		)
	}
	finalizeCtx, cancel := s.finalizationContext(ctx)
	defer cancel()
	return s.repository.CompleteGeneration(
		finalizeCtx,
		job,
		generated.Result,
		evidence,
	)
}

func (s *EnsureService) finalizeFailure(
	ctx context.Context,
	job GenerationJobContext,
	category string,
	cause error,
) error {
	finalizeCtx, cancel := s.finalizationContext(ctx)
	defer cancel()
	return s.fail(finalizeCtx, job, category, cause)
}

func (s *EnsureService) finalizationContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	timeout := s.finalizeTimeout
	if timeout <= 0 {
		timeout = defaultGenerationFinalizeTimeout
	}
	// The Provider has already returned at every call site. Preserve request
	// values for observability, but give the authoritative terminal write a
	// short window to survive a disconnected client.
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

func (s *EnsureService) fail(
	ctx context.Context,
	job GenerationJobContext,
	category string,
	cause error,
) error {
	if err := s.repository.FailGeneration(ctx, job, category); err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(ErrGenerationFailed, cause)
}

func stableCategory(err error, fallback string) string {
	var categorized StableGenerationError
	if errors.As(err, &categorized) {
		if category := strings.TrimSpace(categorized.StableCategory()); validStableErrorCategory(category) {
			return category
		}
	}
	return fallback
}
