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
		"继续上次那个面试":        ReasonMissingRequiredContext,
		"看看我上次面试评价":       ReasonMissingRequiredContext,
		"结合我的简历和 JD 帮我准备": ReasonMaterialContextRequest,
		"查一下我以前的语法错题":     ReasonHistoricalMistakeRequest,
		"把刚才的问题加到错题":      ReasonMissingRequiredContext,
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
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output = %q, want %q", logged, want)
		}
	}
}
