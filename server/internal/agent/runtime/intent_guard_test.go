package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestIntentGuardClassifiesHighConfidenceDirectLanguageHelp(t *testing.T) {
	tests := []string{
		"帮我把这句话说得委婉一点",
		"I very like this project 有什么问题",
		"Please polish this sentence",
		"翻译：我今天可以晚点回复吗",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			decision := NewIntentGuard(nil).Guard("run-1", input)
			if decision.Mode != IntentDirectOnly {
				t.Fatalf("Mode = %q, want %q", decision.Mode, IntentDirectOnly)
			}
			if decision.ReasonCode != ReasonDirectLanguageHelp {
				t.Fatalf("ReasonCode = %q, want %q", decision.ReasonCode, ReasonDirectLanguageHelp)
			}
			if decision.GuardVersion == "" {
				t.Fatal("GuardVersion is empty")
			}
		})
	}
}

func TestIntentGuardKeepsBusinessAndContextualRequestsToolEligible(t *testing.T) {
	tests := map[string]string{
		"我下周有英文 PM 面试":    ReasonNewRealWorldScenario,
		"继续上次那个面试":        ReasonNewRealWorldScenario,
		"看看我上次面试评价":       ReasonHistoricalReviewRequest,
		"结合我的简历和 JD 帮我准备": ReasonMaterialContextRequest,
		"查一下我以前的语法错题":     ReasonHistoricalMistakeRequest,
		"把刚才的问题加到错题":      ReasonHistoricalMistakeRequest,
	}
	for input, reason := range tests {
		t.Run(input, func(t *testing.T) {
			decision := NewIntentGuard(nil).Guard("run-1", input)
			if decision.Mode != IntentToolEligible {
				t.Fatalf("Mode = %q, want %q", decision.Mode, IntentToolEligible)
			}
			if decision.ReasonCode != reason {
				t.Fatalf("ReasonCode = %q, want %q", decision.ReasonCode, reason)
			}
		})
	}
}

func TestIntentGuardBuildsStructuredRouteDecision(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		intent     RouteIntent
		toolMode   RouteToolUseMode
		preferred  []string
		contextRef bool
		mode       IntentMode
	}{
		{
			name:     "language help",
			input:    "把“我负责后端稳定性”翻译成自然英文",
			intent:   RouteIntentLanguageHelp,
			toolMode: RouteToolUseNone,
			mode:     IntentDirectOnly,
		},
		{
			name:       "historical review with context",
			input:      "帮我找一下上次 PM interview 的 review",
			intent:     RouteIntentHistoricalReview,
			toolMode:   RouteToolUseSpecific,
			preferred:  []string{toolReviewSearch},
			contextRef: true,
			mode:       IntentToolEligible,
		},
		{
			name:      "material context",
			input:     "结合我的简历和后端岗位 JD，帮我准备英文自我介绍",
			intent:    RouteIntentMaterialContext,
			toolMode:  RouteToolUseSpecific,
			preferred: []string{toolMaterialSearch},
			mode:      IntentToolEligible,
		},
		{
			name:      "historical mistake",
			input:     "看一下我以前的错题，找 recurring mistakes",
			intent:    RouteIntentHistoricalMistake,
			toolMode:  RouteToolUseSpecific,
			preferred: []string{toolMistakeSearch},
			mode:      IntentToolEligible,
		},
		{
			name:      "scenario create",
			input:     "帮我创建一个英文后端面试练习场景",
			intent:    RouteIntentScenarioCreate,
			toolMode:  RouteToolUseConfirmRequired,
			preferred: []string{toolScenarioCreate},
			mode:      IntentToolEligible,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := NewIntentGuard(nil).Guard("run-1", tt.input)
			if decision.Mode != tt.mode {
				t.Fatalf("Mode = %q, want %q", decision.Mode, tt.mode)
			}
			if decision.Route.Intent != tt.intent ||
				decision.Route.ToolUseMode != tt.toolMode ||
				decision.Route.HasContextReference != tt.contextRef {
				t.Fatalf("Route = %#v", decision.Route)
			}
			if strings.Join(decision.Route.PreferredTools, ",") != strings.Join(tt.preferred, ",") {
				t.Fatalf("PreferredTools = %#v, want %#v", decision.Route.PreferredTools, tt.preferred)
			}
			if decision.Route.RouterVersion == "" {
				t.Fatal("RouterVersion is empty")
			}
		})
	}
}

func TestIntentGuardLogsStableRoutingEvent(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	NewIntentGuard(logger).Guard("run-1", "帮我润色这句话")

	logged := output.String()
	for _, want := range []string{
		"agent.intent.guarded",
		"run_id=run-1",
		"mode=direct_only",
		"reason_code=direct_language_help",
		"route_intent=language_help",
		"tool_use_mode=none",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}
