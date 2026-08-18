package agentcapability

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const readyPlanID = "00000000-0000-4000-8000-000000000001"

func TestPreviewToolDefinitionsPublishRequiredProviderCompatibleSchemas(
	t *testing.T,
) {
	catalog := newTestCatalog(t)
	port := newTestServicePort(t, &readyPlanApplication{catalog: catalog}, catalog)
	tool, err := NewPreviewTool(port)
	if err != nil {
		t.Fatalf("NewPreviewTool() error = %v", err)
	}
	definition := tool.Definition()
	if err := capability.ValidateDefinition(definition); err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	resolutionKind := properties["resolution_kind"].(map[string]any)
	if !reflect.DeepEqual(
		resolutionKind["enum"],
		[]string{"CATALOG", "CUSTOM", "NEEDS_CLARIFICATION"},
	) {
		t.Fatalf("resolution_kind enum = %#v", resolutionKind["enum"])
	}
	ids := properties["catalog_scene_ids"].(map[string]any)["items"].(map[string]any)["enum"].([]string)
	if len(ids) == 0 || !sort.StringsAreSorted(ids) {
		t.Fatalf("catalog_scene_ids enum = %#v", ids)
	}
	if !reflect.DeepEqual(
		definition.InputSchema["required"],
		[]string{
			"scene_query",
			"resolution_kind",
			"catalog_scene_ids",
			"custom_scenario",
			"custom_experience_hint",
		},
	) {
		t.Fatalf("required = %#v", definition.InputSchema["required"])
	}
	if properties["custom_scenario"] == nil ||
		properties["custom_experience_hint"] == nil ||
		properties["scene_intent"] != nil {
		t.Fatalf("properties = %#v", properties)
	}
	for _, value := range []string{
		"CATALOG", "CUSTOM", "NEEDS_CLARIFICATION",
		"Broad experience defaults",
		"INTERVIEW | aliases: interview, job interview, 英文面试, 面试 | default_scene_id: scn_interview_self_introduction",
		"WORKPLACE | aliases: workplace, workplace english, 职场, 职场英语 | default_scene_id: scn_workplace_progress_risk_update",
		"scn_interview_self_introduction",
		"scn_travel_hotel_checkin",
		"Trusted Catalog manifest",
		`"catalog_scene_ids":[]`,
		`"custom_scenario":""`,
		`"custom_experience_hint":"NONE"`,
	} {
		if !strings.Contains(definition.Description, value) {
			t.Fatalf("description does not contain %q", value)
		}
	}
	if !strings.Contains(definition.Description, catalogTemplateSelectionPolicy) {
		t.Fatal("tool definition omits the Catalog selection policy")
	}
}

func TestParsePreviewToolInputsAcceptRequiredShapes(t *testing.T) {
	tests := []string{`{
  "scene_query":"明天入住英文酒店",
  "resolution_kind":"CATALOG",
  "catalog_scene_ids":["scn_travel_hotel_checkin"],
  "custom_scenario":"",
  "custom_experience_hint":"NONE",
  "background_summary":"明天入住，没有找到海景房预订"
}`,
		`{
  "scene_query":"在展会上介绍仓储机器人",
  "resolution_kind":"CUSTOM",
  "catalog_scene_ids":[],
  "custom_scenario":"在展会上介绍仓储机器人",
  "custom_experience_hint":"WORKPLACE"
}`,
		`{
  "scene_query":"我想练会议英语",
  "resolution_kind":"NEEDS_CLARIFICATION",
  "catalog_scene_ids":["scene_a","scene_b"],
  "custom_scenario":"",
  "custom_experience_hint":"NONE"
}`}
	for _, raw := range tests {
		parsed, err := parseUnifiedPreviewToolInput(json.RawMessage(raw))
		if err != nil || !validPreviewInputShape(parsed.previewInput()) {
			t.Fatalf("parseUnifiedPreviewToolInput(%s) = %#v, %v", raw, parsed, err)
		}
	}
}

func TestParsePreviewToolInputRejectsInconsistentSelection(t *testing.T) {
	inputs := map[string]string{
		"missing required":            `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scene_a"]}`,
		"null required":               `{"scene_query":"新场景","resolution_kind":"CUSTOM","catalog_scene_ids":null,"custom_scenario":"新场景","custom_experience_hint":"WORKPLACE"}`,
		"unknown kind":                `{"scene_query":"酒店入住","resolution_kind":"MATCH","catalog_scene_ids":["scene_a"],"custom_scenario":"","custom_experience_hint":"NONE"}`,
		"catalog multiple ids":        `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scene_a","scene_b"],"custom_scenario":"","custom_experience_hint":"NONE"}`,
		"catalog custom fields":       `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scene_a"],"custom_scenario":"酒店","custom_experience_hint":"WORKPLACE"}`,
		"custom missing scenario":     `{"scene_query":"新场景","resolution_kind":"CUSTOM","catalog_scene_ids":[],"custom_scenario":"","custom_experience_hint":"WORKPLACE"}`,
		"custom missing experience":   `{"scene_query":"新场景","resolution_kind":"CUSTOM","catalog_scene_ids":[],"custom_scenario":"新场景","custom_experience_hint":"NONE"}`,
		"custom with id":              `{"scene_query":"新场景","resolution_kind":"CUSTOM","catalog_scene_ids":["scene_a"],"custom_scenario":"新场景","custom_experience_hint":"WORKPLACE"}`,
		"clarification empty":         `{"scene_query":"会议","resolution_kind":"NEEDS_CLARIFICATION","catalog_scene_ids":[],"custom_scenario":"","custom_experience_hint":"NONE"}`,
		"clarification custom fields": `{"scene_query":"会议","resolution_kind":"NEEDS_CLARIFICATION","catalog_scene_ids":["scene_a"],"custom_scenario":"会议","custom_experience_hint":"WORKPLACE"}`,
		"unknown field":               `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scene_a"],"custom_scenario":"","custom_experience_hint":"NONE","guess":true}`,
		"trailing json":               `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scene_a"],"custom_scenario":"","custom_experience_hint":"NONE"} {}`,
	}
	for name, raw := range inputs {
		t.Run(name, func(t *testing.T) {
			if _, err := parseUnifiedPreviewToolInput(json.RawMessage(raw)); !errors.Is(err, capability.ErrInvalidInput) {
				t.Fatalf("parseUnifiedPreviewToolInput() error = %v", err)
			}
		})
	}
}

func TestPreviewToolInvocationEffectsFollowToolBoundary(t *testing.T) {
	tool := newTestPreviewTool(t)
	tests := []struct {
		name string
		raw  string
		want capability.InvocationEffect
	}{
		{
			name: "catalog may write",
			raw:  `{"scene_query":"酒店入住","resolution_kind":"CATALOG","catalog_scene_ids":["scn_travel_hotel_checkin"],"custom_scenario":"","custom_experience_hint":"NONE"}`,
			want: capability.InvocationEffectMayWrite,
		},
		{
			name: "custom may write",
			raw:  `{"scene_query":"展会介绍机器人","resolution_kind":"CUSTOM","catalog_scene_ids":[],"custom_scenario":"展会介绍机器人","custom_experience_hint":"WORKPLACE"}`,
			want: capability.InvocationEffectMayWrite,
		},
		{
			name: "clarification is read only",
			raw:  `{"scene_query":"会议","resolution_kind":"NEEDS_CLARIFICATION","catalog_scene_ids":["scn_workplace_meeting_disagreement"],"custom_scenario":"","custom_experience_hint":"NONE"}`,
			want: capability.InvocationEffectReadOnly,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			effect, err := tool.ClassifyInvocationEffect(json.RawMessage(test.raw))
			if err != nil || effect != test.want {
				t.Fatalf("ClassifyInvocationEffect() = %v, %v", effect, err)
			}
		})
	}
}

func TestCatalogResolutionUsesExplicitIDAndPreservesNaturalRequestFacts(
	t *testing.T,
) {
	catalog := newTestCatalog(t)
	tests := []struct {
		name       string
		query      string
		background string
		sceneID    string
	}{
		{
			name:       "personalized interview self introduction",
			query:      "我要面试 Java 后端岗位，重点讲支付系统重构，直接帮我创建自我介绍练习。",
			background: "Java 后端岗位；重点经历是支付系统重构。",
			sceneID:    "scn_interview_self_introduction",
		},
		{
			name:       "hotel with missing sea view reservation",
			query:      "明天入住英文酒店，我没有找到海景房预订，直接帮我创建练习。",
			background: "明天入住；没有找到海景房预订。",
			sceneID:    "scn_travel_hotel_checkin",
		},
		{
			name:       "workplace meeting disagreement",
			query:      "我要在会议上礼貌反对发布日期，直接帮我创建练习。",
			background: "需要在会议上礼貌反对发布日期。",
			sceneID:    "scn_workplace_meeting_disagreement",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans := &readyPlanApplication{catalog: catalog}
			port := newTestServicePort(t, plans, catalog)
			result, err := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				catalogPreviewInput(test.query, test.sceneID, test.background),
			)
			if err != nil {
				t.Fatalf("PreviewPractice() error = %v", err)
			}
			assertReadyPreview(t, result, PreviewPlanSourceCatalog)
			if plans.request.SceneID != test.sceneID ||
				plans.request.BackgroundSummary != test.background {
				t.Fatalf("plan request = %#v", plans.request)
			}
		})
	}
}

func TestSceneQueryIsEvidenceOnlyAndCannotOverrideExplicitCatalogID(t *testing.T) {
	catalog := newTestCatalog(t)
	plans := &readyPlanApplication{catalog: catalog}
	port := newTestServicePort(t, plans, catalog)
	result, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		catalogPreviewInput(
			"这句原话故意写机场值机，但正式选择是酒店入住。",
			"scn_travel_hotel_checkin",
			"用户明确选择酒店入住。",
		),
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	assertReadyPreview(t, result, PreviewPlanSourceCatalog)
	if plans.request.SceneID != "scn_travel_hotel_checkin" {
		t.Fatalf("plan request = %#v", plans.request)
	}
}

func TestCatalogResolutionPreservesOriginalQueryWhenSummaryIsAbsent(t *testing.T) {
	catalog := newTestCatalog(t)
	plans := &readyPlanApplication{catalog: catalog}
	port := newTestServicePort(t, plans, catalog)
	const query = "明天入住英文酒店，我没有找到海景房预订。"
	_, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		catalogPreviewInput(query, "scn_travel_hotel_checkin", ""),
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if !strings.Contains(plans.request.BackgroundSummary, query) {
		t.Fatalf("background = %q", plans.request.BackgroundSummary)
	}
}

func TestEveryManifestSceneCreatesOneFormalCatalogPlan(t *testing.T) {
	catalog := newTestCatalog(t)
	manifest, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	for _, item := range manifest.Scenes {
		t.Run(item.SceneID, func(t *testing.T) {
			plans := &readyPlanApplication{catalog: catalog}
			port := newTestServicePort(t, plans, catalog)
			input := catalogPreviewInput(item.Name, item.SceneID, item.Name)
			result, previewErr := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				input,
			)
			if previewErr != nil {
				t.Fatalf("PreviewPractice() error = %v", previewErr)
			}
			assertReadyPreview(t, result, PreviewPlanSourceCatalog)
			if plans.request.SceneID != item.SceneID || plans.planCalls != 1 {
				t.Fatalf("plan request = %#v, calls = %d", plans.request, plans.planCalls)
			}
		})
	}
}

func TestCustomResolutionCompilesScenarioIntoFormalCustomPlan(t *testing.T) {
	catalog := newTestCatalog(t)
	plans := &readyPlanApplication{catalog: catalog}
	port := newTestServicePort(t, plans, catalog)
	result, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		PreviewInput{
			SceneQuery: "在展会上介绍仓储机器人",
			SceneResolution: SceneResolutionInput{
				Kind: SceneResolutionKindCustom,
			},
			SceneIntent: &SceneIntent{
				Scenario:       "在展会上介绍仓储机器人",
				ExperienceHint: "WORKPLACE",
			},
			BackgroundSummary: "产品用于仓库自动化。",
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	assertReadyPreview(t, result, PreviewPlanSourceCustom)
	spec := plans.customRequest.SceneSpec
	if spec.Scenario != "在展会上介绍仓储机器人" ||
		spec.UserRole != "场景中的英语学习者" ||
		spec.AIRole != "场景中的对话方" || spec.PracticeGoal == "" ||
		spec.ExperienceHint != scene.PracticeExperienceWorkplace ||
		plans.customRequest.BackgroundSummary != "产品用于仓库自动化。" {
		t.Fatalf("custom request = %#v", plans.customRequest)
	}
}

func TestCustomRestrictedExperiencesNeverWrite(t *testing.T) {
	catalog := newTestCatalog(t)
	for _, experience := range []string{"INTERVIEW", "IELTS_SPEAKING"} {
		t.Run(experience, func(t *testing.T) {
			plans := &readyPlanApplication{catalog: catalog}
			port := newTestServicePort(t, plans, catalog)
			result, err := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				PreviewInput{
					SceneQuery: "目录外的专项练习",
					SceneResolution: SceneResolutionInput{
						Kind: SceneResolutionKindCustom,
					},
					SceneIntent: &SceneIntent{
						Scenario:       "目录外的专项练习",
						ExperienceHint: experience,
					},
				},
			)
			if err != nil {
				t.Fatalf("PreviewPractice() error = %v", err)
			}
			if result.Status != PreviewOutcomeRequiresSpecializedFlow ||
				result.SceneResolution != SceneResolutionRejected ||
				plans.customPlanCalls != 0 || plans.planCalls != 0 {
				t.Fatalf("result = %#v, plan calls = %d/%d", result, plans.planCalls, plans.customPlanCalls)
			}
		})
	}
}

func TestCustomMissingExperienceRequestsDetailsWithoutWriting(t *testing.T) {
	catalog := newTestCatalog(t)
	plans := &readyPlanApplication{catalog: catalog}
	port := newTestServicePort(t, plans, catalog)
	result, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		PreviewInput{
			SceneQuery:      "和鹦鹉寄养店沟通",
			SceneResolution: SceneResolutionInput{Kind: SceneResolutionKindCustom},
			SceneIntent:     &SceneIntent{Scenario: "和鹦鹉寄养店沟通"},
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != PreviewOutcomeNeedsDetails ||
		!reflect.DeepEqual(result.RequiredMissingFields, []string{"experience_hint"}) ||
		plans.planCalls != 0 || plans.customPlanCalls != 0 {
		t.Fatalf("result = %#v, plan calls = %d/%d", result, plans.planCalls, plans.customPlanCalls)
	}
}

func TestClarificationResolutionReturnsTrustedCandidatesWithoutWriting(t *testing.T) {
	catalog := newTestCatalog(t)
	tests := []struct {
		name       string
		ids        []string
		wantStatus PreviewOutcome
	}{
		{
			name:       "one candidate asks for confirmation",
			ids:        []string{"scn_travel_hotel_checkin"},
			wantStatus: PreviewOutcomeNeedsDetails,
		},
		{
			name: "multiple candidates ask for selection",
			ids: []string{
				"scn_workplace_meeting_disagreement",
				"scn_workplace_progress_risk_update",
			},
			wantStatus: PreviewOutcomeAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans := &readyPlanApplication{catalog: catalog}
			port := newTestServicePort(t, plans, catalog)
			result, err := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				PreviewInput{
					SceneQuery: "我想练习，但还不能确定小场景",
					SceneResolution: SceneResolutionInput{
						Kind:              SceneResolutionKindNeedsClarification,
						CandidateSceneIDs: test.ids,
					},
				},
			)
			if err != nil {
				t.Fatalf("PreviewPractice() error = %v", err)
			}
			if result.Status != test.wantStatus || len(result.Candidates) != len(test.ids) ||
				plans.planCalls != 0 || plans.customPlanCalls != 0 || result.AssistantText == "" {
				t.Fatalf("result = %#v, plan calls = %d/%d", result, plans.planCalls, plans.customPlanCalls)
			}
			for index, id := range test.ids {
				if result.Candidates[index].SceneID != id {
					t.Fatalf("candidates = %#v", result.Candidates)
				}
			}
		})
	}
}

func TestUnknownManifestIDIsRejectedBeforePortExecution(t *testing.T) {
	catalog := newTestCatalog(t)
	port := newTestServicePort(t, &readyPlanApplication{catalog: catalog}, catalog)
	counting := &countingPreviewPort{manifest: port.PreviewCatalogManifest()}
	tool, err := NewPreviewTool(counting)
	if err != nil {
		t.Fatalf("NewPreviewTool() error = %v", err)
	}
	_, err = tool.Execute(
		context.Background(),
		validPreviewCallContext(),
		json.RawMessage(`{
  "scene_query":"酒店入住",
  "resolution_kind":"CATALOG",
  "catalog_scene_ids":["not_in_manifest"]
}`),
	)
	if !errors.Is(err, capability.ErrInvalidInput) || counting.calls != 0 {
		t.Fatalf("Execute() error = %v, calls = %d", err, counting.calls)
	}
}

func TestUnknownCatalogIDNeverFallsBackToCustom(t *testing.T) {
	catalog := newTestCatalog(t)
	plans := &readyPlanApplication{catalog: catalog}
	port := newTestServicePort(t, plans, catalog)
	_, err := port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		catalogPreviewInput("不存在的目录场景", "scn_not_published", ""),
	)
	if !errors.Is(err, capability.ErrInvalidInput) ||
		plans.planCalls != 0 || plans.customPlanCalls != 0 {
		t.Fatalf(
			"PreviewPractice() error = %v, calls = %d/%d",
			err,
			plans.planCalls,
			plans.customPlanCalls,
		)
	}
}

func TestExactCatalogDependencyFailureNeverFallsBackToCustom(t *testing.T) {
	base := newTestCatalog(t)
	manifest, err := base.PreviewCatalogManifest(context.Background())
	if err != nil {
		t.Fatalf("PreviewCatalogManifest() error = %v", err)
	}
	dependencyErr := errors.New("catalog unavailable")
	catalog := exactFailureCatalog{manifest: manifest, err: dependencyErr}
	plans := &readyPlanApplication{catalog: base}
	port := newTestServicePort(t, plans, catalog)
	_, err = port.PreviewPractice(
		context.Background(),
		validPreviewCallContext(),
		catalogPreviewInput("酒店入住", "scn_travel_hotel_checkin", ""),
	)
	if !errors.Is(err, dependencyErr) || plans.planCalls != 0 || plans.customPlanCalls != 0 {
		t.Fatalf("PreviewPractice() error = %v, calls = %d/%d", err, plans.planCalls, plans.customPlanCalls)
	}
}

func TestManifestDependencyFailureStopsServiceConstruction(t *testing.T) {
	dependencyErr := errors.New("manifest unavailable")
	_, err := NewServicePort(
		context.Background(),
		&readyPlanApplication{},
		exactFailureCatalog{err: dependencyErr},
	)
	if !errors.Is(err, dependencyErr) {
		t.Fatalf("NewServicePort() error = %v", err)
	}
}

func TestIELTSCatalogModeIsServerValidatedAndBackgroundIsPreserved(t *testing.T) {
	catalog := newTestCatalog(t)
	tests := []struct {
		name       string
		mode       string
		topic      string
		wantOption string
		wantStatus PreviewOutcome
	}{
		{
			name:       "full mock default",
			wantOption: "option_ielts_speaking_full_mock",
			wantStatus: PreviewOutcomeReady,
		},
		{
			name:       "part two with random topic",
			mode:       "PART_2",
			topic:      "random",
			wantOption: "option_ielts_speaking_part_2",
			wantStatus: PreviewOutcomeReady,
		},
		{
			name:       "part two missing topic",
			mode:       "PART_2",
			wantStatus: PreviewOutcomeNeedsDetails,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans := &readyPlanApplication{catalog: catalog}
			port := newTestServicePort(t, plans, catalog)
			input := catalogPreviewInput(
				"帮我创建雅思口语练习",
				"scn_ielts_speaking",
				"用户明天考试，希望练习流畅度。",
			)
			input.IELTSPracticeMode = test.mode
			input.IELTSTopicChoice = test.topic
			result, err := port.PreviewPractice(
				context.Background(),
				validPreviewCallContext(),
				input,
			)
			if err != nil {
				t.Fatalf("PreviewPractice() error = %v", err)
			}
			if result.Status != test.wantStatus {
				t.Fatalf("result = %#v", result)
			}
			if test.wantStatus == PreviewOutcomeReady {
				if plans.request.PracticeOptionID != test.wantOption ||
					plans.request.BackgroundSummary != "用户明天考试，希望练习流畅度。" {
					t.Fatalf("plan request = %#v", plans.request)
				}
			} else if plans.planCalls != 0 {
				t.Fatalf("plan calls = %d", plans.planCalls)
			}
		})
	}
}

func TestPreviewToolResultClosesOutcomeAndClientActionInvariant(t *testing.T) {
	valid := readyCustomPlan(t)
	action, err := practicePlanClientAction(valid)
	if err != nil {
		t.Fatalf("practicePlanClientAction() error = %v", err)
	}
	tests := map[string]PreviewResult{
		"ready without action": {
			Status:          PreviewOutcomeReady,
			SceneResolution: SceneResolutionCatalogResolved,
			PlanID:          readyPlanID,
			PlanSource:      PreviewPlanSourceCatalog,
		},
		"ready with mismatched source": {
			Status:          PreviewOutcomeReady,
			SceneResolution: SceneResolutionCatalogResolved,
			PlanID:          readyPlanID,
			PlanSource:      PreviewPlanSourceCustom,
			ClientAction:    action,
		},
		"non-ready with action": {
			Status:          PreviewOutcomeNeedsDetails,
			SceneResolution: SceneResolutionNeedsDetails,
			ClientAction:    action,
		},
		"unknown outcome": {
			Status:          PreviewOutcome("future"),
			SceneResolution: SceneResolutionNeedsDetails,
		},
	}
	for name, preview := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := previewToolResult(preview); !errors.Is(err, capability.ErrExecutionRejected) {
				t.Fatalf("previewToolResult() error = %v", err)
			}
		})
	}
}

func TestPracticePlanClientActionEmitsExactV2BusinessPayload(t *testing.T) {
	action, err := practicePlanClientAction(readyCustomPlan(t))
	if err != nil {
		t.Fatalf("practicePlanClientAction() error = %v", err)
	}
	if action.Type != ConfirmPracticePlanActionType {
		t.Fatalf("action type = %q", action.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		t.Fatalf("decode action payload: %v", err)
	}
	if len(payload) != len(confirmPracticePlanActionFields) {
		t.Fatalf("payload keys = %#v", payload)
	}
	for key := range confirmPracticePlanActionFields {
		if _, found := payload[key]; !found {
			t.Fatalf("payload missing %q: %#v", key, payload)
		}
	}
	if _, found := payload["target"]; found {
		t.Fatalf("v2 payload contains legacy target: %#v", payload)
	}
	if _, found := payload["roles"]; found {
		t.Fatalf("v2 payload contains legacy roles: %#v", payload)
	}
	if payload["scene_id"] == "" ||
		payload["scene_name"] != "在展会上介绍机器人" ||
		payload["user_role"] != "销售代表" ||
		payload["practice_goal"] != "说明产品价值并回应疑虑" {
		t.Fatalf("payload values = %#v", payload)
	}
}

type readyPlanApplication struct {
	catalog         scene.CatalogReader
	request         preparation.CreatePlanRequest
	customRequest   preparation.CreateCustomPlanRequest
	planCalls       int
	customPlanCalls int
}

func (application *readyPlanApplication) PreviewPlan(
	ctx context.Context,
	_ requestcontext.Actor,
	_ string,
	request preparation.CreatePlanRequest,
) (preparation.PracticePlan, bool, error) {
	application.planCalls++
	application.request = request
	selection, err := application.catalog.ResolveSelection(
		ctx,
		request.SceneID,
		request.SceneVersion,
		request.SelectedRoleIDs,
		request.PracticeOptionID,
	)
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	option, err := selection.PracticeOption()
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	return preparation.PracticePlan{
		ID:             readyPlanID,
		Version:        1,
		Status:         preparation.PlanStatusDraft,
		SceneSelection: selection,
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: option.SuggestedDurationSeconds,
			MinEffectiveTurns:        1,
			MaxEffectiveTurns:        request.MaxEffectiveTurns,
		},
	}, false, nil
}

func (application *readyPlanApplication) PreviewCustomPlan(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	request preparation.CreateCustomPlanRequest,
) (preparation.PracticePlan, bool, error) {
	application.customPlanCalls++
	application.customRequest = request
	selection, err := scene.NewCustomSelection(readyPlanID, request.SceneSpec)
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	option, err := selection.PracticeOption()
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	return preparation.PracticePlan{
		ID:             readyPlanID,
		Version:        1,
		Status:         preparation.PlanStatusDraft,
		SceneSelection: selection,
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: option.SuggestedDurationSeconds,
			MinEffectiveTurns:        1,
			MaxEffectiveTurns:        0,
		},
	}, false, nil
}

type exactFailureCatalog struct {
	manifest scene.CatalogManifest
	err      error
}

func (catalog exactFailureCatalog) ResolvePreviewCatalogSelection(
	context.Context,
	string,
) (scene.PreviewCatalogSelection, error) {
	return scene.PreviewCatalogSelection{}, catalog.err
}

func (catalog exactFailureCatalog) PreviewCatalogManifest(
	context.Context,
) (scene.CatalogManifest, error) {
	if catalog.err != nil && !catalog.manifest.Valid() {
		return scene.CatalogManifest{}, catalog.err
	}
	return catalog.manifest, nil
}

type countingPreviewPort struct {
	manifest PreviewCatalogManifest
	calls    int
}

func (port *countingPreviewPort) PreviewPractice(
	context.Context,
	capability.CallContext,
	PreviewInput,
) (PreviewResult, error) {
	port.calls++
	return PreviewResult{}, nil
}

func (port *countingPreviewPort) PreviewCatalogManifest() PreviewCatalogManifest {
	return clonePreviewCatalogManifest(port.manifest)
}

func newTestCatalog(t *testing.T) *scene.Catalog {
	t.Helper()
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(func(string) error { return nil }),
	)
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	return catalog
}

func newTestServicePort(
	t *testing.T,
	plans PlanApplication,
	catalog scene.PreviewCatalog,
) *ServicePort {
	t.Helper()
	port, err := NewServicePort(context.Background(), plans, catalog)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	return port
}

func newTestPreviewTool(t *testing.T) PreviewTool {
	t.Helper()
	catalog := newTestCatalog(t)
	port := newTestServicePort(t, &readyPlanApplication{catalog: catalog}, catalog)
	tool, err := NewPreviewTool(port)
	if err != nil {
		t.Fatalf("NewPreviewTool() error = %v", err)
	}
	return tool
}

func validPreviewCallContext() capability.CallContext {
	return capability.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		ThreadID:  "30000000-0000-4000-8000-000000000001",
		RequestID: "request-preview",
	}
}

func catalogPreviewInput(query, sceneID, background string) PreviewInput {
	return PreviewInput{
		SceneQuery: query,
		SceneResolution: SceneResolutionInput{
			Kind:           SceneResolutionKindCatalog,
			CatalogSceneID: sceneID,
		},
		BackgroundSummary: background,
	}
}

func assertReadyPreview(
	t *testing.T,
	result PreviewResult,
	source PreviewPlanSource,
) {
	t.Helper()
	wantResolution := SceneResolutionCustomResolved
	if source == PreviewPlanSourceCatalog {
		wantResolution = SceneResolutionCatalogResolved
	}
	if result.Status != PreviewOutcomeReady ||
		result.SceneResolution != wantResolution ||
		result.PlanID != readyPlanID || result.PlanSource != source ||
		result.ClientAction.Type != ConfirmPracticePlanActionType {
		t.Fatalf("ready preview = %#v", result)
	}
	toolResult, err := previewToolResult(result)
	if err != nil || len(toolResult.ClientActions) != 1 ||
		toolResult.TurnOutcome != capability.TurnOutcomeCompleted {
		t.Fatalf("previewToolResult() = %#v, %v", toolResult, err)
	}
}

func readyCustomPlan(t *testing.T) preparation.PracticePlan {
	t.Helper()
	selection, err := scene.NewCustomSelection(readyPlanID, scene.CustomSceneSpec{
		Scenario:       "在展会上介绍机器人",
		UserRole:       "销售代表",
		AIRole:         "潜在客户",
		PracticeGoal:   "说明产品价值并回应疑虑",
		ExperienceHint: scene.PracticeExperienceWorkplace,
	})
	if err != nil {
		t.Fatalf("NewCustomSelection() error = %v", err)
	}
	return preparation.PracticePlan{
		ID:             readyPlanID,
		Version:        1,
		SceneSelection: selection,
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: 480,
			MinEffectiveTurns:        1,
			MaxEffectiveTurns:        0,
		},
	}
}
