package planpolicy

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestResolverFreezesCompleteRegisteredPolicy(t *testing.T) {
	t.Parallel()

	definition := scene.SceneDefinition{
		Prompt: scene.ScenePrompt{
			TurnBlueprints: []string{"one", "two", "three", "four"},
		},
	}
	policy, err := NewResolver().ResolveSessionPolicy(
		definition,
		scene.PracticeOption{
			Mode:                     scene.PracticeModeFullSimulation,
			SuggestedDurationSeconds: 600,
			SessionPolicyRef:         practice.DailyPracticeSessionPolicy,
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.RetryAllowed ||
		policy.CompletionMode != preparation.CompletionModeUserControlled ||
		policy.MinEffectiveTurns != 1 || policy.MaxEffectiveTurns != 0 ||
		policy.CoverageCheckpointTurn != 1 ||
		policy.MaxFollowUpsPerQuestion != 1 {
		t.Fatalf("resolved policy = %#v", policy)
	}
}
