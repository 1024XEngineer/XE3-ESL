package agenttool

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const PracticePreviewToolName = "practice.preview.v1"

type PreviewInput struct {
	ScenarioQuery             string   `json:"scenario_query,omitempty"`
	BackgroundSummary         string   `json:"background_summary,omitempty"`
	MatterID                  string   `json:"matter_id,omitempty"`
	PreparationProfileID      string   `json:"preparation_profile_id,omitempty"`
	PreparationSnapshotID     string   `json:"preparation_snapshot_id,omitempty"`
	ScenarioDefinitionID      string   `json:"scenario_definition_id,omitempty"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version,omitempty"`
	ScenarioConfigID          string   `json:"scenario_config_id,omitempty"`
	ScenarioConfigVersion     int      `json:"scenario_config_version,omitempty"`
	SelectedRoleIDs           []string `json:"selected_role_ids,omitempty"`
	PracticeOptionID          string   `json:"practice_option_id,omitempty"`
	PracticeOptionVersion     int      `json:"practice_option_version,omitempty"`
	MaxEffectiveTurns         int      `json:"max_effective_turns,omitempty"`
}

type CatalogCandidate struct {
	ScenarioDefinitionID         string   `json:"scenario_definition_id"`
	ScenarioDefinitionVersion    int      `json:"scenario_definition_version"`
	Name                         string   `json:"name"`
	ScenarioFamily               string   `json:"scenario_family"`
	ScenarioModel                string   `json:"scenario_model"`
	ScenarioConfigID             string   `json:"scenario_config_id"`
	ScenarioConfigVersion        int      `json:"scenario_config_version"`
	DefaultRoleIDs               []string `json:"default_role_ids"`
	DefaultPracticeOptionID      string   `json:"default_practice_option_id"`
	DefaultPracticeOptionVersion int      `json:"default_practice_option_version"`
}

type PreviewResult struct {
	Status                string
	RequiredMissingFields []string
	Candidates            []CatalogCandidate
	PracticePlanID        string
	PlanRevision          int
	PracticePlanStatus    string
	ScenarioName          string
	ScenarioFamily        string
	ScenarioModel         string
	SelectedRoleIDs       []string
	PracticeOptionID      string
	MaxEffectiveTurns     int
	Replayed              bool
	SourceRefs            []SourceRef
}

type PreviewPort interface {
	PreviewPractice(
		context.Context,
		CallContext,
		PreviewInput,
	) (PreviewResult, error)
}

type PreviewTool struct {
	port PreviewPort
}

func NewPreviewTool(port PreviewPort) PreviewTool {
	return PreviewTool{port: port}
}

func (tool PreviewTool) Definition() Definition {
	identifierArray := map[string]any{
		"type":        "array",
		"description": "Exact role definition ids selected from the Preparation catalog.",
		"items":       IdentifierSchema("One exact role definition id."),
		"minItems":    1,
		"maxItems":    8,
	}
	return Definition{
		Name:        PracticePreviewToolName,
		Description: "Resolve and prepare a server-authoritative PracticePlan preview for the current Agent thread. Use when the user wants to configure an English practice session after identifying the desired catalog scenario. Pass background_summary using only facts the user already provided; the server creates and links Preparation data internally. Missing user-facing details return needs_input without creating a Plan. All identifiers are internal: never ask the user to provide, repeat, or understand an id. A preview_ready result creates only a PracticePlan and never starts a PracticeSession. Do not use for creating a RealityMatter, starting practice, or claiming that the user confirmed.",
		InputSchema: ObjectSchema(map[string]any{
			"scenario_query": TextSchema(
				"Natural-language catalog query used only to return or resolve official Preparation candidates.",
				500,
			),
			"background_summary": TextSchema(
				"Concise preparation background assembled only from facts the user provided, such as their target, experience, and practice focus.",
				6000,
			),
			"matter_id": IdentifierSchema(
				"Optional internal RealityMatter id returned by a tool in this conversation. Never request it from the user.",
			),
			"scenario_definition_id": IdentifierSchema(
				"Exact official Preparation scenario definition id.",
			),
			"scenario_definition_version": IntegerRangeSchema(
				"Exact scenario definition version.",
				1,
				1000000,
			),
			"scenario_config_id": IdentifierSchema(
				"Exact official scenario config id.",
			),
			"scenario_config_version": IntegerRangeSchema(
				"Exact scenario config version.",
				1,
				1000000,
			),
			"selected_role_ids": identifierArray,
			"practice_option_id": IdentifierSchema(
				"Exact official Practice option id.",
			),
			"practice_option_version": IntegerRangeSchema(
				"Exact Practice option version.",
				1,
				1000000,
			),
			"max_effective_turns": IntegerRangeSchema(
				"Maximum effective turns for this Practice preview.",
				1,
				100,
			),
		}, nil),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

func (tool PreviewTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrExecutionRejected
	}
	var parsed PreviewInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{}, ErrInvalidInput
	}
	result, err := tool.port.PreviewPractice(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	return previewToolResult(result), nil
}

func previewToolResult(preview PreviewResult) Result {
	content := map[string]any{
		"status":                  preview.Status,
		"required_missing_fields": preview.RequiredMissingFields,
		"catalog_candidates":      preview.Candidates,
	}
	if preview.Status == "preview_ready" {
		content["practice_plan_id"] = preview.PracticePlanID
		content["plan_revision"] = preview.PlanRevision
		content["practice_plan_status"] = preview.PracticePlanStatus
		content["scenario_name"] = preview.ScenarioName
		content["scenario_family"] = preview.ScenarioFamily
		content["scenario_model"] = preview.ScenarioModel
		content["selected_role_ids"] = preview.SelectedRoleIDs
		content["practice_option_id"] = preview.PracticeOptionID
		content["max_effective_turns"] = preview.MaxEffectiveTurns
		content["replayed"] = preview.Replayed
	}
	return Result{Content: content, SourceRefs: preview.SourceRefs}
}
