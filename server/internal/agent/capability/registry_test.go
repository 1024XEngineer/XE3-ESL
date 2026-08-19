package capability

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

type conditionalStubTool struct {
	*stubTool
	effect      InvocationEffect
	effectErr   error
	effectInput json.RawMessage
}

type guardedStubTool struct {
	*stubTool
	decision ExposureDecision
}

func (tool *guardedStubTool) AuthorizeExposure(
	context.Context,
	ExposureRequest,
) (ExposureDecision, error) {
	return tool.decision, nil
}

func (tool *conditionalStubTool) ClassifyInvocationEffect(
	input json.RawMessage,
) (InvocationEffect, error) {
	tool.effectInput = append(json.RawMessage{}, input...)
	return tool.effect, tool.effectErr
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

func TestRegistryExposureExcludesConversationalToolAndRequiresAuthorizedTool(
	t *testing.T,
) {
	guarded := &guardedStubTool{
		stubTool: &stubTool{definition: readToolDefinition("practice.preview.v3")},
		decision: ExposureDecision{Expose: false, AuditLabel: "CONVERSE"},
	}
	registry, err := NewRegistry(guarded)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	request := ExposureRequest{
		Actor:    requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		ThreadID: "thread-1", RunID: "run-1", InputMessageID: "message-1",
	}
	plan, err := registry.ResolveExposure(context.Background(), request)
	if err != nil || len(plan.Definitions) != 0 || plan.RequiredTool != "" {
		t.Fatalf("converse plan = %#v, %v", plan, err)
	}
	guarded.decision = ExposureDecision{
		Expose: true, Require: true,
		Authorization: json.RawMessage(`{"intent":"REQUEST_CREATE"}`),
		AuditLabel:    "REQUEST_CREATE",
		InputSchema: ObjectSchema(map[string]any{
			"decision": StringEnumSchema("Authorized decision", "CREATE"),
		}, []string{"decision"}),
	}
	plan, err = registry.ResolveExposure(context.Background(), request)
	if err != nil || len(plan.Definitions) != 1 ||
		plan.RequiredTool != "practice.preview.v3" ||
		string(plan.Authorizations[plan.RequiredTool]) != `{"intent":"REQUEST_CREATE"}` ||
		plan.Definitions[0].InputSchema["properties"] == nil ||
		plan.InputSchemas[plan.RequiredTool]["properties"] == nil {
		t.Fatalf("authorized plan = %#v, %v", plan, err)
	}
}

func TestRegistryRejectsReadOnlyConditionalClassifier(t *testing.T) {
	conditional := &conditionalStubTool{
		stubTool: &stubTool{
			definition: readToolDefinition("conditional.readonly.v1"),
		},
		effect: InvocationEffectMayWrite,
	}
	if _, err := NewRegistry(conditional); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("NewRegistry() error = %v, want %v", err, ErrInvalidDefinition)
	}
}

func TestRegistryDefinitionsAreCompleteSortedAndIsolated(t *testing.T) {
	review := &stubTool{definition: readToolDefinition("review.search.v1")}
	contextSearch := &stubTool{definition: readToolDefinition("context.search.v1")}
	registry, err := NewRegistry(
		review,
		contextSearch,
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
	if got, want := definitions[1].Name, "review.search.v1"; got != want {
		t.Fatalf("Definitions()[1].Name = %q, want %q", got, want)
	}

	definitions[0].InputSchema["type"] = "array"
	properties := definitions[0].InputSchema["properties"].(map[string]any)
	properties["query"].(map[string]any)["type"] = "integer"
	definitions[0].InputSchema["required"].([]string)[0] = "changed"

	fresh := registry.Definitions()
	if got, want := fresh[0].InputSchema["type"], "object"; got != want {
		t.Fatalf("fresh schema type = %v, want %v", got, want)
	}
	freshProperties := fresh[0].InputSchema["properties"].(map[string]any)
	if got, want := freshProperties["query"].(map[string]any)["type"], "string"; got != want {
		t.Fatalf("fresh query type = %v, want %v", got, want)
	}
	if got, want := fresh[0].InputSchema["required"].([]string)[0], "query"; got != want {
		t.Fatalf("fresh required field = %v, want %v", got, want)
	}
}

func TestRegistryClassifiesInvocationEffects(t *testing.T) {
	readOnly := &stubTool{
		definition: readToolDefinition("readonly.search.v1"),
	}
	ordinaryWrite := &stubTool{
		definition: writeToolDefinition("ordinary.write.v1"),
	}
	conditionalRead := &conditionalStubTool{
		stubTool: &stubTool{
			definition: writeToolDefinition("conditional.read.v1"),
		},
		effect: InvocationEffectReadOnly,
	}
	conditionalWrite := &conditionalStubTool{
		stubTool: &stubTool{
			definition: writeToolDefinition("conditional.write.v1"),
		},
		effect: InvocationEffectMayWrite,
	}
	registry, err := NewRegistry(
		readOnly,
		ordinaryWrite,
		conditionalRead,
		conditionalWrite,
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	tests := []struct {
		name       string
		invocation Invocation
		want       InvocationEffect
	}{
		{
			name: "static readonly",
			invocation: Invocation{
				Name:  "readonly.search.v1",
				Input: json.RawMessage(`{"query":"history"}`),
			},
			want: InvocationEffectReadOnly,
		},
		{
			name: "ordinary write",
			invocation: Invocation{
				Name:  "ordinary.write.v1",
				Input: json.RawMessage(`{"query":"create"}`),
			},
			want: InvocationEffectMayWrite,
		},
		{
			name: "conditional readonly",
			invocation: Invocation{
				Name: "conditional.read.v1",
				Input: json.RawMessage(
					`{"query":"lookup","unexpected":true}`,
				),
			},
			want: InvocationEffectReadOnly,
		},
		{
			name: "conditional write",
			invocation: Invocation{
				Name:  "conditional.write.v1",
				Input: json.RawMessage(`{"query":"create"}`),
			},
			want: InvocationEffectMayWrite,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := registry.InvocationEffect(test.invocation); got != test.want {
				t.Fatalf("InvocationEffect() = %v, want %v", got, test.want)
			}
		})
	}
	if got, want := string(conditionalRead.effectInput),
		`{"query":"lookup"}`; got != want {
		t.Fatalf("classifier input = %s, want %s", got, want)
	}
}

func TestRegistryInvocationEffectFailsClosed(t *testing.T) {
	classifierFailure := &conditionalStubTool{
		stubTool: &stubTool{
			definition: writeToolDefinition("conditional.error.v1"),
		},
		effect:    InvocationEffectReadOnly,
		effectErr: errors.New("classification failed"),
	}
	invalidEffect := &conditionalStubTool{
		stubTool: &stubTool{
			definition: writeToolDefinition("conditional.invalid.v1"),
		},
	}
	readOnly := &stubTool{
		definition: readToolDefinition("readonly.search.v1"),
	}
	registry, err := NewRegistry(classifierFailure, invalidEffect, readOnly)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	tests := []struct {
		name       string
		registry   *Registry
		invocation Invocation
	}{
		{
			name:     "nil registry",
			registry: nil,
			invocation: Invocation{
				Name:  "readonly.search.v1",
				Input: json.RawMessage(`{"query":"history"}`),
			},
		},
		{
			name:     "unknown tool",
			registry: registry,
			invocation: Invocation{
				Name:  "missing.v1",
				Input: json.RawMessage(`{}`),
			},
		},
		{
			name:     "malformed input",
			registry: registry,
			invocation: Invocation{
				Name:  "readonly.search.v1",
				Input: json.RawMessage(`{`),
			},
		},
		{
			name:     "schema invalid input",
			registry: registry,
			invocation: Invocation{
				Name:  "readonly.search.v1",
				Input: json.RawMessage(`{"query":" "}`),
			},
		},
		{
			name:     "classifier error",
			registry: registry,
			invocation: Invocation{
				Name:  "conditional.error.v1",
				Input: json.RawMessage(`{"query":"lookup"}`),
			},
		},
		{
			name:     "classifier invalid effect",
			registry: registry,
			invocation: Invocation{
				Name:  "conditional.invalid.v1",
				Input: json.RawMessage(`{"query":"lookup"}`),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			effect := test.registry.InvocationEffect(test.invocation)
			if effect != InvocationEffectMayWrite || !effect.MayWrite() {
				t.Fatalf("InvocationEffect() = %v, want fail-closed write", effect)
			}
		})
	}
	if !InvocationEffect(0).MayWrite() {
		t.Fatal("zero InvocationEffect must fail closed")
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

func TestExecutorRejectsInvalidTurnOutcome(t *testing.T) {
	tool := &stubTool{
		definition: readToolDefinition("review.search.v1"),
		result: Result{
			Content:     map[string]any{"ok": true},
			TurnOutcome: TurnOutcome(255),
		},
	}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{
			Name:  "review.search.v1",
			Input: json.RawMessage(`{"query":"last interview"}`),
		},
	)
	if !errors.Is(err, ErrExecutionRejected) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrExecutionRejected)
	}
}

func TestExecutorFiltersUnknownInputFields(t *testing.T) {
	tool := &stubTool{
		definition: readToolDefinition("review.search.v1"),
		result:     Result{Content: map[string]any{"ok": true}},
	}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{
			Name: "review.search.v1",
			Input: json.RawMessage(
				`{"query":"last interview","unexpected":true}`,
			),
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := string(tool.input), `{"query":"last interview"}`; got != want {
		t.Fatalf("tool input = %s, want %s", got, want)
	}
}

func TestExecutorRejectsUnknownTool(t *testing.T) {
	registry, err := NewRegistry(
		&stubTool{definition: readToolDefinition("review.search.v1")},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{
			Name:  "missing.search.v1",
			Input: json.RawMessage(`{"query":"last interview"}`),
		},
	)
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrUnknownTool)
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
	)
	if !errors.Is(err, ErrExecutionRejected) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrExecutionRejected)
	}
}

func TestExecutorValidatesInputSchema(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
	}{
		{name: "missing required", input: json.RawMessage(`{}`)},
		{name: "wrong type", input: json.RawMessage(`{"query":123}`)},
		{name: "null field", input: json.RawMessage(`{"query":null}`)},
		{name: "blank text", input: json.RawMessage(`{"query":"   "}`)},
		{name: "invalid enum", input: json.RawMessage(`{"query":"x","kind":"other"}`)},
		{name: "integer below minimum", input: json.RawMessage(`{"query":"x","limit":0}`)},
		{name: "integer above maximum", input: json.RawMessage(`{"query":"x","limit":21}`)},
		{name: "fractional integer", input: json.RawMessage(`{"query":"x","limit":1.5}`)},
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
			"query": TextSchema("Query text.", 100),
			"kind": StringEnumSchema(
				"Data kind.",
				"review",
				"scenario",
			),
			"limit": IntegerRangeSchema("Maximum results.", 1, 20),
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

func writeToolDefinition(name string) Definition {
	definition := readToolDefinition(name)
	definition.ReadOnly = false
	definition.Risk = RiskLowRiskWrite
	return definition
}

func TestExecutorNormalizesEmptyResultAndErrorCategories(t *testing.T) {
	emptyTool := &stubTool{
		definition: readToolDefinition("review.search.v1"),
		result:     Result{},
	}
	registry, err := NewRegistry(emptyTool)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	result, err := NewExecutor(registry).Execute(
		context.Background(),
		validCallContext(),
		Invocation{
			Name:  "review.search.v1",
			Input: json.RawMessage(`{"query":"nothing"}`),
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content == nil || len(result.Content) != 0 {
		t.Fatalf("Content = %#v, want empty object", result.Content)
	}

	tests := map[string]struct {
		err       error
		category  string
		retryable bool
	}{
		"invalid input": {
			err:       ErrInvalidInput,
			category:  "invalid_input",
			retryable: false,
		},
		"unknown tool": {
			err:       ErrUnknownTool,
			category:  "unknown_tool",
			retryable: false,
		},
		"rejected tool": {
			err:       ErrExecutionRejected,
			category:  "execution_rejected",
			retryable: false,
		},
		"internal failure": {
			err:       errors.New("temporary"),
			category:  "internal",
			retryable: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ErrorCategory(tt.err); got != tt.category {
				t.Fatalf("ErrorCategory() = %q, want %q", got, tt.category)
			}
			if got := RetryableError(tt.err); got != tt.retryable {
				t.Fatalf("RetryableError() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func validCallContext() CallContext {
	return CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:       "thread-1",
		RunID:          "run-1",
		InputMessageID: "message-1",
		ToolCallID:     "tool-call-1",
		RequestID:      "request-1",
	}
}
