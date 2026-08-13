package routing

import (
	"context"
	"strings"
	"testing"

	evaluationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agentcapability"
	goalcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcapability"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
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
	if result.CoreRoutingAccuracy < 0.90 {
		t.Fatalf("CoreRoutingAccuracy = %v, want >= 0.90", result.CoreRoutingAccuracy)
	}
}

func TestEvaluationRegistryContainsInterviewMainlineTools(t *testing.T) {
	evaluator, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	want := map[string]bool{
		goalcapability.GoalCreateCapabilityName:           true,
		goalcapability.GoalSearchCapabilityName:           true,
		preparationcapability.IELTSWarmUpToolName:         true,
		preparationcapability.PracticePreviewToolName:     true,
		evaluationcapability.LatestPracticeReportToolName: true,
		reviewcapability.ReviewSearchToolName:             true,
		reviewcapability.ReviewGetToolName:                true,
	}
	for _, definition := range evaluator.registry.Definitions() {
		if definition.Name == removedPracticeStartToolName {
			t.Fatalf("removed practice start tool remains registered")
		}
		delete(want, definition.Name)
	}
	if len(want) != 0 {
		t.Fatalf("evaluation registry missing tools: %#v", want)
	}
}

func TestRegisteredWriteToolNamesUsesToolDefinitions(t *testing.T) {
	evaluator, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	writes := registeredWriteToolNames(evaluator.registry)
	for _, name := range []string{
		goalcapability.GoalCreateCapabilityName,
		preparationcapability.PracticePreviewToolName,
	} {
		if !containsString(writes, name) {
			t.Errorf("write tools missing %q", name)
		}
	}
	for _, name := range []string{
		goalcapability.GoalSearchCapabilityName,
		preparationcapability.IELTSWarmUpToolName,
		evaluationcapability.LatestPracticeReportToolName,
		reviewcapability.ReviewSearchToolName,
	} {
		if containsString(writes, name) {
			t.Errorf("read-only tool %q classified as write", name)
		}
	}
}
