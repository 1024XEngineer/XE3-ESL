package review_test

import (
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

var testTime = time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)

func TestNewTurnAnalysisRejectsMissingTurnID(t *testing.T) {
	_, err := review.NewTurnAnalysis("analysis-1", "", "evaluator-v1", testTime)
	if !errors.Is(err, review.ErrInvalidAnalysis) {
		t.Fatalf("expected ErrInvalidAnalysis, got %v", err)
	}
}

func TestTurnAnalysisCompleteRequiresResult(t *testing.T) {
	analysis := mustPendingAnalysis(t)

	if err := analysis.Complete(0, " ", "", testTime.Add(time.Minute)); !errors.Is(err, review.ErrInvalidAnalysis) {
		t.Fatalf("expected blank summary to fail, got %v", err)
	}
	if analysis.Status != review.AnalysisStatusPending {
		t.Fatalf("failed transition changed status to %q", analysis.Status)
	}

	completedAt := testTime.Add(time.Minute)
	if err := analysis.Complete(0, "The answer is relevant.", "scoring transcript", completedAt); err != nil {
		t.Fatalf("complete analysis: %v", err)
	}
	if analysis.Status != review.AnalysisStatusCompleted {
		t.Fatalf("expected completed, got %q", analysis.Status)
	}
	if analysis.Score == nil || *analysis.Score != 0 {
		t.Fatalf("expected valid zero score, got %#v", analysis.Score)
	}
	if analysis.CompletedAt == nil || !analysis.CompletedAt.Equal(completedAt) {
		t.Fatalf("unexpected completed_at: %#v", analysis.CompletedAt)
	}
	if err := analysis.Validate(); err != nil {
		t.Fatalf("completed analysis should validate: %v", err)
	}
	if err := analysis.Complete(1, "again", "", completedAt); !errors.Is(err, review.ErrInvalidStateChange) {
		t.Fatalf("expected terminal analysis to reject second completion, got %v", err)
	}
}

func TestTurnAnalysisFailedRequiresReason(t *testing.T) {
	analysis := mustPendingAnalysis(t)

	if err := analysis.Fail(" ", testTime.Add(time.Minute)); !errors.Is(err, review.ErrInvalidAnalysis) {
		t.Fatalf("expected blank reason to fail, got %v", err)
	}
	if analysis.Status != review.AnalysisStatusPending {
		t.Fatalf("failed transition changed status to %q", analysis.Status)
	}

	failedAt := testTime.Add(time.Minute)
	if err := analysis.Fail("provider unavailable", failedAt); err != nil {
		t.Fatalf("fail analysis: %v", err)
	}
	if analysis.Status != review.AnalysisStatusFailed || analysis.FailureReason != "provider unavailable" {
		t.Fatalf("unexpected failed analysis: %#v", analysis)
	}
	if analysis.FailedAt == nil || !analysis.FailedAt.Equal(failedAt) {
		t.Fatalf("unexpected failed_at: %#v", analysis.FailedAt)
	}
	if err := analysis.Validate(); err != nil {
		t.Fatalf("failed analysis should validate: %v", err)
	}
}

func TestTurnAnalysisFailedTransitionDoesNotMutateReceiver(t *testing.T) {
	analysis := mustPendingAnalysis(t)
	before := analysis

	err := analysis.Complete(80, "valid summary", "", testTime.Add(-time.Minute))
	if !errors.Is(err, review.ErrInvalidAnalysis) {
		t.Fatalf("expected invalid completion time to fail, got %v", err)
	}
	if analysis != before {
		t.Fatalf("failed transition mutated analysis: before %#v after %#v", before, analysis)
	}
}

func TestTurnAnalysisKeyUsesTurnAndEvaluatorVersion(t *testing.T) {
	analysis := mustPendingAnalysis(t)
	want := (review.AnalysisKey{TurnID: "turn-1", EvaluatorVersion: "evaluator-v1"})
	if got := analysis.Key(); got != want {
		t.Fatalf("unexpected analysis key: got %#v want %#v", got, want)
	}
}

func TestEvidenceValidate(t *testing.T) {
	negative := int64(-1)
	zero := int64(0)
	ten := int64(10)
	twenty := int64(20)

	tests := []struct {
		name    string
		value   review.Evidence
		wantErr bool
	}{
		{name: "text only", value: review.Evidence{TranscriptText: "I led the migration."}},
		{name: "range only", value: review.Evidence{StartMS: &zero, EndMS: &ten}},
		{name: "text and range", value: review.Evidence{TranscriptText: "I led it.", StartMS: &ten, EndMS: &twenty}},
		{name: "empty", value: review.Evidence{}, wantErr: true},
		{name: "negative", value: review.Evidence{StartMS: &negative, EndMS: &ten}, wantErr: true},
		{name: "reversed", value: review.Evidence{StartMS: &twenty, EndMS: &ten}, wantErr: true},
		{name: "missing end", value: review.Evidence{StartMS: &zero}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.value.Validate()
			if tt.wantErr && !errors.Is(err, review.ErrInvalidEvidence) {
				t.Fatalf("expected ErrInvalidEvidence, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected valid evidence, got %v", err)
			}
		})
	}
}

func TestNewFeedbackItemCopiesEvidence(t *testing.T) {
	startMS := int64(100)
	endMS := int64(200)
	evidence := []review.Evidence{{
		TranscriptText: "I reduced latency by 30%.",
		StartMS:        &startMS,
		EndMS:          &endMS,
	}}
	item, err := review.NewFeedbackItem(
		"feedback-1",
		"analysis-1",
		review.FeedbackCategory("evidence"),
		"The answer includes a measurable result.",
		"Keep the metric in the final answer.",
		evidence,
		true,
		testTime,
	)
	if err != nil {
		t.Fatalf("new feedback: %v", err)
	}

	evidence[0].TranscriptText = "changed by caller"
	startMS = -1
	endMS = 50
	if item.Evidence[0].TranscriptText != "I reduced latency by 30%." {
		t.Fatalf("feedback evidence changed through caller slice: %#v", item.Evidence)
	}
	if *item.Evidence[0].StartMS != 100 || *item.Evidence[0].EndMS != 200 {
		t.Fatalf("feedback evidence range changed through caller pointers: %#v", item.Evidence)
	}
	if err := item.Validate(); err != nil {
		t.Fatalf("feedback should validate: %v", err)
	}
}

func TestNewRetryRequestRejectsNonRetryableFeedback(t *testing.T) {
	feedback := mustFeedback(t, false)
	_, err := review.NewRetryRequest("retry-1", "turn-1", feedback, testTime)
	if !errors.Is(err, review.ErrFeedbackNotRetryable) {
		t.Fatalf("expected ErrFeedbackNotRetryable, got %v", err)
	}
}

func TestRetryRequestTransitions(t *testing.T) {
	t.Run("turn created", func(t *testing.T) {
		request := mustRetryRequest(t)
		if err := request.MarkTurnCreated("", testTime.Add(time.Minute)); !errors.Is(err, review.ErrInvalidRetryRequest) {
			t.Fatalf("expected missing new turn to fail, got %v", err)
		}
		if request.Status != review.RetryStatusPending {
			t.Fatalf("failed transition changed status to %q", request.Status)
		}

		if err := request.MarkTurnCreated("turn-2", testTime.Add(time.Minute)); err != nil {
			t.Fatalf("mark turn created: %v", err)
		}
		if request.Status != review.RetryStatusTurnCreated || request.NewTurnID != "turn-2" {
			t.Fatalf("unexpected request: %#v", request)
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("created request should validate: %v", err)
		}
	})

	t.Run("failed", func(t *testing.T) {
		request := mustRetryRequest(t)
		if err := request.MarkFailed(" ", testTime.Add(time.Minute)); !errors.Is(err, review.ErrInvalidRetryRequest) {
			t.Fatalf("expected missing reason to fail, got %v", err)
		}
		if request.Status != review.RetryStatusPending {
			t.Fatalf("failed transition changed status to %q", request.Status)
		}

		if err := request.MarkFailed("conversation unavailable", testTime.Add(time.Minute)); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
		if request.Status != review.RetryStatusFailed || request.FailureReason != "conversation unavailable" {
			t.Fatalf("unexpected request: %#v", request)
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("failed request should validate: %v", err)
		}
	})
}

func TestRetryFailedTransitionDoesNotMutateReceiver(t *testing.T) {
	request := mustRetryRequest(t)
	before := request

	err := request.MarkTurnCreated("turn-2", testTime.Add(-time.Minute))
	if !errors.Is(err, review.ErrInvalidRetryRequest) {
		t.Fatalf("expected backwards timestamp to fail, got %v", err)
	}
	if request != before {
		t.Fatalf("failed transition mutated request: before %#v after %#v", before, request)
	}
}

func TestNewHistoryRecordUsesStableSourceIDs(t *testing.T) {
	analysis := mustPendingAnalysis(t)
	if err := analysis.Complete(72, "The answer needs a clearer result.", "", testTime.Add(time.Minute)); err != nil {
		t.Fatalf("complete analysis: %v", err)
	}

	record, err := review.NewHistoryRecord("history-1", "session-1", analysis, testTime.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("new history record: %v", err)
	}
	wantKey := (review.HistoryKey{SessionID: "session-1", TurnID: "turn-1", AnalysisID: "analysis-1"})
	if got := record.Key(); got != wantKey {
		t.Fatalf("unexpected history key: got %#v want %#v", got, wantKey)
	}
	if record.Score == nil || *record.Score != 72 {
		t.Fatalf("unexpected projected score: %#v", record.Score)
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("history record should validate: %v", err)
	}
}

func TestNewHistoryRecordRejectsReviewBeforeAnalysisCompletion(t *testing.T) {
	analysis := mustPendingAnalysis(t)
	if err := analysis.Complete(72, "The answer needs a clearer result.", "", testTime.Add(time.Minute)); err != nil {
		t.Fatalf("complete analysis: %v", err)
	}

	_, err := review.NewHistoryRecord("history-1", "session-1", analysis, testTime.Add(30*time.Second))
	if !errors.Is(err, review.ErrInvalidHistoryRecord) {
		t.Fatalf("expected history before analysis completion to fail, got %v", err)
	}
}

func mustPendingAnalysis(t *testing.T) review.TurnAnalysis {
	t.Helper()
	analysis, err := review.NewTurnAnalysis("analysis-1", "turn-1", "evaluator-v1", testTime)
	if err != nil {
		t.Fatalf("new analysis: %v", err)
	}
	return analysis
}

func mustFeedback(t *testing.T, retryable bool) review.FeedbackItem {
	t.Helper()
	item, err := review.NewFeedbackItem(
		"feedback-1",
		"analysis-1",
		review.FeedbackCategory("evidence"),
		"Add a measurable result.",
		"State the outcome with a metric.",
		[]review.Evidence{{TranscriptText: "We finished the project."}},
		retryable,
		testTime,
	)
	if err != nil {
		t.Fatalf("new feedback: %v", err)
	}
	return item
}

func mustRetryRequest(t *testing.T) review.RetryRequest {
	t.Helper()
	request, err := review.NewRetryRequest("retry-1", "turn-1", mustFeedback(t, true), testTime)
	if err != nil {
		t.Fatalf("new retry request: %v", err)
	}
	return request
}
