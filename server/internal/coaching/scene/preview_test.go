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

func TestCatalogPreviewResolverPreservesExactNamePriority(t *testing.T) {
	selfIntroduction := previewIntentTestScene(
		"scn_interview_self_introduction",
		"英文自我介绍",
	)
	smallTalk := previewIntentTestScene(
		"scn_daily_small_talk",
		"自我介绍与寒暄",
	)
	resolver, err := NewCatalogPreviewResolver(mustTestCatalog(
		t,
		selfIntroduction,
		smallTalk,
	))
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

func TestCatalogPreviewResolverRoutesExplicitSceneIntents(t *testing.T) {
	definitions := []SceneDefinition{
		previewIntentTestScene("scn_daily_rental_viewing", "看房与租赁咨询"),
		previewIntentTestScene("scn_daily_rental_maintenance", "租房报修"),
		previewIntentTestScene("scn_daily_product_shopping", "商品咨询与购买"),
		previewIntentTestScene("scn_daily_return_refund", "换货与退款"),
		previewIntentTestScene("scn_travel_airport_checkin", "机场值机与航班信息"),
		previewIntentTestScene("scn_workplace_client_delay", "客户延期沟通"),
		previewIntentTestScene("scn_workplace_requirement_clarification", "客户需求澄清"),
		previewIntentTestScene("scn_travel_hotel_checkin", "酒店入住"),
		previewIntentTestScene("scn_workplace_feedback_conflict", "向同事提供反馈"),
		previewIntentTestScene("scn_workplace_conflict_resolution", "处理职场冲突"),
		previewIntentTestScene("scn_daily_phone_call", "电话信息确认"),
		previewIntentTestScene("scn_daily_complaint_help", "投诉与求助"),
		previewIntentTestScene("scn_workplace_solution_presentation", "方案介绍与问答"),
	}
	resolver, err := NewCatalogPreviewResolver(mustTestCatalog(t, definitions...))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	tests := map[string]string{
		"帮我创建一个租房场景":        "scn_daily_rental_viewing",
		"我家水管坏了，想练习报修":      "scn_daily_rental_maintenance",
		"我想买一件外套":           "scn_daily_product_shopping",
		"我想退掉昨天买的外套":        "scn_daily_return_refund",
		"我需要练习机场值机":         "scn_travel_airport_checkin",
		"我要向客户说明项目延期":       "scn_workplace_client_delay",
		"我想和客户澄清业务需求":       "scn_workplace_requirement_clarification",
		"我想练习办理酒店入住":        "scn_travel_hotel_checkin",
		"我要向同事提供反馈":         "scn_workplace_feedback_conflict",
		"我需要处理与同事的工作分歧":     "scn_workplace_conflict_resolution",
		"我想练习打电话确认信息":       "scn_daily_phone_call",
		"我想向工作人员求助":         "scn_daily_complaint_help",
		"面向领导、客户或技术评审的方案介绍": "scn_workplace_solution_presentation",
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

func TestCatalogPreviewResolverRejectsUnsupportedPresetIntents(t *testing.T) {
	resolver, err := NewCatalogPreviewResolver(mustTestCatalog(
		t,
		previewIntentTestScene("scn_travel_airport_checkin", "机场值机与航班信息"),
		previewIntentTestScene("scn_daily_social_invitation", "社交邀请与礼貌拒绝"),
	))
	if err != nil {
		t.Fatalf("NewCatalogPreviewResolver() error = %v", err)
	}
	for _, query := range []string{
		"我想练习坐出租车",
		"我想练习拒绝朋友邀请",
	} {
		items, resolveErr := resolver.ResolvePreviewCatalog(
			context.Background(),
			query,
		)
		if resolveErr != nil {
			t.Fatalf("ResolvePreviewCatalog(%q) error = %v", query, resolveErr)
		}
		if len(items) != 0 {
			t.Fatalf("ResolvePreviewCatalog(%q) = %#v", query, items)
		}
	}
}

func previewIntentTestScene(id, name string) SceneDefinition {
	definition := testSceneDefinition()
	definition.ID = id
	definition.Name = name
	definition.Prompt.PublicSceneBrief = "Intent routing fixture."
	definition.Prompt.PracticeGoal = "Complete the selected intent."
	definition.Prompt.UserRole = "Learner"
	definition.Prompt.AIRole = "Counterpart"
	reparentTestScene(&definition)
	return definition
}
