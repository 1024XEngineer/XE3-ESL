package capabilityfixture

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
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
		reviewcapability.ReviewGetToolName,
		reviewcapability.ReviewSearchToolName,
	}
	if !equalStrings(names, want) {
		t.Fatalf("tool names = %#v, want %#v", names, want)
	}
}

func TestToolDefinitionsGuideModelAndConstrainArguments(t *testing.T) {
	registry, err := NewRegistry(NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	for _, definition := range registry.Definitions() {
		t.Run(definition.Name, func(t *testing.T) {
			if !strings.Contains(definition.Description, "Use ") ||
				!strings.Contains(definition.Description, "Do not use") {
				t.Fatalf(
					"description lacks routing guidance: %q",
					definition.Description,
				)
			}
			if disabled, ok := definition.InputSchema["additionalProperties"].(bool); !ok || disabled {
				t.Fatalf(
					"additionalProperties = %#v, want false",
					definition.InputSchema["additionalProperties"],
				)
			}
			properties, ok := definition.InputSchema["properties"].(map[string]any)
			if !ok || len(properties) == 0 {
				t.Fatalf("properties = %#v", definition.InputSchema["properties"])
			}
			for name, raw := range properties {
				property, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("property %q has no description: %#v", name, raw)
				}
				description, _ := property["description"].(string)
				if strings.TrimSpace(description) == "" {
					t.Fatalf("property %q has no description: %#v", name, raw)
				}
			}
			if limit, ok := properties["limit"].(map[string]any); ok {
				if limit["type"] != "integer" ||
					limit["minimum"] != 1 ||
					limit["maximum"] != 20 {
					t.Fatalf("limit schema = %#v", limit)
				}
			}
		})
	}

	materialTool, ok := registry.Get(MaterialSearchToolName)
	if !ok {
		t.Fatal("material.search.v1 not registered")
	}
	materialProperties := materialTool.Definition().
		InputSchema["properties"].(map[string]any)
	if !equalAny(
		materialProperties["kind"].(map[string]any)["enum"],
		[]string{"resume", "jd"},
	) {
		t.Fatalf("material kind schema = %#v", materialProperties["kind"])
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
			name:       "review search",
			toolName:   reviewcapability.ReviewSearchToolName,
			input:      json.RawMessage(`{"query":"metrics","limit":1}`),
			resultKey:  "reports",
			sourceType: "evaluation_report",
		},
		{
			name:       "review get",
			toolName:   reviewcapability.ReviewGetToolName,
			input:      json.RawMessage(`{"report_id":"mock-report-001"}`),
			resultKey:  "report",
			sourceType: "evaluation_report",
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
			result, err := capability.NewExecutor(registry).Execute(
				context.Background(),
				validCallContext("read-"+tt.toolName),
				capability.Invocation{Name: tt.toolName, Input: tt.input},
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
	executor := capability.NewExecutor(registry)
	empty, err := executor.Execute(
		context.Background(),
		validCallContext("empty-material"),
		capability.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"nothing matches this"}`),
		},
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
		capability.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend","kind":"linkedin"}`),
		},
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("invalid Execute() error = %v, want %v", err, capability.ErrInvalidInput)
	}

	store.SetForbidden(MaterialSearchToolName, true)
	_, err = executor.Execute(
		context.Background(),
		validCallContext("forbidden-material"),
		capability.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend"}`),
		},
	)
	if !errors.Is(err, capability.ErrExecutionRejected) {
		t.Fatalf("forbidden Execute() error = %v, want %v", err, capability.ErrExecutionRejected)
	}
	store.SetForbidden(MaterialSearchToolName, false)

	store.SetUnavailable(MaterialSearchToolName, true)
	_, err = executor.Execute(
		context.Background(),
		validCallContext("unavailable-material"),
		capability.Invocation{
			Name:  MaterialSearchToolName,
			Input: json.RawMessage(`{"query":"backend"}`),
		},
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
	if got, want := len(summaries), 4; got != want {
		t.Fatalf("len(CapabilitySummaries()) = %d, want %d", got, want)
	}
}

func validCallContext(requestID string) capability.CallContext {
	return capability.CallContext{
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
