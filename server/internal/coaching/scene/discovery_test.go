package scene

import "testing"

func TestBuiltinDiscoveryLoadsWithNormalizedUniquePhrases(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	if len(catalog.sceneDiscovery) != 27 || len(catalog.experienceDiscovery) != 4 {
		t.Fatalf(
			"discovery profiles = %d scenes, %d experiences",
			len(catalog.sceneDiscovery),
			len(catalog.experienceDiscovery),
		)
	}
}

func TestDiscoveryPhraseValidationRejectsPunctuationEquivalentDuplicates(
	t *testing.T,
) {
	if validDiscoveryPhrases([]string{"hotel check-in", "hotel check in"}) {
		t.Fatal("punctuation-equivalent discovery phrases were accepted")
	}
}
