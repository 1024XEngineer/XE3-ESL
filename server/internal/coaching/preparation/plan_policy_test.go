package preparation

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestPolicyCatalogRejectsUnregisteredReference(t *testing.T) {
	t.Parallel()

	selection := planSelectionFixture()
	selection.Scene.SessionPolicyRef = "unknown.session.v1"
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption: %v", err)
	}
	_, err = NewPolicyCatalog().ResolveSessionPolicy(selection.Scene, option)
	if !errors.Is(err, ErrPlanConflict) {
		t.Fatalf("ResolveSessionPolicy unknown ref error = %v", err)
	}
}

func TestPolicyCatalogUsesBlueprintCountForRegisteredIELTSPolicy(
	t *testing.T,
) {
	t.Parallel()

	selection := planSelectionFixture()
	selection.Scene.SessionPolicyRef = ieltsSpeakingPart2SessionPolicyRef
	selection.Scene.Prompt.TurnBlueprints = []string{"one", "two", "three"}
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption: %v", err)
	}
	policy, err := NewPolicyCatalog().ResolveSessionPolicy(
		selection.Scene,
		option,
	)
	if err != nil {
		t.Fatalf("ResolveSessionPolicy: %v", err)
	}
	if policy.MinEffectiveTurns != 3 || policy.MaxEffectiveTurns != 3 ||
		policy.CoverageCheckpointTurn != 3 ||
		policy.MaxFollowUpsPerQuestion != 0 {
		t.Fatalf("IELTS policy = %#v", policy)
	}
}

func TestPolicyCatalogFocusPolicyIsExplicit(t *testing.T) {
	t.Parallel()

	selection := planSelectionFixture()
	selection.PracticeOptionID = "option-focus"
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("PracticeOption: %v", err)
	}
	policy, err := NewPolicyCatalog().ResolveSessionPolicy(
		selection.Scene,
		option,
	)
	if err != nil {
		t.Fatalf("ResolveSessionPolicy: %v", err)
	}
	if policy.MinEffectiveTurns != 1 || policy.MaxEffectiveTurns != 3 ||
		policy.CoverageCheckpointTurn != 1 {
		t.Fatalf("focus policy = %#v", policy)
	}
	if option.Type != scene.PracticeOptionFocus {
		t.Fatalf("fixture option type = %q", option.Type)
	}
}
