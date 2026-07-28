package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type stubTool struct {
	definition Definition
	result     Result
	err        error
	input      json.RawMessage
	calls      int
}

func (tool *stubTool) Definition() Definition {
	return tool.definition
}

func (tool *stubTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	tool.calls++
	tool.input = append(json.RawMessage{}, input...)
	return tool.result, tool.err
}

func TestRegistryRejectsDuplicateTools(t *testing.T) {
	tool := &stubTool{definition: readToolDefinition("review.search.v1")}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if err := registry.Register(tool); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("Register duplicate error = %v, want %v", err, ErrDuplicateTool)
	}
}

func TestRegistryDefinitionsAreSorted(t *testing.T) {
	registry, err := NewRegistry(
		&stubTool{definition: readToolDefinition("review.search.v1")},
		&stubTool{definition: readToolDefinition("context.search.v1")},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	definitions := registry.Definitions()
	if got, want := len(definitions), 2; got != want {
		t.Fatalf("len(Definitions()) = %d, want %d", got, want)
	}
	if got, want := definitions[0].Name, "context.search.v1"; got != want {
		t.Fatalf("Definitions()[0].Name = %q, want %q", got, want)
	}
}

func TestPolicyFiltersWriteTools(t *testing.T) {
	registry, err := NewRegistry(
		&stubTool{definition: readToolDefinition("review.search.v1")},
		&stubTool{definition: writeToolDefinition("scenario.create.v1")},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	definitions, err := (Policy{}).Select(registry)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got, want := len(definitions), 1; got != want {
		t.Fatalf("len(Select()) = %d, want %d", got, want)
	}
	if got, want := definitions[0].Name, "review.search.v1"; got != want {
		t.Fatalf("selected tool = %q, want %q", got, want)
	}
}

func TestExecutorValidatesAndRunsTool(t *testing.T) {
	tool := &stubTool{
		definition: readToolDefinition("review.search.v1"),
		result: Result{
			Content: map[string]any{"ok": true},
		},
	}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	input := json.RawMessage(`{"query":"last interview"}`)
	result, err := NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{Name: "review.search.v1", Input: input},
		Policy{AllowedNames: []string{"review.search.v1"}},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content["ok"] != true {
		t.Fatalf("Execute() content = %#v, want ok", result.Content)
	}
	if string(tool.input) != string(input) {
		t.Fatalf("tool input = %s, want %s", tool.input, input)
	}
}

func TestExecutorRejectsToolOutsidePolicy(t *testing.T) {
	tool := &stubTool{definition: writeToolDefinition("scenario.create.v1")}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{Name: "scenario.create.v1", Input: json.RawMessage(`{"title":"PM interview"}`)},
		Policy{},
	)
	if !errors.Is(err, ErrToolRejected) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrToolRejected)
	}
	if tool.calls != 0 {
		t.Fatalf("tool calls = %d, want 0", tool.calls)
	}
}

func TestExecutorRejectsInvalidCallContext(t *testing.T) {
	registry, err := NewRegistry(&stubTool{definition: readToolDefinition("review.search.v1")})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = NewExecutor(registry).Execute(
		context.Background(),
		CallContext{},
		Invocation{Name: "review.search.v1", Input: json.RawMessage(`{}`)},
		Policy{AllowedNames: []string{"review.search.v1"}},
	)
	if !errors.Is(err, ErrToolRejected) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrToolRejected)
	}
}

func TestExecutorValidatesInputSchema(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
	}{
		{name: "missing required", input: json.RawMessage(`{}`)},
		{name: "wrong type", input: json.RawMessage(`{"query":123}`)},
		{name: "extra field", input: json.RawMessage(`{"query":"x","unexpected":true}`)},
		{name: "null field", input: json.RawMessage(`{"query":null}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &stubTool{definition: readToolDefinition("review.search.v1")}
			registry, err := NewRegistry(tool)
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			_, err = NewExecutor(registry).Execute(
				context.Background(),
				validCallContext(),
				Invocation{Name: "review.search.v1", Input: tt.input},
				Policy{AllowedNames: []string{"review.search.v1"}},
			)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Execute() error = %v, want %v", err, ErrInvalidInput)
			}
			if tool.calls != 0 {
				t.Fatalf("tool calls = %d, want 0", tool.calls)
			}
		})
	}
}

func readToolDefinition(name string) Definition {
	return Definition{
		Name:        name,
		Description: "Search data.",
		InputSchema: objectSchema(map[string]any{
			"query": stringSchema("Query text."),
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

func writeToolDefinition(name string) Definition {
	return Definition{
		Name:        name,
		Description: "Create data.",
		InputSchema: objectSchema(map[string]any{
			"title": stringSchema("Title."),
		}, nil),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

func validCallContext() CallContext {
	return CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}
