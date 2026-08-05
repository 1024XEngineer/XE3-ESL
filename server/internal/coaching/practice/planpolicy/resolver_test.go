package planpolicy

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestResolverFreezesCompleteRegisteredPolicy(t *testing.T) {
	t.Parallel()

	definition := scene.SceneDefinition{
		SessionPolicyRef: practice.DailyPracticeSessionPolicy,
		Prompt: scene.ScenePrompt{
			TurnBlueprints:           []string{"one", "two", "three", "four"},
			SuggestedDurationSeconds: 600,
		},
	}
	policy, err := NewResolver().ResolveSessionPolicy(
		definition,
		scene.PracticeOption{Type: scene.PracticeOptionFullSimulation},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.RetryAllowed || policy.MinEffectiveTurns != 4 ||
		policy.MaxEffectiveTurns != 6 || policy.CoverageCheckpointTurn != 4 ||
		policy.MaxFollowUpsPerQuestion != 1 {
		t.Fatalf("resolved policy = %#v", policy)
	}
}
