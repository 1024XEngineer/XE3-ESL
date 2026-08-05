package agentcapability

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
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
	SceneID                 string                  `json:"scene_id"`
	SceneVersion            int                     `json:"scene_version"`
	Name                    string                  `json:"name"`
	PracticeExperience      string                  `json:"practice_experience"`
	SceneCategory           string                  `json:"scene_category"`
	DefaultRoleIDs          []string                `json:"default_role_ids"`
	DefaultPracticeOptionID string                  `json:"default_practice_option_id"`
	PracticeOptions         []CatalogPracticeOption `json:"practice_options"`
}

type CatalogPracticeOption struct {
	ID          string `json:"practice_option_id"`
	DisplayName string `json:"display_name"`
	Mode        string `json:"practice_mode"`
}

type PreviewResult struct {
	Status                string
	RequiredMissingFields []string
	Candidates            []CatalogCandidate
	Replayed              bool
	Handoff               agenthandoff.Item
	SourceRefs            []capability.SourceRef
}

type PreviewPort interface {
	PreviewPractice(
		context.Context,
		capability.CallContext,
		PreviewInput,
	) (PreviewResult, error)
}

type PreviewTool struct {
	port PreviewPort
}

func NewPreviewTool(port PreviewPort) PreviewTool {
	return PreviewTool{port: port}
}

func (value PreviewTool) Definition() capability.Definition {
	identifierArray := map[string]any{
		"type":        "array",
		"description": "Exact role definition ids selected from the Scene catalog.",
		"items":       capability.IdentifierSchema("One exact role definition id."),
		"minItems":    1,
		"maxItems":    8,
	}
	return capability.Definition{
		Name:        PracticePreviewToolName,
		Description: "Resolve and create a server-authoritative PracticePlan for the current Agent thread. Use when the user wants to configure an English practice after identifying a Scene. Pass background_summary using only facts the user already provided; Preparation creates the Profile and immutable Snapshot internally. IELTS Scenes require an explicit ielts_selection; never infer or automatically choose a question set. Missing user-facing details return needs_input without creating a Plan. All identifiers are internal: never ask the user to provide, repeat, or understand an id. A preview_ready result creates only a PracticePlan and never starts a PracticeSession. Do not use for creating a Goal, starting practice, or claiming that the user confirmed.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"scene_query": capability.TextSchema(
				"Natural-language catalog query used only to return or resolve official Scene candidates.",
				500,
			),
			"background_summary": capability.TextSchema(
				"Concise preparation background assembled only from facts the user provided, such as their target, experience, and practice focus.",
				6000,
			),
			"goal_id": capability.IdentifierSchema(
				"Optional internal Goal id returned by a capability in this conversation. Never request it from the user.",
			),
			"scene_id": capability.IdentifierSchema("Exact official Scene id."),
			"scene_version": capability.IntegerRangeSchema(
				"Exact Scene version.",
				1,
				1000000,
			),
			"selected_role_ids": identifierArray,
			"practice_option_id": capability.IdentifierSchema(
				"Exact official Practice option id.",
			),
			"max_effective_turns": capability.IntegerRangeSchema(
				"Maximum effective turns for this Practice Plan.",
				1,
				100,
			),
			"ielts_selection": capability.ObjectSchema(map[string]any{
				"part_1_set_id": capability.IdentifierSchema(
					"Exact published Part 1 set id.",
				),
				"topic_group_id": capability.IdentifierSchema(
					"Exact published Part 2/3 topic-group id.",
				),
			}, nil),
		}, nil),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (value PreviewTool) ClassifyInvocationEffect(
	input json.RawMessage,
) (capability.InvocationEffect, error) {
	parsed, err := parsePreviewInput(input)
	if err != nil {
		return 0, err
	}
	if parsed.BackgroundSummary != "" && parsed.MaxEffectiveTurns > 0 {
		return capability.InvocationEffectMayWrite, nil
	}
	return capability.InvocationEffectReadOnly, nil
}

func (value PreviewTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if value.port == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	parsed, err := parsePreviewInput(input)
	if err != nil {
		return capability.Result{}, err
	}
	result, err := value.port.PreviewPractice(ctx, call, parsed)
	if err != nil {
		return capability.Result{}, err
	}
	return previewToolResult(result), nil
}

func parsePreviewInput(input json.RawMessage) (PreviewInput, error) {
	var parsed PreviewInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return PreviewInput{}, capability.ErrInvalidInput
	}
	return parsed, nil
}

func previewToolResult(preview PreviewResult) capability.Result {
	content := map[string]any{
		"status":                  preview.Status,
		"required_missing_fields": preview.RequiredMissingFields,
		"catalog_candidates":      preview.Candidates,
	}
	if preview.Status == "preview_ready" {
		content["target"] = preview.Handoff.Target
		content["scene_name"] = preview.Handoff.SceneName
		content["roles"] = preview.Handoff.Roles
		content["practice_scope"] = preview.Handoff.PracticeScope
		content["suggested_duration_seconds"] =
			preview.Handoff.SuggestedDurationSeconds
		content["min_effective_turns"] = preview.Handoff.MinEffectiveTurns
		content["max_effective_turns"] = preview.Handoff.MaxEffectiveTurns
		content["confirmation_required"] = true
		content["replayed"] = preview.Replayed
	}
	handoffs := []agenthandoff.Item(nil)
	if preview.Status == "preview_ready" {
		handoffs = []agenthandoff.Item{preview.Handoff}
	}
	return capability.Result{
		Content:    content,
		SourceRefs: preview.SourceRefs,
		Handoffs:   handoffs,
	}
}
