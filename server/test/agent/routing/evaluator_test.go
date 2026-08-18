package routing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
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
		preparationcapability.IELTSWarmUpToolName:     true,
		preparationcapability.PracticePreviewToolName: true,
		reviewcapability.ReviewSearchToolName:         true,
		reviewcapability.ReviewGetToolName:            true,
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
	for _, name := range []string{preparationcapability.PracticePreviewToolName} {
		if !containsString(writes, name) {
			t.Errorf("write tools missing %q", name)
		}
	}
	for _, name := range []string{
		preparationcapability.IELTSWarmUpToolName,
		reviewcapability.ReviewSearchToolName,
	} {
		if containsString(writes, name) {
			t.Errorf("read-only tool %q classified as write", name)
		}
	}
}

func TestPreviewFixtureRejectsInvalidClosedUnionBeforePort(t *testing.T) {
	evaluator, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	_, err = evaluator.executor.Execute(
		context.Background(),
		capability.CallContext{
			Actor:      requestcontext.Actor{UserID: "eval-user", SessionID: "eval-session"},
			ThreadID:   "eval-thread",
			RunID:      "invalid-preview-run",
			ToolCallID: "invalid-preview-call",
			RequestID:  "invalid-preview-request",
		},
		capability.Invocation{
			Name: preparationcapability.PracticePreviewToolName,
			Input: mustRaw(map[string]any{
				"scene_query":            "酒店入住",
				"resolution_kind":        preparationcapability.SceneResolutionKindCatalog,
				"catalog_scene_ids":      []string{"scn_travel_hotel_checkin", "scn_travel_airport_checkin"},
				"custom_scenario":        "",
				"custom_experience_hint": "NONE",
			}),
		},
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("Execute() error = %v, want ErrInvalidInput", err)
	}
	if inputs := evaluator.preview.takeInputsForRun("invalid-preview-run"); len(inputs) != 0 {
		t.Fatalf("fixture received invalid Preview input: %#v", inputs)
	}
}

func TestPreviewFixtureRequiresCustomScenarioBeforePort(t *testing.T) {
	evaluator, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	_, err = evaluator.executor.Execute(
		context.Background(),
		capability.CallContext{
			Actor:      requestcontext.Actor{UserID: "eval-user", SessionID: "eval-session"},
			ThreadID:   "eval-thread",
			RunID:      "invalid-custom-preview-run",
			ToolCallID: "invalid-custom-preview-call",
			RequestID:  "invalid-custom-preview-request",
		},
		capability.Invocation{
			Name: preparationcapability.PracticePreviewToolName,
			Input: mustRaw(map[string]any{
				"scene_query":            "在宠物店沟通鹦鹉寄养",
				"resolution_kind":        preparationcapability.SceneResolutionKindCustom,
				"catalog_scene_ids":      []string{},
				"custom_scenario":        "",
				"custom_experience_hint": "LIFE_AND_TRAVEL",
			}),
		},
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("Execute() error = %v, want ErrInvalidInput", err)
	}
	if inputs := evaluator.preview.takeInputsForRun("invalid-custom-preview-run"); len(inputs) != 0 {
		t.Fatalf("fixture received invalid Custom input: %#v", inputs)
	}
}
