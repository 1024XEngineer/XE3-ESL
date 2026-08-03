package store

import (
	"math"
	"testing"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
)

func TestSummarySourceSequenceSpanBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		sourceFromSequence     int64
		coveredThroughSequence int64
		wantValid              bool
		wantCount              int
	}{
		{
			name:                   "single message at maximum sequence",
			sourceFromSequence:     math.MaxInt64,
			coveredThroughSequence: math.MaxInt64,
			wantValid:              true,
			wantCount:              1,
		},
		{
			name:                   "maximum allowed range ending at maximum sequence",
			sourceFromSequence:     math.MaxInt64 - agentsummary.MaxSourceMessages + 1,
			coveredThroughSequence: math.MaxInt64,
			wantValid:              true,
			wantCount:              agentsummary.MaxSourceMessages,
		},
		{
			name:                   "range over limit ending at maximum sequence",
			sourceFromSequence:     math.MaxInt64 - agentsummary.MaxSourceMessages,
			coveredThroughSequence: math.MaxInt64,
			wantValid:              false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			count, valid := summarySourceMessageCount(
				test.sourceFromSequence,
				test.coveredThroughSequence,
			)
			if valid != test.wantValid {
				t.Fatalf("valid = %t, want %t", valid, test.wantValid)
			}
			if !valid {
				return
			}
			if count != test.wantCount {
				t.Fatalf("count = %d, want %d", count, test.wantCount)
			}
		})
	}
}
