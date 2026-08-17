package agentcapability

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
)

const PracticePreviewToolName = "practice.preview.v2"

const ConfirmPracticePlanActionType = "practice.plan.confirm.v1"

type PreviewInput struct {
	SceneQuery        string       `json:"scene_query,omitempty"`
	SceneIntent       *SceneIntent `json:"scene_intent,omitempty"`
	BackgroundSummary string       `json:"background_summary,omitempty"`
	IELTSPracticeMode string       `json:"ielts_practice_mode,omitempty"`
	IELTSTopicChoice  string       `json:"ielts_topic_choice,omitempty"`

	// The server resolves these fields from the catalog. They are intentionally
	// absent from the model-facing schema.
	SceneID           string   `json:"-"`
	SceneVersion      int      `json:"-"`
	SelectedRoleIDs   []string `json:"-"`
	PracticeOptionID  string   `json:"-"`
	MaxEffectiveTurns int      `json:"-"`
}

// SceneIntent contains only user-authored business facts. It never contains a
// catalog id, source discriminator, policy reference, duration, or turn count.
type SceneIntent struct {
	Scenario       string `json:"scenario,omitempty"`
	UserRole       string `json:"user_role,omitempty"`
	AIRole         string `json:"ai_role,omitempty"`
	PracticeGoal   string `json:"practice_goal,omitempty"`
	ExperienceHint string `json:"experience_hint,omitempty"`
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
	ClientAction          agentclientaction.Action
	SourceRefs            []capability.SourceRef
	AssistantText         string
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
	return capability.Definition{
		Name:        PracticePreviewToolName,
		Description: "Create one server-authoritative practice preview from the user's request. Always pass the user's natural scene wording as scene_query. Also pass scene_intent with the scenario, user role, AI counterpart role, practice goal, and WORKPLACE or LIFE_AND_TRAVEL hint when those facts are available; it is used only if the server confirms that no catalog scene matches. The server alone decides CATALOG versus CUSTOM and owns catalog ids, roles, practice options, duration, turn count, and execution policies. Extra personal conditions belong in background_summary and do not turn a matching catalog scene into CUSTOM. Interview customization stays in the interview preparation flow; IELTS customization stays in the IELTS flow. For IELTS, pass ielts_practice_mode and, for PART_1/PART_2/PART_3, one ielts_topic_choice. A result of preview_ready, ambiguous, needs_details, or rejected completes the current turn; never call this tool again until the user provides new information. Never claim a practice is ready unless this tool returns preview_ready.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"scene_query": capability.TextSchema(
				"The user's natural-language practice scene request, in any supported language.",
				500,
			),
			"scene_intent": capability.ObjectSchema(map[string]any{
				"scenario":      capability.TextSchema("Concrete situation to simulate.", 200),
				"user_role":     capability.TextSchema("The user's role in the situation.", 200),
				"ai_role":       capability.TextSchema("The counterpart role played by the AI.", 200),
				"practice_goal": capability.TextSchema("What the user wants to practise or achieve.", 500),
				"experience_hint": capability.StringEnumSchema(
					"Broad experience inferred from the request; never use this to choose CATALOG or CUSTOM.",
					"WORKPLACE", "LIFE_AND_TRAVEL", "INTERVIEW", "IELTS_SPEAKING",
				),
			}, nil),
			"background_summary": capability.TextSchema(
				"Concise preparation background assembled only from facts the user provided, such as their target, experience, and practice focus.",
				6000,
			),
			"ielts_practice_mode": capability.StringEnumSchema(
				"IELTS Speaking mode requested by the user.",
				"FULL_MOCK", "PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"For an IELTS specialty Part, the user's topic choice. random lets the server choose from the published bank.",
				"random", "person", "place", "thing", "experience",
			),
		}, []string{"scene_query"}),
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
	if parsed.SceneQuery != "" || parsed.SceneIntent != nil || parsed.BackgroundSummary != "" ||
		parsed.IELTSPracticeMode != "" {
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
	clientActions := []agentclientaction.Action(nil)
	turnOutcome := capability.TurnOutcomeContinue
	if preview.Status == "preview_ready" {
		clientActions = []agentclientaction.Action{preview.ClientAction}
		turnOutcome = capability.TurnOutcomeCompleted
	} else if preview.AssistantText != "" {
		turnOutcome = capability.TurnOutcomeCompleted
	}
	return capability.Result{
		Content:       content,
		SourceRefs:    preview.SourceRefs,
		ClientActions: clientActions,
		TurnOutcome:   turnOutcome,
		AssistantText: preview.AssistantText,
	}
}
