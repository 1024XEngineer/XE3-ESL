package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestEvaluationShadowWorkerDrainsFullBatch(t *testing.T) {
	t.Parallel()
	secondSweep := make(chan struct{})
	calls := 0
	processor := evaluationShadowProcessorFunc(func(
		context.Context,
		int,
	) (scoring.InterviewShadowSweepResult, error) {
		calls++
		if calls == 1 {
			return scoring.InterviewShadowSweepResult{
				Claimed: 1,
			}, nil
		}
		close(secondSweep)
		return scoring.InterviewShadowSweepResult{}, nil
	})
	worker, err := newEvaluationShadowWorker(
		processor,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Hour,
		time.Second,
		1,
	)
	if err != nil {
		t.Fatalf("newEvaluationShadowWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	select {
	case <-secondSweep:
	case <-time.After(time.Second):
		t.Fatal("worker waited after a full batch")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestEvaluationShadowWorkerRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	processor := evaluationShadowProcessorFunc(func(
		context.Context,
		int,
	) (scoring.InterviewShadowSweepResult, error) {
		return scoring.InterviewShadowSweepResult{}, nil
	})
	tests := []struct {
		name         string
		processor    evaluationShadowProcessor
		logger       *slog.Logger
		interval     time.Duration
		sweepTimeout time.Duration
		claimLimit   int
	}{
		{
			name:         "nil processor",
			logger:       logger,
			interval:     time.Second,
			sweepTimeout: time.Second,
			claimLimit:   1,
		},
		{
			name:         "nil logger",
			processor:    processor,
			interval:     time.Second,
			sweepTimeout: time.Second,
			claimLimit:   1,
		},
		{
			name:         "zero interval",
			processor:    processor,
			logger:       logger,
			sweepTimeout: time.Second,
			claimLimit:   1,
		},
		{
			name:       "zero timeout",
			processor:  processor,
			logger:     logger,
			interval:   time.Second,
			claimLimit: 1,
		},
		{
			name:         "zero limit",
			processor:    processor,
			logger:       logger,
			interval:     time.Second,
			sweepTimeout: time.Second,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newEvaluationShadowWorker(
				test.processor,
				test.logger,
				test.interval,
				test.sweepTimeout,
				test.claimLimit,
			); err == nil {
				t.Fatal("invalid dependency was accepted")
			}
		})
	}
}

func TestEvaluationShadowErrorKindIsStable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want string
	}{
		{err: context.Canceled, want: "canceled"},
		{err: context.DeadlineExceeded, want: "deadline_exceeded"},
		{
			err:  scoring.ErrRuntimeLeaseLost,
			want: "lease_lost",
		},
		{
			err:  scoring.ErrRuntimeConfigurationConflict,
			want: "configuration_conflict",
		},
		{err: evaluation.ErrInvalidRequest, want: "invalid_state"},
	}
	for _, test := range tests {
		if got := evaluationShadowErrorKind(test.err); got != test.want {
			t.Fatalf(
				"evaluationShadowErrorKind(%v) = %q, want %q",
				test.err,
				got,
				test.want,
			)
		}
	}
}

type evaluationShadowProcessorFunc func(
	context.Context,
	int,
) (scoring.InterviewShadowSweepResult, error)

func (processor evaluationShadowProcessorFunc) ProcessPending(
	ctx context.Context,
	limit int,
) (scoring.InterviewShadowSweepResult, error) {
	return processor(ctx, limit)
}
