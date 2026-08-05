package routing

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	evaluationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agentcapability"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func newEvaluationRegistry() (*capability.Registry, error) {
	tools := capabilityfixture.Tools(capabilityfixture.NewStore())
	ports := evaluationPorts{}
	tools = append(
		tools,
		preparationcapability.NewPreviewTool(ports),
		evaluationcapability.NewLatestPracticeReportTool(ports),
	)
	return capability.NewRegistry(tools...)
}

type evaluationPorts struct{}

func (evaluationPorts) PreviewPractice(
	context.Context,
	capability.CallContext,
	preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	return preparationcapability.PreviewResult{
		Status: "preview_ready",
		Handoff: agenthandoff.Item{
			Type:                     agenthandoff.ConfirmPracticePlanType,
			Label:                    "确认并开始练习",
			PracticePlanID:           "00000000-0000-4000-8000-000000000001",
			PlanRevision:             1,
			Target:                   "准备英文产品经理面试",
			SceneName:                "英文产品经理面试",
			SceneFamily:              "interview",
			SceneModel:               "structured_interview",
			Roles:                    []string{"产品负责人"},
			PracticeScope:            "完整模拟",
			SuggestedDurationSeconds: 900,
			MinEffectiveTurns:        1,
			MaxEffectiveTurns:        3,
			ExecutableStatus:         agenthandoff.PracticePlanReadyStatus,
			ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
		},
	}, nil
}

func (evaluationPorts) LatestPracticeReport(
	context.Context,
	capability.CallContext,
) (evaluationcapability.LatestPracticeReport, error) {
	return evaluationcapability.LatestPracticeReport{
		Scene:          "英文产品经理面试",
		AssessmentMode: "interview",
	}, nil
}
