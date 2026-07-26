package review

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStableCategoryAcceptsOnlyBoundedMachineCategories(t *testing.T) {
	const fallback = "generation_failed"
	for _, test := range []struct {
		name     string
		category string
		want     string
	}{
		{name: "valid", category: "provider_timeout", want: "provider_timeout"},
		{name: "uppercase", category: "ProviderTimeout", want: fallback},
		{name: "spaces", category: "provider timeout", want: fallback},
		{name: "punctuation", category: "provider:error", want: fallback},
		{
			name:     "oversized",
			category: strings.Repeat("a", 65),
			want:     fallback,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := stableCategory(
				categorizedReviewError(test.category),
				fallback,
			)
			if got != test.want {
				t.Fatalf("stableCategory() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompletionPayloadRequiresValidResultAndCoveredEvidence(t *testing.T) {
	result := ReviewResult{
		OverallScore: 80,
		Summary:      "Clear answer.",
		Conclusions: []ReviewConclusion{{
			Key:      "clarity",
			Category: "clarity",
			Message:  "The answer is clear.",
		}},
	}
	evidence := []ReviewEvidence{{
		ConclusionKey: "clarity",
		SourceType:    SourceTypeConversationTurn,
		SourceID:      "turn-3",
		SourceVersion: "confirmed-v1",
		Snapshot:      json.RawMessage(`{"answer":"clear"}`),
	}}
	if err := validateCompletionPayload(result, evidence); err != nil {
		t.Fatalf("valid payload: %v", err)
	}

	invalidScore := result
	invalidScore.OverallScore = 101
	if err := validateCompletionPayload(
		invalidScore,
		evidence,
	); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("invalid score error = %v", err)
	}
	if err := validateCompletionPayload(
		result,
		nil,
	); !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("missing evidence error = %v", err)
	}
	wrongConclusion := append([]ReviewEvidence(nil), evidence...)
	wrongConclusion[0].ConclusionKey = "other"
	if err := validateCompletionPayload(
		result,
		wrongConclusion,
	); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("foreign conclusion error = %v", err)
	}
	oversized := append([]ReviewEvidence(nil), evidence...)
	oversized[0].Snapshot = json.RawMessage(
		`"` + strings.Repeat("a", 16*1024) + `"`,
	)
	if err := validateCompletionPayload(
		result,
		oversized,
	); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("oversized evidence error = %v", err)
	}
}

func TestGenerationFinalizationContextPreservesValuesAndHasBoundedLifetime(
	t *testing.T,
) {
	type contextKey string
	const key contextKey = "trace"
	parent, cancelParent := context.WithCancel(
		context.WithValue(context.Background(), key, "review-run"),
	)
	cancelParent()

	service := &EnsureService{finalizeTimeout: 20 * time.Millisecond}
	ctx, cancel := service.finalizationContext(parent)
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("finalization context inherited cancellation: %v", err)
	}
	if got := ctx.Value(key); got != "review-run" {
		t.Fatalf("finalization context value = %v, want review-run", got)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > service.finalizeTimeout {
		t.Fatalf("finalization context deadline = %v, present = %t", deadline, ok)
	}

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("finalization context error = %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("finalization context did not terminate at its deadline")
	}
}

type categorizedReviewError string

func (err categorizedReviewError) Error() string {
	return "provider failed"
}

func (err categorizedReviewError) StableCategory() string {
	return string(err)
}
