package scene

import (
	"context"
	"testing"
)

func TestCatalogPreviewResolverReturnsExactScene(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustTestCatalog(t))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(context.Background(), testSceneID)
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) != 1 || items[0].Scene.ID != testSceneID ||
		len(items[0].DefaultRoleIDs) != 1 ||
		items[0].DefaultOption.Mode != PracticeModeFullSimulation {
		t.Fatalf("exact candidate = %#v", items)
	}
}

func TestCatalogPreviewResolverBoundsNaturalLanguageCandidates(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustTestCatalog(t))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(context.Background(), "interview")
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) == 0 || len(items) > MaxPreviewCatalogCandidates {
		t.Fatalf("candidate count = %d", len(items))
	}
}

func TestCatalogPreviewResolverMatchesPhoneInformationConfirmation(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	resolver, err := NewCatalogPreviewResolver(catalog)
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(context.Background(), "电话信息确认")
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) != 1 || items[0].Scene.ID != "scn_daily_phone_call" {
		t.Fatalf("phone candidates = %#v", items)
	}
}
