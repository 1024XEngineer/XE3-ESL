package run

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	goalcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcapability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func TestModelToolRoutingExposesEveryRegisteredTool(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	routing := buildModelToolRouting(registry, nil, "run-1", ToolChoice{})
	got := exposedToolNameList(routing.Definitions)
	want := []string{
		goalcapability.GoalCreateCapabilityName,
		goalcapability.GoalSearchCapabilityName,
		capabilityfixture.MaterialSearchToolName,
		capabilityfixture.MistakeSearchToolName,
		reviewcapability.ReviewGetToolName,
		reviewcapability.ReviewSearchToolName,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exposed tools = %#v, want %#v", got, want)
	}
	if routing.ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf("ToolChoice = %#v, want auto", routing.ToolChoice)
	}
}

func TestModelToolRoutingDoesNotDependOnUserInput(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	want := exposedToolNameList(
		buildModelToolRouting(registry, nil, "run-1", ToolChoice{}).Definitions,
	)
	for _, input := range []string{
		"帮我润色这句话",
		"看看上次面试评价",
		"一段完全没有业务关键词的自然语言",
	} {
		t.Run(input, func(t *testing.T) {
			got := exposedToolNameList(
				buildModelToolRouting(registry, nil, "run-1", ToolChoice{}).Definitions,
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

	buildModelToolRouting(registry, logger, "run-1", ToolChoice{})

	logged := output.String()
	for _, want := range []string{
		"agent.tools.exposed",
		"run_id=run-1",
		"tool_count=6",
		"routing_version=model-tool-routing-v2",
		"tool_choice_mode=auto",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}

func TestModelToolRoutingLogsSpecificChoice(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	choice := specificToolChoice(reviewcapability.ReviewSearchToolName)

	routing := buildModelToolRouting(registry, logger, "run-1", choice)

	if routing.ToolChoice != choice {
		t.Fatalf("ToolChoice = %#v, want %#v", routing.ToolChoice, choice)
	}
	logged := output.String()
	for _, want := range []string{
		"tool_choice_mode=specific",
		"tool_choice_name=" + reviewcapability.ReviewSearchToolName,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}
