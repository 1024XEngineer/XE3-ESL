package scene

import (
	"context"
	"reflect"
	"testing"
)

func TestPreviewCatalogManifestAndExactSelectorShareOneActiveUniverse(
	t *testing.T,
) {
	catalog := mustBuiltinCatalog(t)
	active, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	if len(active) != len(manifest.Scenes) {
		t.Fatalf("active=%d manifest=%d", len(active), len(manifest.Scenes))
	}
	manifestScenes := make(map[string]CatalogSceneManifest, len(manifest.Scenes))
	for _, item := range manifest.Scenes {
		manifestScenes[item.SceneID] = item
	}
	for _, definition := range active {
		item, found := manifestScenes[definition.ID]
		if !found || item.SceneVersion != definition.Version {
			t.Fatalf("active scene %q missing or stale in manifest", definition.ID)
		}
		selection, resolveErr := catalog.ResolvePreviewCatalogSelection(
			context.Background(),
			definition.ID,
		)
		if resolveErr != nil || selection.Scene.ID != definition.ID {
			t.Fatalf("exact scene %q selection=%#v error=%v", definition.ID, selection, resolveErr)
		}
	}
}

func TestPreviewCatalogManifestIsDeterministic(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	first, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("first manifest error = %v", err)
	}
	second, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("second manifest error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("manifest changed between reads:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestCatalogManifestValidRejectsIncompleteOrUnstableDTO(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	mutations := map[string]func(*CatalogManifest){
		"unsorted scenes": func(value *CatalogManifest) {
			value.Scenes[0], value.Scenes[1] = value.Scenes[1], value.Scenes[0]
		},
		"missing alias": func(value *CatalogManifest) {
			value.Scenes[0].Aliases = nil
		},
		"shared alias": func(value *CatalogManifest) {
			value.Scenes[1].Aliases[0] = value.Scenes[0].Aliases[0]
		},
		"missing brief": func(value *CatalogManifest) {
			value.Scenes[0].PublicSceneBrief = ""
		},
		"missing default": func(value *CatalogManifest) {
			value.Experiences[0].DefaultSceneID = "missing"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copyValue := manifest
			copyValue.Scenes = append([]CatalogSceneManifest(nil), manifest.Scenes...)
			for index := range copyValue.Scenes {
				copyValue.Scenes[index].Aliases = append(
					[]string(nil),
					manifest.Scenes[index].Aliases...,
				)
			}
			copyValue.Experiences = append(
				[]CatalogExperienceManifest(nil),
				manifest.Experiences...,
			)
			for index := range copyValue.Experiences {
				copyValue.Experiences[index].Aliases = append(
					[]string(nil),
					manifest.Experiences[index].Aliases...,
				)
			}
			mutate(&copyValue)
			if copyValue.Valid() {
				t.Fatalf("mutated manifest is valid: %#v", copyValue)
			}
		})
	}
}
