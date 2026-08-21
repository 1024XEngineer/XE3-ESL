package routing

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func newEvaluationRegistry() (*capability.Registry, *routingPorts, error) {
	tools := capabilityfixture.Tools(capabilityfixture.NewStore())
	ports, err := newRoutingPorts()
	if err != nil {
		return nil, nil, err
	}
	previewTool, err := preparationcapability.NewPreviewTool(ports)
	if err != nil {
		return nil, nil, err
	}
	tools = append(
		tools,
		preparationcapability.NewIELTSWarmUpTool(),
		previewTool,
	)
	registry, err := capability.NewRegistry(tools...)
	return registry, ports, err
}

type routingPorts struct {
	manifest preparationcapability.PreviewCatalogManifest
	mu       sync.Mutex
	inputs   map[string][]PreviewInputRecord
}

func newRoutingPorts() (*routingPorts, error) {
	manifest, err := previewCatalogManifestFixture()
	if err != nil {
		return nil, err
	}
	return &routingPorts{
		manifest: manifest,
		inputs:   make(map[string][]PreviewInputRecord),
	}, nil
}

func (ports *routingPorts) PreviewCatalogManifest() preparationcapability.PreviewCatalogManifest {
	return ports.manifest
}

func (ports *routingPorts) AuthorizePracticeTurn(
	context.Context,
	capability.ExposureRequest,
) (preparationcapability.PracticeTurnIntent, error) {
	return preparationcapability.PracticeTurnIntentRequestCreate, nil
}

func (ports *routingPorts) PreviewPractice(
	_ context.Context,
	call capability.CallContext,
	input preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	ports.record(call.RunID, input)
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindNeedsClarification {
		status := preparationcapability.PreviewOutcomeAmbiguous
		resolution := preparationcapability.SceneResolutionAmbiguous
		if len(input.SceneResolution.CandidateSceneIDs) == 1 {
			status = preparationcapability.PreviewOutcomeNeedsDetails
			resolution = preparationcapability.SceneResolutionNeedsDetails
		}
		candidates := make([]preparationcapability.CatalogCandidate, len(input.SceneResolution.CandidateSceneIDs))
		for index, id := range input.SceneResolution.CandidateSceneIDs {
			candidates[index] = preparationcapability.CatalogCandidate{SceneID: id}
		}
		return preparationcapability.PreviewResult{
			Status:                status,
			SceneResolution:       resolution,
			CatalogCandidateCount: len(candidates),
			Candidates:            candidates,
			AssistantText:         "请选择一个具体场景后再继续。",
		}, nil
	}
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindCustom &&
		(input.SceneIntent.ExperienceHint == "INTERVIEW" ||
			input.SceneIntent.ExperienceHint == "IELTS_SPEAKING") {
		return preparationcapability.PreviewResult{
			Status:           preparationcapability.PreviewOutcomeRequiresSpecializedFlow,
			SceneResolution:  preparationcapability.SceneResolutionRejected,
			ResolutionReason: preparationcapability.ResolutionReasonSpecializedFlowRequired,
			AssistantText: "面试和雅思练习使用各自的正式准备流程。" +
				"请选择目录中的面试或雅思场景。",
		}, nil
	}
	action, err := agentclientaction.New(
		preparationcapability.ConfirmPracticePlanActionType,
		json.RawMessage(`{
  "label": "确认并开始练习",
  "practice_plan_id": "00000000-0000-4000-8000-000000000001",
  "plan_version": 1,
  "scene_id": "scn_workplace_meeting_disagreement",
  "scene_name": "会议发言与表达异议",
  "user_role": "参会者",
  "ai_roles": ["会议主持人"],
  "practice_goal": "礼貌表达不同意见",
  "practice_experience": "WORKPLACE",
  "scene_category": "WORKPLACE_GENERAL",
  "practice_mode": "FULL_SIMULATION",
  "practice_scope": "完整模拟",
  "suggested_duration_seconds": 480,
  "min_effective_turns": 1,
  "max_effective_turns": 5,
  "confirmation_prompt": "确认后将创建练习会话；确认前不会开始练习。"
}`),
	)
	if err != nil {
		return preparationcapability.PreviewResult{}, err
	}
	resolution := preparationcapability.SceneResolutionCatalogResolved
	source := preparationcapability.PreviewPlanSourceCatalog
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindCustom {
		resolution = preparationcapability.SceneResolutionCustomResolved
		source = preparationcapability.PreviewPlanSourceCustom
	}
	return preparationcapability.PreviewResult{
		Status:          preparationcapability.PreviewOutcomeReady,
		SceneResolution: resolution,
		PlanID:          "00000000-0000-4000-8000-000000000001",
		PlanSource:      source,
		ClientAction:    action,
		AssistantText:   "练习已准备好，请确认开始。",
	}, nil
}

func (ports *routingPorts) record(
	runID string,
	input preparationcapability.PreviewInput,
) {
	ports.mu.Lock()
	defer ports.mu.Unlock()
	ports.inputs[runID] = append(ports.inputs[runID], PreviewInputRecord{
		Kind:              input.SceneResolution.Kind,
		CatalogSceneID:    input.SceneResolution.CatalogSceneID,
		CandidateSceneIDs: append([]string(nil), input.SceneResolution.CandidateSceneIDs...),
	})
}

func (ports *routingPorts) takeInputsForRun(runID string) []PreviewInputRecord {
	ports.mu.Lock()
	defer ports.mu.Unlock()
	inputs := ports.inputs[runID]
	delete(ports.inputs, runID)
	result := make([]PreviewInputRecord, len(inputs))
	copy(result, inputs)
	for index := range result {
		result[index].CandidateSceneIDs = append([]string(nil), inputs[index].CandidateSceneIDs...)
	}
	return result
}
