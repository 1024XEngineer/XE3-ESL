package scene

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCatalogOwnsOneCanonicalSceneDefinition(t *testing.T) {
	definition, err := mustTestCatalog(t).GetScene(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("GetScene() error = %v", err)
	}
	if definition.ID != testSceneID ||
		definition.Experience != PracticeExperienceInterview ||
		definition.Category != SceneCategoryInterviewProfessional ||
		definition.Version != 1 || definition.Status != SceneStatusActive ||
		len(definition.Roles) != 1 || len(definition.PracticeOptions) != 2 {
		t.Fatalf("Scene = %#v", definition)
	}
}

func TestCatalogResolveSelectionPinsExactVersion(t *testing.T) {
	catalog := mustTestCatalog(t)
	selection, err := catalog.ResolveSelection(
		context.Background(),
		testSceneID,
		1,
		[]string{testRoleID},
		testFocusOptionID,
	)
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}
	if selection.Scene.ID != testSceneID || selection.Scene.Version != 1 ||
		!reflect.DeepEqual(selection.SelectedRoleIDs, []string{testRoleID}) ||
		selection.PracticeOptionID != testFocusOptionID {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestCatalogResolveSelectionRejectsInvalidPins(t *testing.T) {
	catalog := mustTestCatalog(t)
	tests := []struct {
		name     string
		sceneID  string
		version  int
		roleIDs  []string
		optionID string
		want     error
	}{
		{"unknown scene", "missing", 1, []string{testRoleID}, testFocusOptionID, ErrSceneNotFound},
		{"wrong version", testSceneID, 2, []string{testRoleID}, testFocusOptionID, ErrSceneNotFound},
		{"missing roles", testSceneID, 1, nil, testFullOptionID, ErrCatalogSelectionInvalid},
		{"duplicate roles", testSceneID, 1, []string{testRoleID, testRoleID}, testFullOptionID, ErrCatalogSelectionInvalid},
		{"unknown role", testSceneID, 1, []string{"missing"}, testFullOptionID, ErrRoleDefinitionNotFound},
		{"unknown option", testSceneID, 1, []string{testRoleID}, "missing", ErrPracticeOptionNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := catalog.ResolveSelection(
				context.Background(),
				test.sceneID,
				test.version,
				test.roleIDs,
				test.optionID,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCatalogReturnsIndependentCopies(t *testing.T) {
	catalog := mustTestCatalog(t)
	first, err := catalog.GetScene(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("GetScene() error = %v", err)
	}
	first.Prompt.FocusAreas[0] = "mutated"
	first.Roles[0].PracticeObjectives[0].Description = "mutated"
	first.PracticeOptions[0].DisplayName = "mutated"
	second, err := catalog.GetScene(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("GetScene() error = %v", err)
	}
	if second.Prompt.FocusAreas[0] == "mutated" ||
		second.Roles[0].PracticeObjectives[0].Description == "mutated" ||
		second.PracticeOptions[0].DisplayName == "mutated" {
		t.Fatal("Catalog exposed mutable Scene data")
	}
}

func TestCatalogHidesInactiveScene(t *testing.T) {
	definition := testSceneDefinition()
	definition.Status = SceneStatusInactive
	catalog := mustTestCatalog(t, definition)
	items, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("active Scenes = %#v", items)
	}
	if _, err := catalog.GetScene(context.Background(), definition.ID); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("GetScene() error = %v", err)
	}
}

func TestCatalogRejectsInvalidCanonicalDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SceneDefinition)
	}{
		{"missing id", func(value *SceneDefinition) { value.ID = "" }},
		{"invalid experience category", func(value *SceneDefinition) {
			value.Experience = PracticeExperienceWorkplace
		}},
		{"missing prompt", func(value *SceneDefinition) { value.Prompt.PublicSceneBrief = "" }},
		{"role parent", func(value *SceneDefinition) { value.Roles[0].SceneID = "other" }},
		{"invalid objective id", func(value *SceneDefinition) {
			value.Roles[0].PracticeObjectives[0].ID = "Evidence"
		}},
		{"blank objective description", func(value *SceneDefinition) {
			value.Roles[0].PracticeObjectives[0].Description = ""
		}},
		{"duplicate role objective", func(value *SceneDefinition) {
			value.Roles[0].PracticeObjectives = append(
				value.Roles[0].PracticeObjectives,
				value.Roles[0].PracticeObjectives[0],
			)
		}},
		{"option parent", func(value *SceneDefinition) { value.PracticeOptions[0].SceneID = "other" }},
		{"missing full simulation", func(value *SceneDefinition) { value.PracticeOptions = value.PracticeOptions[1:] }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := testSceneDefinition()
			test.mutate(&definition)
			if _, err := NewCatalog(
				[]SceneDefinition{definition},
				testPolicyValidator(),
			); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("NewCatalog() error = %v", err)
			}
		})
	}
}

func TestCatalogRejectsConflictingObjectiveDescriptionsAcrossRoles(t *testing.T) {
	definition := testSceneDefinition()
	second := definition.Roles[0]
	second.PracticeObjectives = append(
		[]PracticeObjectiveDefinition(nil),
		second.PracticeObjectives...,
	)
	second.ID = testSecondRoleID
	second.Type = "HR_INTERVIEWER"
	second.DisplayName = "HR interviewer"
	second.PracticeObjectives[0].Description = "Use evidence persuasively."
	definition.Roles = append(definition.Roles, second)
	definition.PracticeOptions = append(
		definition.PracticeOptions,
		PracticeOption{
			ID:                       "option_test_hr_focus",
			SceneID:                  testSceneID,
			RoleDefinitionID:         testSecondRoleID,
			Mode:                     PracticeModeFocus,
			DisplayName:              "HR focus",
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            "interview.test.turn.v1",
			SessionPolicyRef:         "interview.test.session.v1",
			EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
			DisplayOrder:             30,
		},
	)

	if _, err := NewCatalog(
		[]SceneDefinition{definition},
		testPolicyValidator(),
	); !errors.Is(err, ErrCatalogDefinitionInvalid) {
		t.Fatalf("NewCatalog() error = %v", err)
	}
}

func TestCatalogRejectsUnregisteredOrDisabledEvaluationPolicy(t *testing.T) {
	for _, reference := range []string{
		"unknown.fixture.evaluation.v1",
		"disabled.fixture.evaluation.v1",
	} {
		t.Run(reference, func(t *testing.T) {
			definition := testSceneDefinition()
			definition.PracticeOptions[0].EvaluationPolicyRef = reference
			if _, err := NewCatalog(
				[]SceneDefinition{definition},
				testPolicyValidator(),
			); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("NewCatalog() error = %v", err)
			}
		})
	}
}

func TestCatalogSortsScenesRolesAndOptions(t *testing.T) {
	first := testSceneDefinition()
	first.ID = "scene-b"
	first.DisplayOrder = 20
	reparentTestScene(&first)
	second := testSceneDefinition()
	second.ID = "scene-a"
	second.DisplayOrder = 10
	reparentTestScene(&second)
	catalog := mustTestCatalog(t, first, second)
	items, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("Scene order = %#v", items)
	}
}

func TestCatalogHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mustTestCatalog(t).ListActiveScenes(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrCatalogReadFailed) {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
}
