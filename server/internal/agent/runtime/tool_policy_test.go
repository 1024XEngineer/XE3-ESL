package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

func TestToolPolicyKeepsEmptyAllowedNamesAsAllRegisteredTools(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	policy := tool.Policy{AllowWrites: true}
	definitions, err := policy.Select(registry)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got, want := toolNames(definitions), []string{
		mocktool.MaterialSearchToolName,
		mocktool.MistakeSearchToolName,
		reviewtool.ReviewGetToolName,
		reviewtool.ReviewSearchToolName,
		mattertool.ScenarioSearchToolName,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected names = %#v, want %#v", got, want)
	}
}

func TestToolPolicyBuilderFiltersByFeaturesAndDirectIntent(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	decision := NewToolPolicyBuilder(registry, nil).Build(
		"run-1",
		PolicyContext{
			Actor:    validPolicyActor(),
			ThreadID: "thread-1",
			Intent: IntentDecision{
				Mode:       IntentDirectOnly,
				ReasonCode: ReasonDirectLanguageHelp,
			},
			AvailableFeatures: map[string]bool{
				mattertool.ScenarioCreateToolName: true,
				mattertool.ScenarioSearchToolName: true,
				reviewtool.ReviewSearchToolName:   true,
			},
		},
	)
	if decision.Policy.AllowWrites {
		t.Fatal("AllowWrites = true, want false")
	}
	if got, want := decision.AllowedTools, []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedTools = %#v, want %#v", got, want)
	}
	if !blockedWithReason(
		decision.BlockedTools,
		mattertool.ScenarioCreateToolName,
		ReasonDirectLanguageHelp,
	) {
		t.Fatalf("scenario.create blocked tools = %#v", decision.BlockedTools)
	}
	if !blockedWithReason(
		decision.BlockedTools,
		mocktool.MaterialSearchToolName,
		ReasonDirectLanguageHelp,
	) {
		t.Fatalf("material.search blocked tools = %#v", decision.BlockedTools)
	}
	if decision.ToolChoice.Mode != ai.ToolChoiceNone {
		t.Fatalf("ToolChoice = %#v, want none", decision.ToolChoice)
	}
}

func TestToolPolicyBuilderRequiresConfirmationForScenarioCreate(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	base := PolicyContext{
		Actor:    validPolicyActor(),
		ThreadID: "thread-1",
		Intent: NewIntentGuard(nil).Guard(
			"run-1",
			"帮我创建一个英文后端面试练习场景",
		),
	}
	decision := NewToolPolicyBuilder(registry, nil).Build("run-1", base)
	if containsString(decision.AllowedTools, mattertool.ScenarioCreateToolName) {
		t.Fatalf("unconfirmed AllowedTools = %#v, should not include scenario.create", decision.AllowedTools)
	}
	if decision.ToolChoice.Mode != ai.ToolChoiceNone {
		t.Fatalf("unconfirmed ToolChoice = %#v, want none", decision.ToolChoice)
	}

	base.ConfirmedActions = []string{mattertool.ScenarioCreateToolName}
	decision = NewToolPolicyBuilder(registry, nil).Build("run-1", base)
	if !containsString(decision.AllowedTools, mattertool.ScenarioCreateToolName) {
		t.Fatalf("confirmed AllowedTools = %#v, should include scenario.create", decision.AllowedTools)
	}
}

func TestToolPolicyBuilderUsesPreferredToolsForStrongBusinessIntent(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	tests := []struct {
		name string
		text string
		tool string
	}{
		{
			name: "review",
			text: "帮我找一下上次 PM interview 的 review",
			tool: reviewtool.ReviewSearchToolName,
		},
		{
			name: "material",
			text: "结合我的简历和后端岗位 JD，帮我准备英文自我介绍",
			tool: mocktool.MaterialSearchToolName,
		},
		{
			name: "mistake",
			text: "看一下我以前的错题，找 recurring mistakes",
			tool: mocktool.MistakeSearchToolName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := NewToolPolicyBuilder(registry, nil).Build(
				"run-1",
				PolicyContext{
					Actor:    validPolicyActor(),
					ThreadID: "thread-1",
					Intent:   NewIntentGuard(nil).Guard("run-1", tt.text),
				},
			)
			if got, want := decision.AllowedTools, []string{tt.tool}; !reflect.DeepEqual(got, want) {
				t.Fatalf("AllowedTools = %#v, want %#v", got, want)
			}
			if decision.ToolChoice.Mode != ai.ToolChoiceSpecific ||
				decision.ToolChoice.Name != tt.tool {
				t.Fatalf("ToolChoice = %#v, want specific %s", decision.ToolChoice, tt.tool)
			}
		})
	}
}

func TestToolPolicyBuilderUsesActiveMatterToAvoidScenarioSearch(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	decision := NewToolPolicyBuilder(registry, nil).Build(
		"run-1",
		PolicyContext{
			Actor:          validPolicyActor(),
			ThreadID:       "thread-1",
			ActiveMatterID: "matter-1",
			Intent: IntentDecision{
				Mode:       IntentToolEligible,
				ReasonCode: ReasonNewRealWorldScenario,
			},
		},
	)
	if containsString(decision.AllowedTools, mattertool.ScenarioSearchToolName) {
		t.Fatalf("AllowedTools = %#v, should not include scenario.search with active matter", decision.AllowedTools)
	}
	if !containsString(decision.AllowedTools, mocktool.MaterialSearchToolName) {
		t.Fatalf("AllowedTools = %#v, should include material.search", decision.AllowedTools)
	}
}

func TestToolPolicyBuilderLimitsExplicitCommandToParsedTool(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	decision := NewToolPolicyBuilder(registry, nil).Build(
		"run-1",
		PolicyContext{
			Actor:            validPolicyActor(),
			ThreadID:         "thread-1",
			EntryPoint:       "command",
			ExplicitToolName: reviewtool.ReviewSearchToolName,
			Intent: IntentDecision{
				Mode:       IntentToolEligible,
				ReasonCode: ReasonExplicitCommand,
			},
		},
	)
	if got, want := decision.Policy.AllowedNames, []string{reviewtool.ReviewSearchToolName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedNames = %#v, want %#v", got, want)
	}
	if got, want := decision.AllowedTools, []string{reviewtool.ReviewSearchToolName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedTools = %#v, want %#v", got, want)
	}
	if _, err := tool.NewExecutor(registry).Execute(
		context.Background(),
		validCallContextForPolicy(),
		tool.Invocation{Name: reviewtool.ReviewSearchToolName, Input: []byte(`{"query":"last review"}`)},
		decision.Policy,
	); err != nil {
		t.Fatalf("Execute() with command policy error = %v", err)
	}
}

func TestToolPolicyBuilderRejectsInvalidTrustedContext(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	decision := NewToolPolicyBuilder(registry, nil).Build(
		"run-1",
		PolicyContext{
			Actor:    requestcontext.Actor{UserID: "user-1"},
			ThreadID: "thread-1",
			Intent: IntentDecision{
				Mode:       IntentToolEligible,
				ReasonCode: ReasonNewRealWorldScenario,
			},
		},
	)
	if decision.ReasonCode != ReasonPolicyRejected {
		t.Fatalf("ReasonCode = %q, want %q", decision.ReasonCode, ReasonPolicyRejected)
	}
	if decision.Policy.AllowWrites {
		t.Fatal("AllowWrites = true, want false")
	}
}

func TestToolPolicyBuilderLogsCandidateEvent(t *testing.T) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	NewToolPolicyBuilder(registry, logger).Build(
		"run-1",
		PolicyContext{
			Actor:    validPolicyActor(),
			ThreadID: "thread-1",
			Intent: IntentDecision{
				Mode:       IntentToolEligible,
				ReasonCode: ReasonHistoricalReviewRequest,
			},
			AvailableFeatures: map[string]bool{
				reviewtool.ReviewSearchToolName: true,
			},
		},
	)

	logged := output.String()
	for _, want := range []string{
		"agent.routing.candidates",
		"run_id=run-1",
		"policy_version=tool-policy-v1",
		"reason_code=historical_review_request",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}

func validPolicyActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
}

func validCallContextForPolicy() tool.CallContext {
	return tool.CallContext{
		Actor:      validPolicyActor(),
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}

func toolNames(definitions []tool.Definition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func blockedWithReason(blocked []BlockedTool, name string, reason string) bool {
	for _, item := range blocked {
		if item.Name == name && item.Reason == reason {
			return true
		}
	}
	return false
}
