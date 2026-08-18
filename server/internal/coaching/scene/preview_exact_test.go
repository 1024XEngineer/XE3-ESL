package scene

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestResolvePreviewCatalogSelectionUsesExactActiveSceneID(t *testing.T) {
	catalog := mustTestCatalog(t)
	selection, err := catalog.ResolvePreviewCatalogSelection(
		context.Background(),
		testSceneID,
	)
	if err != nil {
		t.Fatalf("ResolvePreviewCatalogSelection() error = %v", err)
	}
	if !selection.Valid() || selection.Scene.ID != testSceneID ||
		!reflect.DeepEqual(selection.DefaultRoleIDs, []string{testRoleID}) ||
		selection.DefaultOption.ID != testFullOptionID {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestResolvePreviewCatalogSelectionRejectsNonIDsAndUnavailableScenes(
	t *testing.T,
) {
	catalog := mustBuiltinCatalog(t)
	for _, sceneID := range []string{
		"",
		"酒店入住",
		"明天入住英文酒店，直接帮我创建练习",
	} {
		if _, err := catalog.ResolvePreviewCatalogSelection(
			context.Background(),
			sceneID,
		); !errors.Is(err, ErrCatalogSelectionInvalid) {
			t.Fatalf("ResolvePreviewCatalogSelection(%q) error = %v", sceneID, err)
		}
	}
	for _, sceneID := range []string{"missing", "hotel-check-in"} {
		if _, err := catalog.ResolvePreviewCatalogSelection(
			context.Background(),
			sceneID,
		); !errors.Is(err, ErrSceneNotFound) {
			t.Fatalf("ResolvePreviewCatalogSelection(%q) error = %v", sceneID, err)
		}
	}

	inactive := testSceneDefinition()
	inactive.Status = SceneStatusInactive
	inactiveCatalog := mustTestCatalog(t, inactive)
	if _, err := inactiveCatalog.ResolvePreviewCatalogSelection(
		context.Background(),
		inactive.ID,
	); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("inactive selection error = %v", err)
	}
}

func TestResolvePreviewCatalogSelectionResolvesEveryBuiltinManifestID(
	t *testing.T,
) {
	catalog := mustBuiltinCatalog(t)
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	for _, item := range manifest.Scenes {
		selection, resolveErr := catalog.ResolvePreviewCatalogSelection(
			context.Background(),
			item.SceneID,
		)
		if resolveErr != nil {
			t.Fatalf("ResolvePreviewCatalogSelection(%q) error = %v", item.SceneID, resolveErr)
		}
		if !selection.Valid() || selection.Scene.ID != item.SceneID ||
			selection.Scene.Version != item.SceneVersion {
			t.Fatalf("selection(%q) = %#v", item.SceneID, selection)
		}
	}
}

func TestResolvePreviewCatalogSelectionDoesNotInterpretManifestLanguage(
	t *testing.T,
) {
	catalog := mustBuiltinCatalog(t)
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	for _, item := range manifest.Scenes {
		queries := append([]string{item.Name}, item.Aliases...)
		for _, query := range queries {
			if query == item.SceneID {
				continue
			}
			if _, resolveErr := catalog.ResolvePreviewCatalogSelection(
				context.Background(),
				query,
			); resolveErr == nil {
				t.Fatalf("non-ID %q selected Catalog scene %q", query, item.SceneID)
			}
		}
	}
}

func TestResolvePreviewCatalogSelectionFailsClosedOnInvalidDependencies(
	t *testing.T,
) {
	for name, mutate := range map[string]func(*Catalog){
		"missing roles": func(catalog *Catalog) {
			catalog.scenes[0].Roles = nil
		},
		"missing default option": func(catalog *Catalog) {
			catalog.scenes[0].PracticeOptions = catalog.scenes[0].PracticeOptions[1:]
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := mustTestCatalog(t)
			mutate(catalog)
			if _, err := catalog.ResolvePreviewCatalogSelection(
				context.Background(),
				testSceneID,
			); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("ResolvePreviewCatalogSelection() error = %v", err)
			}
		})
	}
}

func TestResolvePreviewCatalogSelectionReturnsIndependentCopies(t *testing.T) {
	catalog := mustTestCatalog(t)
	first, err := catalog.ResolvePreviewCatalogSelection(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("first selection error = %v", err)
	}
	first.Scene.Prompt.FocusAreas[0] = "mutated"
	first.DefaultRoleIDs[0] = "mutated"
	second, err := catalog.ResolvePreviewCatalogSelection(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("second selection error = %v", err)
	}
	if second.Scene.Prompt.FocusAreas[0] == "mutated" ||
		second.DefaultRoleIDs[0] == "mutated" {
		t.Fatal("exact selector exposed mutable Catalog state")
	}
}

func TestPreviewCatalogManifestIsCompleteStableAndConcise(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	if !manifest.Valid() || len(manifest.Scenes) != 27 ||
		len(manifest.Experiences) != 4 {
		t.Fatalf("manifest = %#v", manifest)
	}
	for index, item := range manifest.Scenes {
		if index > 0 && manifest.Scenes[index-1].SceneID >= item.SceneID {
			t.Fatalf("scenes are not strictly sorted: %#v", manifest.Scenes)
		}
		definition, getErr := catalog.GetScene(context.Background(), item.SceneID)
		if getErr != nil {
			t.Fatalf("GetScene(%q) error = %v", item.SceneID, getErr)
		}
		if item.SceneVersion != definition.Version ||
			item.PracticeExperience != definition.Experience ||
			item.Name != definition.Name ||
			item.PublicSceneBrief != definition.Prompt.PublicSceneBrief ||
			item.PracticeGoal != definition.Prompt.PracticeGoal ||
			!sort.StringsAreSorted(item.Aliases) {
			t.Fatalf("manifest scene = %#v, definition = %#v", item, definition)
		}
	}
	for _, item := range manifest.Experiences {
		selection, resolveErr := catalog.ResolvePreviewCatalogSelection(
			context.Background(),
			item.DefaultSceneID,
		)
		if resolveErr != nil {
			t.Fatalf("resolve default %q error = %v", item.DefaultSceneID, resolveErr)
		}
		if selection.Scene.Experience != item.Experience ||
			selection.DefaultOption.ID != item.DefaultPracticeOptionID {
			t.Fatalf("experience default = %#v, selection = %#v", item, selection)
		}
	}
}

func TestPreviewCatalogManifestReturnsIndependentCopies(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	first, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("first manifest error = %v", err)
	}
	first.Scenes[0].Aliases[0] = "mutated"
	first.Experiences[0].Aliases[0] = "mutated"
	second, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("second manifest error = %v", err)
	}
	if second.Scenes[0].Aliases[0] == "mutated" ||
		second.Experiences[0].Aliases[0] == "mutated" {
		t.Fatal("manifest exposed mutable Catalog discovery state")
	}
}

func TestPreviewCatalogManifestFailsClosedOnInvalidDependencies(t *testing.T) {
	for name, mutate := range map[string]func(*Catalog){
		"missing scene profile": func(catalog *Catalog) {
			delete(catalog.sceneDiscovery, "scn_travel_hotel_checkin")
		},
		"missing experience profile": func(catalog *Catalog) {
			delete(catalog.experienceDiscovery, PracticeExperienceWorkplace)
		},
		"unknown default scene": func(catalog *Catalog) {
			profile := catalog.experienceDiscovery[PracticeExperienceWorkplace]
			profile.DefaultSceneID = "missing"
			catalog.experienceDiscovery[PracticeExperienceWorkplace] = profile
		},
		"unknown default option": func(catalog *Catalog) {
			profile := catalog.experienceDiscovery[PracticeExperienceWorkplace]
			profile.DefaultPracticeOptionID = "missing"
			catalog.experienceDiscovery[PracticeExperienceWorkplace] = profile
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := mustBuiltinCatalog(t)
			mutate(catalog)
			if _, err := catalog.PreviewCatalogManifest(
				context.Background(),
			); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("PreviewCatalogManifest() error = %v", err)
			}
		})
	}
}

func TestPreviewCatalogBoundariesPropagateContextFailure(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	if _, err := catalog.ResolvePreviewCatalogSelection(
		nil,
		"scn_travel_hotel_checkin",
	); !errors.Is(err, ErrCatalogContextRequired) {
		t.Fatalf("ResolvePreviewCatalogSelection(nil) error = %v", err)
	}
	if _, err := catalog.PreviewCatalogManifest(nil); !errors.Is(
		err,
		ErrCatalogContextRequired,
	) {
		t.Fatalf("PreviewCatalogManifest(nil) error = %v", err)
	}
}
