package agentcapability

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
)

type previewPortStub struct {
	input PreviewInput
}

func (stub *previewPortStub) PreviewPractice(
	_ context.Context,
	_ capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	stub.input = input
	handoff, err := agenthandoff.NewConfirmPracticePlan(agenthandoff.Item{
		Label:                    "确认并开始练习",
		PracticePlanID:           "10000000-0000-4000-8000-000000000001",
		PlanRevision:             1,
		Target:                   "练习项目影响力表达",
		SceneName:                "项目经历深挖",
		PracticeExperience:       "INTERVIEW",
		SceneCategory:            "INTERVIEW_PROFESSIONAL",
		PracticeMode:             "FULL_SIMULATION",
		Roles:                    []string{"面试官"},
		PracticeScope:            "完整模拟",
		SuggestedDurationSeconds: 600,
		MinEffectiveTurns:        3,
		MaxEffectiveTurns:        6,
		ExecutableStatus:         agenthandoff.PracticePlanReadyStatus,
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	})
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{
		Status:  "preview_ready",
		Handoff: handoff,
		SourceRefs: []capability.SourceRef{{
			Type: "practice_plan",
			ID:   handoff.PracticePlanID,
		}},
	}, nil
}

func TestPreviewToolMapsReadyResult(t *testing.T) {
	port := &previewPortStub{}
	result, err := NewPreviewTool(port).Execute(
		context.Background(),
		previewCallContext(),
		json.RawMessage(`{"scene_query":"IELTS"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if port.input.SceneQuery != "IELTS" ||
		result.Content["status"] != "preview_ready" ||
		result.Content["confirmation_required"] != true ||
		result.Content["replayed"] != false ||
		len(result.Content) != 3 ||
		len(result.SourceRefs) != 1 || len(result.Handoffs) != 1 ||
		result.Handoffs[0].Type != agenthandoff.ConfirmPracticePlanType {
		t.Fatalf("tool result = %#v, input = %#v", result, port.input)
	}
	for _, duplicate := range []string{
		"practice_plan_id",
		"target",
		"scene_name",
		"roles",
		"practice_scope",
		"suggested_duration_seconds",
		"min_effective_turns",
		"max_effective_turns",
		"required_missing_fields",
		"catalog_candidates",
	} {
		if _, found := result.Content[duplicate]; found {
			t.Fatalf("ready result repeats card field %q: %#v", duplicate, result.Content)
		}
	}
}

func TestPreviewToolDoesNotExposePreparationIdentifiers(t *testing.T) {
	definition := NewPreviewTool(&previewPortStub{}).Definition()
	if !strings.Contains(
		definition.Description,
		"never say a practice was created or is ready unless this tool actually returns preview_ready",
	) {
		t.Fatalf("definition permits a false ready claim: %q", definition.Description)
	}
	if required := definition.InputSchema["required"].([]string); len(required) != 0 {
		t.Fatalf("required = %#v, want optional discovery fields", required)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	if properties["background_summary"] == nil ||
		properties["preparation_profile_id"] != nil ||
		properties["preparation_snapshot_id"] != nil ||
		properties["ielts_selection"] != nil {
		t.Fatalf("properties expose internal Preparation identifiers: %#v", properties)
	}
	for field, want := range map[string][]string{
		"ielts_practice_mode": {"FULL_MOCK", "PART_1", "PART_2", "PART_3"},
		"ielts_topic_choice":  {"random", "person", "place", "thing", "experience"},
	} {
		schema := properties[field].(map[string]any)
		if !reflect.DeepEqual(schema["enum"], want) {
			t.Fatalf("%s enum = %#v, want %#v", field, schema["enum"], want)
		}
	}
}

func TestPreviewToolClassifiesDiscoveryAndCreation(t *testing.T) {
	registry, err := capability.NewRegistry(NewPreviewTool(&previewPortStub{}))
	if err != nil {
		t.Fatalf("capability.NewRegistry() error = %v", err)
	}
	readOnly := registry.InvocationEffect(capability.Invocation{
		Name:  PracticePreviewToolName,
		Input: json.RawMessage(`{"scene_query":"AI product manager interview"}`),
	})
	if readOnly != capability.InvocationEffectReadOnly {
		t.Fatalf("discovery effect = %v", readOnly)
	}
	mayWrite := registry.InvocationEffect(capability.Invocation{
		Name: PracticePreviewToolName,
		Input: json.RawMessage(
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
		),
	})
	if mayWrite != capability.InvocationEffectMayWrite {
		t.Fatalf("creation effect = %v", mayWrite)
	}
}
