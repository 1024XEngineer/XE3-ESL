package mocktool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

func TestRegistryExposesInitialMockToolSet(t *testing.T) {
	registry, err := NewRegistry(NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	definitions := registry.Definitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	want := []string{
		MaterialSearchToolName,
		MistakeSearchToolName,
		reviewtool.ReviewGetToolName,
		reviewtool.ReviewSearchToolName,
		mattertool.ScenarioCreateToolName,
		mattertool.ScenarioSearchToolName,
	}
	if !equalStrings(names, want) {
		t.Fatalf("tool names = %#v, want %#v", names, want)
	}
}

func TestScenarioCreateRequiresConfirmationAndIsIdempotent(t *testing.T) {
	registry, err := NewRegistry(NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	executor := tool.NewExecutor(registry)
	input := json.RawMessage(`{"type":"interview","title":"PM interview","goal":"prepare concise answers"}`)
	_, err = executor.Execute(
		context.Background(),
		validCallContext("create-scenario-1"),
		tool.Invocation{Name: mattertool.ScenarioCreateToolName, Input: input},
		tool.Policy{AllowWrites: true},
	)
	if !errors.Is(err, tool.ErrToolRejected) {
		t.Fatalf("unconfirmed Execute() error = %v, want %v", err, tool.ErrToolRejected)
	}

	policy := tool.Policy{
		AllowWrites:    true,
		ConfirmedNames: []string{mattertool.ScenarioCreateToolName},
	}
	first, err := executor.Execute(
		context.Background(),
		validCallContext("create-scenario-1"),
		tool.Invocation{Name: mattertool.ScenarioCreateToolName, Input: input},
		policy,
	)
	if err != nil {
		t.Fatalf("confirmed Execute() error = %v", err)
	}
	replayed, err := executor.Execute(
		context.Background(),
		validCallContext("create-scenario-1"),
		tool.Invocation{Name: mattertool.ScenarioCreateToolName, Input: input},
		policy,
	)
	if err != nil {
		t.Fatalf("replay Execute() error = %v", err)
	}
	if first.Content["scenario"] == nil ||
		!equalAny(first.Content["scenario"], replayed.Content["scenario"]) {
		t.Fatalf("idempotent scenario mismatch: %#v vs %#v", first.Content, replayed.Content)
	}
}

func TestReadOnlyMockToolsReturnExpectedFixtures(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		input      json.RawMessage
		resultKey  string
		sourceType string
	}{
		{
			name:       "scenario search",
			toolName:   mattertool.ScenarioSearchToolName,
			input:      json.RawMessage(`{"query":"interview","limit":1}`),
			resultKey:  "scenarios",
			sourceType: "mock_scenario",
		},
		{
			name:       "review search",
			toolName:   reviewtool.ReviewSearchToolName,
			input:      json.RawMessage(`{"query":"metrics","limit":1}`),
			resultKey:  "reviews",
			sourceType: "mock_review",
		},
		{
			name:       "review get",
			toolName:   reviewtool.ReviewGetToolName,
			input:      json.RawMessage(`{"review_id":"mock-review-001"}`),
			resultKey:  "review",
			sourceType: "mock_review",
		},
		{
			name:       "material search",
			toolName:   MaterialSearchToolName,
			input:      json.RawMessage(`{"query":"backend","kind":"resume","limit":1}`),
			resultKey:  "materials",
			sourceType: "mock_material",
		},
		{
			name:       "mistake search",
			toolName:   MistakeSearchToolName,
			input:      json.RawMessage(`{"query":"owner","limit":1}`),
			resultKey:  "mistakes",
			sourceType: "mock_mistake",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := NewRegistry(NewStore())
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			result, err := tool.NewExecutor(registry).Execute(
				context.Background(),
				validCallContext("read-"+tt.toolName),
				tool.Invocation{Name: tt.toolName, Input: tt.input},
				tool.Policy{},
			)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Content[tt.resultKey] == nil {
				t.Fatalf("Content missing %q: %#v", tt.resultKey, result.Content)
			}
			if len(result.SourceRefs) == 0 ||
				result.SourceRefs[0].Type != tt.sourceType {
				t.Fatalf("SourceRefs = %#v, want first type %q", result.SourceRefs, tt.sourceType)
			}
		})
	}
}

func TestMockToolsSupportEmptyResultInvalidForbiddenAndUnavailable(t *testing.T) {
	store := NewStore()
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	executor := tool.NewExecutor(registry)
	empty, err := executor.Execute(
		context.Background(),
		validCallContext("empty-material"),
		tool.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"nothing matches this"}`),
		},
		tool.Policy{},
	)
	if err != nil {
		t.Fatalf("empty Execute() error = %v", err)
	}
	if got, ok := empty.Content["materials"].([]map[string]any); !ok || len(got) != 0 {
		t.Fatalf("empty materials = %#v, want empty slice", empty.Content["materials"])
	}

	_, err = executor.Execute(
		context.Background(),
		validCallContext("invalid-material"),
		tool.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend","kind":"linkedin"}`),
		},
		tool.Policy{},
	)
	if !errors.Is(err, tool.ErrInvalidInput) {
		t.Fatalf("invalid Execute() error = %v, want %v", err, tool.ErrInvalidInput)
	}

	store.SetForbidden(MaterialSearchToolName, true)
	_, err = executor.Execute(
		context.Background(),
		validCallContext("forbidden-material"),
		tool.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend"}`),
		},
		tool.Policy{},
	)
	if !errors.Is(err, tool.ErrToolRejected) {
		t.Fatalf("forbidden Execute() error = %v, want %v", err, tool.ErrToolRejected)
	}
	store.SetForbidden(MaterialSearchToolName, false)

	store.SetUnavailable(MaterialSearchToolName, true)
	_, err = executor.Execute(
		context.Background(),
		validCallContext("unavailable-material"),
		tool.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend"}`),
		},
		tool.Policy{},
	)
	if !errors.Is(err, ErrTemporarilyUnavailable) {
		t.Fatalf("unavailable Execute() error = %v, want %v", err, ErrTemporarilyUnavailable)
	}
}

func TestCapabilitySummariesIncludeRiskAndSchemaFields(t *testing.T) {
	registry, err := NewRegistry(NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	summaries := CapabilitySummaries(registry)
	if got, want := len(summaries), 6; got != want {
		t.Fatalf("len(CapabilitySummaries()) = %d, want %d", got, want)
	}
	var scenario CapabilitySummary
	for _, summary := range summaries {
		if summary.Name == mattertool.ScenarioCreateToolName {
			scenario = summary
			break
		}
	}
	if scenario.Risk != string(tool.RiskRequiresConfirm) ||
		scenario.ReadOnly ||
		!containsString(scenario.SchemaFields, "type") ||
		!containsString(scenario.RequiredNames, "type") {
		t.Fatalf("scenario summary = %#v", scenario)
	}
}

func validCallContext(requestID string) tool.CallContext {
	return tool.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  requestID,
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalAny(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
