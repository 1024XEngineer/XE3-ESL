package eval

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	evaluationtool "github.com/1024XEngineer/XE3-ESL/server/internal/evaluation/agenttool"
	practicetool "github.com/1024XEngineer/XE3-ESL/server/internal/practice/agenttool"
)

func newEvaluationRegistry() (*tool.Registry, error) {
	tools := mocktool.Tools(mocktool.NewStore())
	ports := evaluationPorts{}
	tools = append(
		tools,
		practicetool.NewPreviewTool(ports),
		practicetool.NewStartTool(ports),
		evaluationtool.NewLatestPracticeReportTool(ports),
	)
	return tool.NewRegistry(tools...)
}

type evaluationPorts struct{}

func (evaluationPorts) PreviewPractice(
	context.Context,
	tool.CallContext,
	practicetool.PreviewInput,
) (practicetool.PreviewResult, error) {
	return practicetool.PreviewResult{
		Status:             "preview_ready",
		PracticePlanID:     "eval-practice-plan-001",
		PlanRevision:       1,
		PracticePlanStatus: "ready",
		ScenarioName:       "英文产品经理面试",
		ScenarioFamily:     "interview",
		ScenarioModel:      "structured_interview",
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
