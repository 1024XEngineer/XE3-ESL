package run

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agenttest/capabilityfixture"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

func TestModelToolRoutingExposesEveryRegisteredTool(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	routing := buildModelToolRouting(registry, nil, "run-1")
	got := exposedToolNameList(routing.Definitions)
	want := []string{
		capabilityfixture.MaterialSearchToolName,
		capabilityfixture.MistakeSearchToolName,
		reviewtool.ReviewGetToolName,
		reviewtool.ReviewSearchToolName,
		mattertool.ScenarioCreateToolName,
		mattertool.ScenarioSearchToolName,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exposed tools = %#v, want %#v", got, want)
	}
	if routing.ToolChoice.Mode != ai.ToolChoiceAuto {
		t.Fatalf("ToolChoice = %#v, want auto", routing.ToolChoice)
	}
}

func TestModelToolRoutingDoesNotDependOnUserInput(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	want := exposedToolNameList(
		buildModelToolRouting(registry, nil, "run-1").Definitions,
	)
	for _, input := range []string{
		"帮我润色这句话",
		"看看上次面试评价",
		"一段完全没有业务关键词的自然语言",
	} {
		t.Run(input, func(t *testing.T) {
			got := exposedToolNameList(
				buildModelToolRouting(registry, nil, "run-1").Definitions,
			)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("exposed tools = %#v, want %#v", got, want)
			}
		})
	}
}

func TestModelToolRoutingLogsFullExposure(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	buildModelToolRouting(registry, logger, "run-1")

	logged := output.String()
	for _, want := range []string{
		"agent.tools.exposed",
		"run_id=run-1",
		"tool_count=6",
		"routing_version=model-tool-routing-v1",
		"tool_choice_mode=auto",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}
