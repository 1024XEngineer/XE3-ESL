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

func TestCatalogPreviewResolverReturnsOnlyExactSceneName(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustBuiltinCatalog(t))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(
		context.Background(),
		"英文自我介绍",
	)
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) != 1 ||
		items[0].Scene.ID != "scn_interview_self_introduction" {
		t.Fatalf("exact-name candidates = %#v", items)
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

func TestCatalogPreviewResolverMatchesHotelCheckin(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	resolver, err := NewCatalogPreviewResolver(catalog)
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(context.Background(), "酒店入住")
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) != 1 || items[0].Scene.ID != "scn_travel_hotel_checkin" {
		t.Fatalf("hotel candidates = %#v", items)
	}
}

func TestCatalogPreviewResolverSeparatesSplitScenarios(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	resolver, err := NewCatalogPreviewResolver(catalog)
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	for query, wantID := range map[string]string{
		"向同事提供反馈": "scn_workplace_feedback_conflict",
		"处理职场冲突":  "scn_workplace_conflict_resolution",
		"客户延期沟通":  "scn_workplace_client_delay",
		"客户需求澄清":  "scn_workplace_requirement_clarification",
		"商品咨询与购买": "scn_daily_product_shopping",
		"换货与退款":   "scn_daily_return_refund",
	} {
		items, err := resolver.ResolvePreviewCatalog(context.Background(), query)
		if err != nil {
			t.Fatalf("ResolvePreviewCatalog(%q) error = %v", query, err)
		}
		if len(items) != 1 || items[0].Scene.ID != wantID {
			t.Fatalf("ResolvePreviewCatalog(%q) = %#v", query, items)
		}
	}
}

func TestCatalogPreviewResolverMatchesSentenceShapedChineseQueries(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustBuiltinCatalog(t))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	tests := map[string]string{
		"我想练习口语":       "scn_ielts_speaking",
		"我想练习看房":       "scn_daily_rental_viewing",
		"我家水管坏了，想练习报修": "scn_daily_rental_maintenance",
	}
	for query, wantSceneID := range tests {
		t.Run(query, func(t *testing.T) {
			items, resolveErr := resolver.ResolvePreviewCatalog(
				context.Background(),
				query,
			)
			if resolveErr != nil {
				t.Fatalf("ResolvePreviewCatalog() error = %v", resolveErr)
			}
			if len(items) != 1 || items[0].Scene.ID != wantSceneID {
				t.Fatalf("candidates = %#v, want only %q", items, wantSceneID)
			}
		})
	}
}

func TestCatalogPreviewResolverKeepsGenericInterviewAmbiguous(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustBuiltinCatalog(t))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	items, err := resolver.ResolvePreviewCatalog(context.Background(), "面试")
	if err != nil {
		t.Fatalf("ResolvePreviewCatalog() error = %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("generic interview candidates = %#v, want multiple", items)
	}
}
