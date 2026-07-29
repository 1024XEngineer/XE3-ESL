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

func TestReviewResultPreservesScenarioZeroAndLegacyScoreShapes(t *testing.T) {
	t.Parallel()
	scenarioJSON := []byte(`{
		"summary_eligibility":"eligible",
		"summary":"Needs a full rewrite.",
		"conclusions":[{
			"key":"clarity",
			"category":"language_clarity",
			"score":0,
			"message":"The answer was not understandable."
		}]
	}`)
	var scenario ReviewResult
	if err := json.Unmarshal(scenarioJSON, &scenario); err != nil {
		t.Fatalf("unmarshal scenario zero: %v", err)
	}
	if scenario.Conclusions[0].Score != 0 ||
		!scenario.Conclusions[0].ScorePresent {
		t.Fatalf("scenario zero presence lost: %+v", scenario.Conclusions[0])
	}
	encoded, err := json.Marshal(scenario)
	if err != nil {
		t.Fatalf("marshal scenario zero: %v", err)
	}
	var wire struct {
		Conclusions []map[string]json.RawMessage `json:"conclusions"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if score, present := wire.Conclusions[0]["score"]; !present || string(score) != "0" {
		t.Fatalf("scenario zero omitted from persistence JSON: %s", encoded)
	}

	missingScenarioScore := []byte(`{
		"summary_eligibility":"eligible",
		"summary":"Missing score.",
		"conclusions":[{
			"key":"clarity",
			"category":"language_clarity",
			"message":"The score is absent."
		}]
	}`)
	if err := json.Unmarshal(
		missingScenarioScore,
		&ReviewResult{},
	); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("missing scenario score error=%v, want invalid Review", err)
	}

	legacyJSON := []byte(`{
		"overall_score":80,
		"summary":"Legacy review.",
		"conclusions":[{
			"key":"clarity",
			"category":"clarity",
			"message":"Clear answer."
		}]
	}`)
	var legacy ReviewResult
	if err := json.Unmarshal(legacyJSON, &legacy); err != nil {
		t.Fatalf("unmarshal legacy result: %v", err)
	}
	encoded, err = json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy result: %v", err)
	}
	var legacyWire struct {
		Conclusions []map[string]json.RawMessage `json:"conclusions"`
	}
	if err := json.Unmarshal(encoded, &legacyWire); err != nil {
		t.Fatal(err)
	}
	if _, present := legacyWire.Conclusions[0]["score"]; present {
		t.Fatalf("legacy JSON unexpectedly gained score: %s", encoded)
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
