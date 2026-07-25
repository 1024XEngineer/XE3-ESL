package review

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	defaultGenerationLease = 30 * time.Second
	defaultPollInterval    = 10 * time.Millisecond
)

type EnsureService struct {
	repository   ReviewRepository
	sourceReader ReviewSourceReader
	generator    ReviewGenerator
	lease        time.Duration
	pollInterval time.Duration
}

func NewEnsureService(
	repository ReviewRepository,
	sourceReader ReviewSourceReader,
	generator ReviewGenerator,
) *EnsureService {
	return &EnsureService{
		repository:   repository,
		sourceReader: sourceReader,
		generator:    generator,
		lease:        defaultGenerationLease,
		pollInterval: defaultPollInterval,
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
	if current.Status == FormalReviewCompleted {
		return current, nil
	}

	for {
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
			return current, ErrGenerationFailed
		}

		timer := time.NewTimer(s.pollInterval)
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
		return FormalReview{}, s.fail(
			ctx,
			job,
			stableCategory(err, "generation_failed"),
			err,
		)
	}

	evidence, err := validateGenerated(source, generated)
	if err != nil {
		return FormalReview{}, s.fail(ctx, job, "invalid_result", err)
	}
	return s.repository.CompleteGeneration(
		ctx,
		job,
		generated.Result,
		evidence,
	)
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
