package agenttool

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const LatestPracticeReportToolName = "evaluation.report.latest.v1"

type ReportFinding struct {
	Message          string   `json:"message"`
	Suggestion       string   `json:"suggestion,omitempty"`
	OriginalExcerpts []string `json:"original_excerpts,omitempty"`
}

type ReportDimension struct {
	Key                    string          `json:"key"`
	Name                   string          `json:"name"`
	Score                  *float64        `json:"score,omitempty"`
	Scale                  string          `json:"scale"`
	Strengths              []ReportFinding `json:"strengths"`
	Improvements           []ReportFinding `json:"improvements"`
	RecommendedExpressions []ReportFinding `json:"recommended_expressions"`
}

type LatestPracticeReport struct {
	Scene           string            `json:"scene"`
	SceneModel      string            `json:"scene_model"`
	CompletedAt     string            `json:"completed_at,omitempty"`
	AssessmentMode  string            `json:"assessment_mode"`
	Summary         string            `json:"summary"`
	Dimensions      []ReportDimension `json:"dimensions"`
	PriorityActions []ReportFinding   `json:"priority_actions"`
}

type LatestPracticeReportPort interface {
	LatestPracticeReport(
		context.Context,
		CallContext,
	) (LatestPracticeReport, error)
}

type LatestPracticeReportTool struct {
	port LatestPracticeReportPort
}

func NewLatestPracticeReportTool(
	port LatestPracticeReportPort,
) LatestPracticeReportTool {
	return LatestPracticeReportTool{port: port}
}

func (tool LatestPracticeReportTool) Definition() Definition {
	return Definition{
		Name:        LatestPracticeReportToolName,
		Description: "Read the current user's latest canonical Evaluation report across supported practice scenes. Use this first when the user says they just finished a practice or asks to continue reviewing it. It needs no user-supplied identifiers and returns only user-facing scores, findings, evidence excerpts, and priority actions. Never ask for or expose profile, plan, session, evaluation, finding, question, or report ids.",
		InputSchema: ObjectSchema(
			map[string]any{},
			nil,
		),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

func (tool LatestPracticeReportTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrExecutionRejected
	}
	if ValidateJSONObject(input) != nil {
		return Result{}, ErrInvalidInput
	}
	report, err := tool.port.LatestPracticeReport(ctx, call)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: map[string]any{"practice_report": report},
	}, nil
}
