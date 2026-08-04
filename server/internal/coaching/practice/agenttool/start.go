package agenttool

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	PracticeStartToolName         = "practice.start.v1"
	practiceStartConfirmationKind = "practice_start"
)

type StartInput struct {
	PracticePlanID       string `json:"practice_plan_id"`
	ExpectedPlanRevision int    `json:"expected_plan_revision"`
	UserConfirmed        bool   `json:"user_confirmed"`
}

type StartResult struct {
	Status            string
	PracticeSessionID string
	PracticePlanID    string
	PlanRevision      int
	SessionStatus     string
	StartTarget       string
	Replayed          bool
	SourceRefs        []SourceRef
}

type StartPort interface {
	StartPractice(context.Context, CallContext, StartInput) (StartResult, error)
}

type StartTool struct {
	port StartPort
}

func NewStartTool(port StartPort) StartTool {
	return StartTool{port: port}
}

func (tool StartTool) Definition() Definition {
	return Definition{
		Name:        PracticeStartToolName,
		Description: "Start exactly one PracticeSession from a ready PracticePlan after an authenticated user confirmation. The model-provided user_confirmed field is descriptive only: the call also requires a matching confirmation injected by the trusted server boundary. Use only after practice.preview.v1 returned preview_ready and the user explicitly confirmed that exact Plan revision.",
		InputSchema: ObjectSchema(map[string]any{
			"practice_plan_id": IdentifierSchema(
				"Exact PracticePlan id returned by practice.preview.v1.",
			),
			"expected_plan_revision": IntegerRangeSchema(
				"Exact confirmed PracticePlan revision.",
				1,
				1000000,
			),
			"user_confirmed": map[string]any{
				"type":        "boolean",
				"description": "Must be true, and must match a trusted server-injected confirmation.",
			},
		}, []string{
			"practice_plan_id",
			"expected_plan_revision",
			"user_confirmed",
		}),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

func (tool StartTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrExecutionRejected
	}
	var parsed StartInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{}, ErrInvalidInput
	}
	result, err := tool.port.StartPractice(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	content := map[string]any{
		"status": result.Status,
	}
	if result.PracticeSessionID != "" {
		content["practice_session_id"] = result.PracticeSessionID
		content["practice_plan_id"] = result.PracticePlanID
		content["plan_revision"] = result.PlanRevision
		content["practice_session_status"] = result.SessionStatus
		content["start_target"] = result.StartTarget
		content["replayed"] = result.Replayed
	}
	return Result{Content: content, SourceRefs: result.SourceRefs}, nil
}
