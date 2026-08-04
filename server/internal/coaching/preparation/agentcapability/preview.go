package agentcapability

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
)

const PracticePreviewToolName = "practice.preview.v1"

type PreviewInput struct {
	SceneQuery        string                              `json:"scene_query,omitempty"`
	BackgroundSummary string                              `json:"background_summary,omitempty"`
	GoalID            string                              `json:"goal_id,omitempty"`
	SceneID           string                              `json:"scene_id,omitempty"`
	SceneVersion      int                                 `json:"scene_version,omitempty"`
	SelectedRoleIDs   []string                            `json:"selected_role_ids,omitempty"`
	PracticeOptionID  string                              `json:"practice_option_id,omitempty"`
	MaxEffectiveTurns int                                 `json:"max_effective_turns,omitempty"`
	IELTSSelection    *preparation.IELTSQuestionSelection `json:"ielts_selection,omitempty"`
}

type CatalogCandidate struct {
	SceneID                 string   `json:"scene_id"`
	SceneVersion            int      `json:"scene_version"`
	Name                    string   `json:"name"`
	SceneFamily             string   `json:"scene_family"`
	SceneModel              string   `json:"scene_model"`
	DefaultRoleIDs          []string `json:"default_role_ids"`
	DefaultPracticeOptionID string   `json:"default_practice_option_id"`
}

type PreviewResult struct {
	Status                string
	RequiredMissingFields []string
	Candidates            []CatalogCandidate
	PracticePlanID        string
	PlanRevision          int
	PracticePlanStatus    string
	SceneName             string
	SceneFamily           string
	SceneModel            string
	SelectedRoleIDs       []string
	PracticeOptionID      string
	MaxEffectiveTurns     int
	Replayed              bool
	SourceRefs            []tool.SourceRef
}

type PreviewPort interface {
	PreviewPractice(
		context.Context,
		tool.CallContext,
		PreviewInput,
	) (PreviewResult, error)
}

type PreviewTool struct {
	port PreviewPort
}

func NewPreviewTool(port PreviewPort) PreviewTool {
	return PreviewTool{port: port}
}

func (value PreviewTool) Definition() tool.Definition {
	identifierArray := map[string]any{
		"type":        "array",
		"description": "Exact role definition ids selected from the Scene catalog.",
		"items":       tool.IdentifierSchema("One exact role definition id."),
		"minItems":    1,
		"maxItems":    8,
	}
	return tool.Definition{
		Name:        PracticePreviewToolName,
		Description: "Resolve and create a server-authoritative PracticePlan for the current Agent thread. Use when the user wants to configure an English practice after identifying a Scene. Pass background_summary using only facts the user already provided; Preparation creates the Profile and immutable Snapshot internally. IELTS Scenes require an explicit ielts_selection; never infer or automatically choose a question set. Missing user-facing details return needs_input without creating a Plan. All identifiers are internal: never ask the user to provide, repeat, or understand an id. A preview_ready result creates only a PracticePlan and never starts a PracticeSession. Do not use for creating a Goal, starting practice, or claiming that the user confirmed.",
		InputSchema: tool.ObjectSchema(map[string]any{
			"scene_query": tool.TextSchema(
				"Natural-language catalog query used only to return or resolve official Scene candidates.",
				500,
			),
			"background_summary": tool.TextSchema(
				"Concise preparation background assembled only from facts the user provided, such as their target, experience, and practice focus.",
				6000,
			),
			"goal_id": tool.IdentifierSchema(
				"Optional internal Goal id returned by a capability in this conversation. Never request it from the user.",
			),
			"scene_id": tool.IdentifierSchema("Exact official Scene id."),
			"scene_version": tool.IntegerRangeSchema(
				"Exact Scene version.",
				1,
				1000000,
			),
			"selected_role_ids": identifierArray,
			"practice_option_id": tool.IdentifierSchema(
				"Exact official Practice option id.",
			),
			"max_effective_turns": tool.IntegerRangeSchema(
				"Maximum effective turns for this Practice Plan.",
				1,
				100,
			),
			"ielts_selection": tool.ObjectSchema(map[string]any{
				"mode": tool.StringEnumSchema(
					"Exact IELTS practice mode required by the selected Scene.",
					"FULL_MOCK",
					"PART_1",
					"PART_2",
					"PART_3",
				),
				"part_1_set_id": tool.IdentifierSchema(
					"Exact published Part 1 set id.",
				),
				"topic_group_id": tool.IdentifierSchema(
					"Exact published Part 2/3 topic-group id.",
				),
			}, []string{"mode"}),
		}, nil),
		ReadOnly: false,
		Risk:     tool.RiskLowRiskWrite,
	}
}

func (value PreviewTool) ClassifyInvocationEffect(
	input json.RawMessage,
) (tool.InvocationEffect, error) {
	parsed, err := parsePreviewInput(input)
	if err != nil {
		return 0, err
	}
	if parsed.BackgroundSummary != "" && parsed.MaxEffectiveTurns > 0 {
		return tool.InvocationEffectMayWrite, nil
	}
	return tool.InvocationEffectReadOnly, nil
}

func (value PreviewTool) Execute(
	ctx context.Context,
	call tool.CallContext,
	input json.RawMessage,
) (tool.Result, error) {
	if value.port == nil {
		return tool.Result{}, tool.ErrExecutionRejected
	}
	parsed, err := parsePreviewInput(input)
	if err != nil {
		return tool.Result{}, err
	}
	result, err := value.port.PreviewPractice(ctx, call, parsed)
	if err != nil {
		return tool.Result{}, err
	}
	return previewToolResult(result), nil
}

func parsePreviewInput(input json.RawMessage) (PreviewInput, error) {
	var parsed PreviewInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return PreviewInput{}, tool.ErrInvalidInput
	}
	return parsed, nil
}

func previewToolResult(preview PreviewResult) tool.Result {
	content := map[string]any{
		"status":                  preview.Status,
		"required_missing_fields": preview.RequiredMissingFields,
		"catalog_candidates":      preview.Candidates,
	}
	if preview.Status == "preview_ready" {
		content["practice_plan_id"] = preview.PracticePlanID
		content["plan_revision"] = preview.PlanRevision
		content["practice_plan_status"] = preview.PracticePlanStatus
		content["scene_name"] = preview.SceneName
		content["scene_family"] = preview.SceneFamily
		content["scene_model"] = preview.SceneModel
		content["selected_role_ids"] = preview.SelectedRoleIDs
		content["practice_option_id"] = preview.PracticeOptionID
		content["max_effective_turns"] = preview.MaxEffectiveTurns
		content["replayed"] = preview.Replayed
	}
	return tool.Result{Content: content, SourceRefs: preview.SourceRefs}
}
