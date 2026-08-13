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

func TestUserIntentToolRoutingSelectsPracticeForNaturalRequests(t *testing.T) {
	base := modelToolRouting{
		Definitions: []ToolDefinition{
			{Name: goalCreateToolName},
			{Name: practicePreviewToolName},
		},
		ToolChoice: ToolChoice{Mode: ToolChoiceAuto},
	}
	for _, input := range []string{
		"我明天要参加英文产品经理面试，帮我模拟一场。",
		"我需要练英文自我介绍，来一场面试。",
		"帮我安排一次模拟。",
		"直接来一场英文面试。",
	} {
		t.Run(input, func(t *testing.T) {
			routing := applyUserIntentToolRouting(base, input)
			if got, want := routing.ToolChoice, (ToolChoice{Mode: ToolChoiceRequired}); got != want {
				t.Fatalf("ToolChoice = %#v, want %#v", got, want)
			}
			if got, want := exposedToolNameList(routing.Definitions), []string{practicePreviewToolName}; !reflect.DeepEqual(got, want) {
				t.Fatalf("exposed tools = %#v, want %#v", got, want)
			}
			if toolExposed(exposedToolNames(routing.Definitions), goalCreateToolName) {
				t.Fatal("goal.create.v1 must not be exposed for a practice request")
			}
		})
	}
}

func TestUserIntentToolRoutingDoesNotCreateImplicitGoal(t *testing.T) {
	routing := applyUserIntentToolRouting(
		modelToolRouting{
			Definitions: []ToolDefinition{
				{Name: goalCreateToolName},
				{Name: practicePreviewToolName},
			},
			ToolChoice: ToolChoice{Mode: ToolChoiceAuto},
		},
		"我明天有一场面试。",
	)
	if routing.ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf("ToolChoice = %#v, want auto", routing.ToolChoice)
	}
	if toolExposed(exposedToolNames(routing.Definitions), goalCreateToolName) {
		t.Fatal("goal.create.v1 must require explicit goal intent")
	}
}

func TestUserIntentToolRoutingKeepsExplicitGoalCreation(t *testing.T) {
	registry, err := capabilityfixture.NewRegistry(capabilityfixture.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	routing := applyUserIntentToolRouting(
		buildModelToolRouting(registry, nil, "run-1", ToolChoice{}),
		"帮我建立一个长期目标，准备产品经理面试。",
	)
	if !toolExposed(exposedToolNames(routing.Definitions), goalCreateToolName) {
		t.Fatal("goal.create.v1 must remain exposed for explicit goal intent")
	}
}

func TestSanitizeUserVisiblePracticeIdentifiers(t *testing.T) {
	input := "场景：英文自我介绍 (scn_interview_self_introduction)\n" +
		"角色：默认面试官 (role_interview_self_introduction_counterpart)\n" +
		"模式：重点练习 (option_interview_self_introduction_focus)"
	want := "场景：英文自我介绍\n角色：默认面试官\n模式：重点练习"
	if got := sanitizeUserVisiblePracticeIdentifiers(input); got != want {
		t.Fatalf("sanitized = %q, want %q", got, want)
	}
}
