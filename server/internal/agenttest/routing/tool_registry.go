package routing

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agenttest/capabilityfixture"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	evaluationtool "github.com/1024XEngineer/XE3-ESL/server/internal/evaluation/agenttool"
	practicetool "github.com/1024XEngineer/XE3-ESL/server/internal/practice/agenttool"
)

func newEvaluationRegistry() (*tool.Registry, error) {
	tools := capabilityfixture.Tools(capabilityfixture.NewStore())
	ports := evaluationPorts{}
	tools = append(
		tools,
		preparationcapability.NewPreviewTool(ports),
		practicetool.NewStartTool(ports),
		evaluationtool.NewLatestPracticeReportTool(ports),
	)
	return tool.NewRegistry(tools...)
}

type evaluationPorts struct{}

func (evaluationPorts) PreviewPractice(
	context.Context,
	tool.CallContext,
	preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	return preparationcapability.PreviewResult{
		Status:             "preview_ready",
		PracticePlanID:     "eval-practice-plan-001",
		PlanRevision:       1,
		PracticePlanStatus: "ready",
		SceneName:          "英文产品经理面试",
		SceneFamily:        "interview",
		SceneModel:         "structured_interview",
		SelectedRoleIDs:    []string{"eval-role-001"},
		PracticeOptionID:   "eval-option-001",
		MaxEffectiveTurns:  3,
	}, nil
}

func (evaluationPorts) StartPractice(
	context.Context,
	tool.CallContext,
	practicetool.StartInput,
) (practicetool.StartResult, error) {
	return practicetool.StartResult{
		Status:            "started",
		PracticeSessionID: "eval-practice-session-001",
		PracticePlanID:    "eval-practice-plan-001",
		PlanRevision:      1,
		SessionStatus:     "active",
		StartTarget:       "immersive_roleplay",
	}, nil
}

func (evaluationPorts) LatestPracticeReport(
	context.Context,
	tool.CallContext,
) (evaluationtool.LatestPracticeReport, error) {
	return evaluationtool.LatestPracticeReport{
		Scene:          "英文产品经理面试",
		AssessmentMode: "interview",
	}, nil
}
