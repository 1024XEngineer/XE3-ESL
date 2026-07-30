package preparation

import "testing"

func TestCatalogPreviewResolverReturnsOfficialExactCandidate(t *testing.T) {
	catalog, err := NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	resolver, err := NewCatalogPreviewResolver(catalog)
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(
		ProgrammerInterviewScenarioID,
	)
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) != 1 ||
		items[0].ScenarioDefinition.ID != ProgrammerInterviewScenarioID ||
		items[0].ScenarioConfig.ID != BackendEngineerConfigID ||
		len(items[0].DefaultRoleIDs) != 1 ||
		items[0].DefaultOption.Type != PracticeOptionFullSimulation {
		t.Fatalf("exact candidate = %#v", items)
	}
}

func TestCatalogPreviewResolverBoundsNaturalLanguageCandidates(t *testing.T) {
	catalog, err := NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	resolver, err := NewCatalogPreviewResolver(catalog)
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog("interview")
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) == 0 || len(items) > MaxPreviewCatalogCandidates {
		t.Fatalf("candidate count = %d", len(items))
	}
}
