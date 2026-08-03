package slashcommand

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRegistryMatchesAliases(t *testing.T) {
	registry, err := NewRegistry(Definition{
		Name:        "创建面试",
		Aliases:     []string{"面试"},
		Description: "Create interview scenario.",
		ToolName:    ToolScenarioCreate,
		BuildInput: func(args string) (json.RawMessage, error) {
			return JSONObjectInput(map[string]any{"title": args})
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	definition, ok := registry.Get("面试")
	if !ok {
		t.Fatal("Get(alias) ok = false, want true")
	}
	if got, want := definition.Name, "创建面试"; got != want {
		t.Fatalf("definition.Name = %q, want %q", got, want)
	}
}

func TestRegistryRejectsDuplicateAlias(t *testing.T) {
	_, err := NewRegistry(
		Definition{
			Name:        "创建面试",
			Aliases:     []string{"面试"},
			Description: "Create interview scenario.",
			ToolName:    ToolScenarioCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(nil)
			},
		},
		Definition{
			Name:        "创建口语场景",
			Aliases:     []string{"面试"},
			Description: "Create speaking scenario.",
			ToolName:    ToolScenarioCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(nil)
			},
		},
	)
	if !errors.Is(err, ErrDuplicateCommand) {
		t.Fatalf("NewRegistry() error = %v, want %v", err, ErrDuplicateCommand)
	}
}

func TestRouterParsesBuiltinCommand(t *testing.T) {
	registry, err := NewRegistry(Builtins()...)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	parsed, matched, err := NewRouter(registry).Parse("/创建面试 产品经理 一面")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !matched {
		t.Fatal("Parse() matched = false, want true")
	}
	if got, want := parsed.Invocation.Name, ToolScenarioCreate; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	if got, want := parsed.Args, "产品经理 一面"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestBuiltinsOnlyReferenceImplementedTools(t *testing.T) {
	implemented := map[string]struct{}{
		ToolScenarioCreate: {},
		ToolScenarioSearch: {},
		ToolReviewSearch:   {},
	}
	for _, definition := range Builtins() {
		if _, ok := implemented[definition.ToolName]; !ok {
			t.Fatalf("builtin %q references unimplemented tool %q", definition.Name, definition.ToolName)
		}
	}
}

func TestRouterIgnoresNonCommand(t *testing.T) {
	registry, err := NewRegistry(Builtins()...)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, matched, err := NewRouter(registry).Parse("帮我润色这句话")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if matched {
		t.Fatal("Parse() matched = true, want false")
	}
}
