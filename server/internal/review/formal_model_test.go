package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReviewResultAcceptsTheMaximumJSONBudget(t *testing.T) {
	result := maximumValidReviewResult(t)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal maximum Result: %v", err)
	}
	if len(payload) != maxReviewResultJSONBytes {
		t.Fatalf(
			"maximum Result bytes = %d, want %d",
			len(payload),
			maxReviewResultJSONBytes,
		)
	}
	if err := validateReviewResult(result); err != nil {
		t.Fatalf("maximum valid Result rejected: %v", err)
	}
}

func TestReviewResultRejectsEveryFrozenBudgetViolation(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	tests := []struct {
		name   string
		mutate func(*ReviewResult)
	}{
		{
			name: "summary over 2048 UTF-8 bytes",
			mutate: func(result *ReviewResult) {
				result.Summary = strings.Repeat("界", 683)
			},
		},
		{
			name: "summary invalid UTF-8",
			mutate: func(result *ReviewResult) {
				result.Summary = invalidUTF8
			},
		},
		{
			name: "summary contains NUL",
			mutate: func(result *ReviewResult) {
				result.Summary = "clear\x00answer"
			},
		},
		{
			name: "no conclusions",
			mutate: func(result *ReviewResult) {
				result.Conclusions = nil
			},
		},
		{
			name: "more than eight conclusions",
			mutate: func(result *ReviewResult) {
				result.Conclusions = append(
					result.Conclusions,
					result.Conclusions[0],
				)
			},
		},
		{
			name: "key over 64 UTF-8 bytes",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Key = strings.Repeat("k", 65)
			},
		},
		{
			name: "key invalid UTF-8",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Key = invalidUTF8
			},
		},
		{
			name: "key contains NUL",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Key = "clarity\x00detail"
			},
		},
		{
			name: "category over 64 UTF-8 bytes",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Category = strings.Repeat("c", 65)
			},
		},
		{
			name: "category invalid UTF-8",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Category = invalidUTF8
			},
		},
		{
			name: "category contains NUL",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Category = "clarity\x00detail"
			},
		},
		{
			name: "message over 2048 UTF-8 bytes",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Message = strings.Repeat("m", 2049)
			},
		},
		{
			name: "message invalid UTF-8",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Message = invalidUTF8
			},
		},
		{
			name: "message contains NUL",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Message = "clear\x00answer"
			},
		},
		{
			name: "suggestion over 2048 UTF-8 bytes",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Suggestion = strings.Repeat("s", 2049)
			},
		},
		{
			name: "suggestion invalid UTF-8",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Suggestion = invalidUTF8
			},
		},
		{
			name: "suggestion contains NUL",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Suggestion = "add\x00detail"
			},
		},
		{
			name: "non-empty suggestion trims to empty",
			mutate: func(result *ReviewResult) {
				result.Conclusions[0].Suggestion = " \t\n"
			},
		},
		{
			name: "marshaled Result over 12 KiB",
			mutate: func(result *ReviewResult) {
				for index := range result.Conclusions {
					result.Conclusions[index].Message =
						strings.Repeat("m", maxReviewConclusionTextBytes)
					result.Conclusions[index].Suggestion =
						strings.Repeat("s", maxReviewConclusionTextBytes)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := maximumValidReviewResult(t)
			test.mutate(&result)
			if err := validateReviewResult(result); !errors.Is(
				err,
				ErrInvalidReview,
			) {
				t.Fatalf("invalid Result error = %v", err)
			}
		})
	}
}

func maximumValidReviewResult(t *testing.T) ReviewResult {
	t.Helper()
	result := ReviewResult{
		OverallScore: 100,
		Summary:      strings.Repeat("s", maxReviewSummaryUTF8Bytes),
		Conclusions: make(
			[]ReviewConclusion,
			maxReviewConclusions,
		),
	}
	for index := range result.Conclusions {
		result.Conclusions[index] = ReviewConclusion{
			Key: fmt.Sprintf("%02d", index) +
				strings.Repeat("k", maxReviewConclusionLabelBytes-2),
			Category: strings.Repeat(
				"c",
				maxReviewConclusionLabelBytes,
			),
			Message:    strings.Repeat("m", 700),
			Suggestion: strings.Repeat("s", 300),
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal Result fixture: %v", err)
	}
	remaining := maxReviewResultJSONBytes - len(payload)
	last := &result.Conclusions[len(result.Conclusions)-1]
	if remaining < 0 ||
		len(last.Suggestion)+remaining > maxReviewConclusionTextBytes {
		t.Fatalf(
			"Result fixture cannot reach budget: bytes=%d remaining=%d",
			len(payload),
			remaining,
		)
	}
	last.Suggestion += strings.Repeat("x", remaining)
	return result
}
