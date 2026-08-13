package agentcapability

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
)

const PracticePreviewToolName = "practice.preview.v1"

type PreviewInput struct {
	SceneQuery        string   `json:"scene_query,omitempty"`
	BackgroundSummary string   `json:"background_summary,omitempty"`
	GoalID            string   `json:"goal_id,omitempty"`
	SceneID           string   `json:"scene_id,omitempty"`
	SceneVersion      int      `json:"scene_version,omitempty"`
	SelectedRoleIDs   []string `json:"selected_role_ids,omitempty"`
	PracticeOptionID  string   `json:"practice_option_id,omitempty"`
	MaxEffectiveTurns int      `json:"max_effective_turns,omitempty"`
	IELTSPracticeMode string   `json:"ielts_practice_mode,omitempty"`
	IELTSTopicChoice  string   `json:"ielts_topic_choice,omitempty"`
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
		Description: "Resolve and create a server-authoritative PracticePlan for the current Agent thread. Use after any optional IELTS warm-up is complete, when the user skips warm-up, or when arranging a full mock. For non-IELTS practice, pass background_summary using only facts the user already provided; Preparation creates the Profile and immutable Snapshot internally. For IELTS, pass ielts_practice_mode and, for PART_1/PART_2/PART_3, exactly one ielts_topic_choice; the server derives the preparation background from those choices. The server selects and freezes questions from the current published bank; never invent or request question ids. Omit max_effective_turns for IELTS because the frozen questions determine it. FULL_MOCK must omit ielts_topic_choice and never reveals questions before practice. Missing user-facing details return needs_input without creating a Plan; never say a practice was created or is ready unless this tool actually returns preview_ready. All identifiers are internal: never ask the user to provide, repeat, or understand an id. A preview_ready result creates only a PracticePlan and never starts a PracticeSession. Do not use for creating a Goal, starting practice, or claiming that the user confirmed.",
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
				"Maximum effective turns for a non-IELTS Practice Plan. Omit for IELTS.",
				1,
				100,
			),
			"ielts_practice_mode": capability.StringEnumSchema(
				"IELTS Speaking mode requested by the user.",
				"FULL_MOCK", "PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"For an IELTS specialty Part, the user's topic choice. random lets the server choose from the published bank.",
				"random", "person", "place", "thing", "experience",
			),
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
	if parsed.BackgroundSummary != "" || parsed.IELTSPracticeMode != "" {
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
	content := map[string]any{"status": preview.Status}
	if preview.Status == "preview_ready" {
		content["confirmation_required"] = true
		content["replayed"] = preview.Replayed
	} else {
		content["required_missing_fields"] = preview.RequiredMissingFields
		content["catalog_candidates"] = preview.Candidates
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
