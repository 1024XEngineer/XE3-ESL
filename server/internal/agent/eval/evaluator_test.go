package eval

import (
	"context"
	"strings"
	"testing"
)

func TestBaselineRoutingEval(t *testing.T) {
	evaluator, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	result, err := evaluator.Evaluate(context.Background(), BaselineCases())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	for _, item := range result.CaseResults {
		if !item.Passed {
			t.Errorf("%s failed: %s", item.Name, strings.Join(item.Failures, "; "))
		}
	}
	if result.Passed != result.Total {
		t.Fatalf("passed %d/%d", result.Passed, result.Total)
	}
	if result.DirectMisrouteRate != 0 {
		t.Fatalf("DirectMisrouteRate = %v, want 0", result.DirectMisrouteRate)
	}
	if result.WriteMisrouteRate != 0 {
		t.Fatalf("WriteMisrouteRate = %v, want 0", result.WriteMisrouteRate)
	}
	if !result.UnauthorizedRejected {
		t.Fatal("UnauthorizedRejected = false, want true")
	}
	if result.CoreRoutingAccuracy < 0.90 {
		t.Fatalf("CoreRoutingAccuracy = %v, want >= 0.90", result.CoreRoutingAccuracy)
	}
}
