package agentcapability

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

func TestPreviewReadyCompletesCurrentTurn(t *testing.T) {
	result := previewToolResult(PreviewResult{
		Status: "preview_ready",
	})
	if result.TurnOutcome != capability.TurnOutcomeCompleted {
		t.Fatalf("TurnOutcome = %v, want completed", result.TurnOutcome)
	}
}

func TestPreviewNeedsInputKeepsCurrentTurnOpen(t *testing.T) {
	result := previewToolResult(PreviewResult{
		Status: "needs_input",
	})
	if result.TurnOutcome != capability.TurnOutcomeContinue {
		t.Fatalf("TurnOutcome = %v, want continue", result.TurnOutcome)
	}
}
