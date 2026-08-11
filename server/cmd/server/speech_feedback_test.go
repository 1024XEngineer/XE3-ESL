package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

func TestSpeechFeedbackExecutionBudgetCoversProvidersAndPersistence(
	t *testing.T,
) {
	t.Parallel()
	budget := deriveSpeechFeedbackExecutionBudget(
		speechfeedback.SpeechFeedbackAudioReadTimeout,
		150*time.Second,
		60*time.Second,
	)
	if budget.processingTimeout != 4*time.Minute+30*time.Second {
		t.Fatalf(
			"processing timeout = %s, want %s",
			budget.processingTimeout,
			4*time.Minute+30*time.Second,
		)
	}
	if budget.leaseDuration != 5*time.Minute {
		t.Fatalf(
			"lease duration = %s, want %s",
			budget.leaseDuration,
			5*time.Minute,
		)
	}
	if budget.processingTimeout >= budget.leaseDuration {
		t.Fatalf(
			"processing timeout %s must end before lease %s",
			budget.processingTimeout,
			budget.leaseDuration,
		)
	}
}

func TestSpeechFeedbackWorkerAllowsAcousticsPastLegacySweepTimeout(
	t *testing.T,
) {
	t.Parallel()
	const simulatedAcousticDuration = 26 * time.Second
	budget := deriveSpeechFeedbackExecutionBudget(
		speechfeedback.SpeechFeedbackAudioReadTimeout,
		simulatedAcousticDuration,
		2*time.Second,
	)
	textProviderCalled := false
	processor := speechFeedbackProcessorFunc(func(
		ctx context.Context,
		_ int,
	) (speechfeedback.SpeechFeedbackSweepResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return speechfeedback.SpeechFeedbackSweepResult{},
				errors.New("processing context has no deadline")
		}
		if !deadline.After(time.Now().Add(simulatedAcousticDuration)) {
			return speechfeedback.SpeechFeedbackSweepResult{},
				context.DeadlineExceeded
		}
		textProviderCalled = true
		return speechfeedback.SpeechFeedbackSweepResult{
			Claimed:   1,
			Completed: 1,
		}, nil
	})
	worker, err := buildSpeechFeedbackWorker(
		processor,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		budget.processingTimeout,
	)
	if err != nil {
		t.Fatalf("buildSpeechFeedbackWorker: %v", err)
	}
	if claimed := worker.sweep(context.Background()); claimed != 1 {
		t.Fatalf("claimed = %d, want 1", claimed)
	}
	if !textProviderCalled {
		t.Fatal("text provider was truncated after acoustic evaluation")
	}
}

func TestSpeechFeedbackAcousticFallbackWarningWhenStorageDisabled(
	t *testing.T,
) {
	t.Parallel()
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	logSpeechFeedbackAcousticFallback(logger, false, true)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode warning: %v", err)
	}
	if record["level"] != "WARN" ||
		record["msg"] != "iFlytek ISE is configured but object storage is disabled; acoustic scoring is unavailable" ||
		record["fallback"] != "transcript_only" ||
		record["reason"] != "object_storage_disabled" {
		t.Fatalf("unexpected warning: %#v", record)
	}
}

type speechFeedbackProcessorFunc func(
	context.Context,
	int,
) (speechfeedback.SpeechFeedbackSweepResult, error)

func (processor speechFeedbackProcessorFunc) ProcessPending(
	ctx context.Context,
	limit int,
) (speechfeedback.SpeechFeedbackSweepResult, error) {
	return processor(ctx, limit)
}
