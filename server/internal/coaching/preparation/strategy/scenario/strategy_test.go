package scenario

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestStrategyResolvesUserValuesAndSceneDefaults(t *testing.T) {
	strategy := New()
	resolved, err := strategy.Resolve(context.Background(), port.ResolveCommand{
		Actor: requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		Input: model.ContextInput{
			Kind: model.PreparationKindScenario,
			Scenario: &model.ScenarioContextInput{
				Situation:       "  Ask for a refund  ",
				UserRole:        "Customer",
				CounterpartRole: "Support agent",
				Goal:            "Receive a refund",
			},
		},
		ScenarioDefaults: model.ScenarioDefaults{
			CounterpartPersona: "Patient but policy-bound",
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Scenario.Situation != "Ask for a refund" ||
		resolved.Scenario.CounterpartPersona != "Patient but policy-bound" {
		t.Fatalf("resolved scenario = %#v", resolved.Scenario)
	}
}

func TestStrategyRequiresEveryResolvedField(t *testing.T) {
	strategy := New()
	_, err := strategy.Resolve(context.Background(), port.ResolveCommand{
		Actor: requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		Input: model.ContextInput{
			Kind:     model.PreparationKindScenario,
			Scenario: &model.ScenarioContextInput{},
		},
	})
	if err == nil {
		t.Fatal("Resolve unexpectedly succeeded")
	}
}
