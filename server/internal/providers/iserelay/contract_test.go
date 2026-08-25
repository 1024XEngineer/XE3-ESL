package iserelay

import (
	"math"
	"testing"
)

func TestValidSummaryRejectsNonFiniteValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary Summary
	}{
		{name: "NaN score", summary: Summary{AccuracyScore: pointer(math.NaN())}},
		{name: "positive infinite score", summary: Summary{FluencyScore: pointer(math.Inf(1))}},
		{name: "negative infinite speed", summary: Summary{SpeakingSpeed: pointer(math.Inf(-1))}},
		{name: "positive infinite speed", summary: Summary{SpeakingSpeed: pointer(math.Inf(1))}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if validSummary(test.summary) {
				t.Fatal("expected non-finite summary value to be rejected")
			}
		})
	}
}

func pointer(value float64) *float64 {
	return &value
}
