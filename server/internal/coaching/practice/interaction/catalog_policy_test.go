package interaction

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestBuiltinSceneCatalogReferencesRegisteredTurnPolicies(t *testing.T) {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(
			func(string) error { return nil },
		),
	)
	if err != nil {
		t.Fatalf("NewBuiltinCatalog: %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes: %v", err)
	}
	if len(definitions) == 0 {
		t.Fatal("active Scene catalog is empty")
	}

	checked := make(map[string]struct{})
	for _, definition := range definitions {
		for _, option := range definition.PracticeOptions {
			reference := option.TurnPolicyRef
			if _, found := checked[reference]; found {
				continue
			}
			checked[reference] = struct{}{}
			if _, err := practice.ResolveTurnPolicy(reference); err != nil {
				t.Errorf(
					"Scene %q option %q references unsupported turn policy %q: %v",
					definition.ID,
					option.ID,
					reference,
					err,
				)
			}
		}
	}
}
