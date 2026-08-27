package agentcapability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
)

const PracticePreviewToolName = "practice.preview.v3"

const ConfirmPracticePlanActionType = "practice.plan.confirm.v2"

const noCustomExperienceHint = "NONE"

const catalogTemplateSelectionPolicy = "Treat every Catalog scene as a parameterizable interaction template, not a literal script. " +
	"The four broad experiences are categories, not scenes. A broad category alone, or a message that says the user has not decided what to practise, is insufficient to choose one scene. " +
	"When multiple listed scenes could fit, use NEEDS_CLARIFICATION with at most three representative candidates, not the entire catalog, and let the assistant ask a natural question. Do not silently select an experience default. " +
	"Choose a Catalog scene when the user's concrete participant roles and core communication task clearly match it. " +
	"A company, product, date, room type, incident, constraint, counterpart attitude, or the words custom/定制 are background details and never by themselves make a scene directory-external. "

type SceneResolutionKind string

const (
	SceneResolutionKindCatalog            SceneResolutionKind = "CATALOG"
	SceneResolutionKindCustom             SceneResolutionKind = "CUSTOM"
	SceneResolutionKindNeedsClarification SceneResolutionKind = "NEEDS_CLARIFICATION"
	SceneResolutionKindNone               SceneResolutionKind = "NONE"
)

type PracticeTurnIntent string

const (
	PracticeTurnIntentConverse       PracticeTurnIntent = "CONVERSE"
	PracticeTurnIntentRequestCreate  PracticeTurnIntent = "REQUEST_CREATE"
	PracticeTurnIntentProposeCreate  PracticeTurnIntent = "PROPOSE_CREATE"
	PracticeTurnIntentConfirmPending PracticeTurnIntent = "CONFIRM_PENDING"
	PracticeTurnIntentRejectPending  PracticeTurnIntent = "REJECT_PENDING"
)

// SceneResolutionInput is the model-authored decision. The server validates
// this closed union and never reclassifies scene_query with lexical matching.
type SceneResolutionInput struct {
	Kind              SceneResolutionKind `json:"kind"`
	CatalogSceneID    string              `json:"catalog_scene_id,omitempty"`
	CandidateSceneIDs []string            `json:"candidate_scene_ids,omitempty"`
}

// UnifiedPreviewToolInput keeps every branch discriminator field required and
// flat. Providers do not need conditional JSON Schema support; the server
// validates the closed-union relationships after decoding.
type UnifiedPreviewToolInput struct {
	ResolutionKind       SceneResolutionKind `json:"resolution_kind"`
	CatalogSceneIDs      []string            `json:"catalog_scene_ids"`
	CustomScenario       string              `json:"custom_scenario"`
	CustomExperienceHint string              `json:"custom_experience_hint"`
	UserRole             string              `json:"user_role,omitempty"`
	AIRole               string              `json:"ai_role,omitempty"`
	PracticeGoal         string              `json:"practice_goal,omitempty"`
	BackgroundSummary    string              `json:"background_summary,omitempty"`
	IELTSPracticeMode    string              `json:"ielts_practice_mode,omitempty"`
	IELTSTopicChoice     string              `json:"ielts_topic_choice,omitempty"`
}

type PreviewInput struct {
	ActionIntent      PracticeTurnIntent   `json:"action_intent"`
	ActionExcerpt     string               `json:"-"`
	SceneQuery        string               `json:"-"`
	InputSequence     int64                `json:"-"`
	SceneResolution   SceneResolutionInput `json:"scene_resolution"`
	SceneIntent       *SceneIntent         `json:"scene_intent,omitempty"`
	BackgroundSummary string               `json:"background_summary,omitempty"`
	IELTSPracticeMode string               `json:"ielts_practice_mode,omitempty"`
	IELTSTopicChoice  string               `json:"ielts_topic_choice,omitempty"`
}

// SceneIntent contains only user-authored Custom-scene facts. Catalog ids,
// source discriminators, duration, turn count, and policies never belong here.
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

// PreviewCatalogManifest is the immutable, trusted subset of Catalog content
// exposed to the model at tool-registration time.
type PreviewCatalogManifest struct {
	Experiences []PreviewCatalogManifestExperience
	Scenes      []PreviewCatalogManifestScene
}

type PreviewCatalogManifestExperience struct {
	PracticeExperience      string
	Aliases                 []string
	DefaultSceneID          string
	DefaultPracticeOptionID string
}

type PreviewCatalogManifestScene struct {
	SceneID            string
	Name               string
	PracticeExperience string
	Aliases            []string
	PublicSceneBrief   string
	PracticeGoal       string
}

type PreviewOutcome string

const (
	PreviewOutcomeReady                   PreviewOutcome = "preview_ready"
	PreviewOutcomeNeedsDetails            PreviewOutcome = "needs_details"
	PreviewOutcomeAmbiguous               PreviewOutcome = "ambiguous"
	PreviewOutcomeRequiresSpecializedFlow PreviewOutcome = "requires_specialized_flow"
	PreviewOutcomeActionPending           PreviewOutcome = "action_pending"
	PreviewOutcomeActionDeclined          PreviewOutcome = "action_declined"
)

type ResolvedSceneStatus string

const (
	SceneResolutionCatalogResolved ResolvedSceneStatus = "CATALOG_RESOLVED"
	SceneResolutionCustomResolved  ResolvedSceneStatus = "CUSTOM_RESOLVED"
	SceneResolutionAmbiguous       ResolvedSceneStatus = "AMBIGUOUS"
	SceneResolutionNeedsDetails    ResolvedSceneStatus = "NEEDS_DETAILS"
	SceneResolutionRejected        ResolvedSceneStatus = "REJECTED"
	SceneResolutionNotRequested    ResolvedSceneStatus = "NOT_REQUESTED"
)

type ResolutionReason string

const ResolutionReasonSpecializedFlowRequired ResolutionReason = "SPECIALIZED_FLOW_REQUIRED"

type PreviewPlanSource string

const (
	PreviewPlanSourceCatalog PreviewPlanSource = "CATALOG"
	PreviewPlanSourceCustom  PreviewPlanSource = "CUSTOM"
)

type PreviewResult struct {
	Status                PreviewOutcome
	SceneResolution       ResolvedSceneStatus
	ResolutionReason      ResolutionReason
	CatalogCandidateCount int
	PlanID                string
	PlanSource            PreviewPlanSource
	RequiredMissingFields []string
	Candidates            []CatalogCandidate
	Replayed              bool
	ClientAction          agentclientaction.Action
	SourceRefs            []capability.SourceRef
	AssistantText         string
}

type PreviewPort interface {
	AuthorizePracticeTurn(
		context.Context,
		capability.ExposureRequest,
	) (PracticeTurnIntent, error)
	PreviewPractice(
		context.Context,
		capability.CallContext,
		PreviewInput,
	) (PreviewResult, error)
	PreviewCatalogManifest() PreviewCatalogManifest
}

type PreviewTool struct {
	port       PreviewPort
	manifest   PreviewCatalogManifest
	catalogIDs map[string]struct{}
}

type previewInputError struct{ code string }

func (value previewInputError) Error() string          { return capability.ErrInvalidInput.Error() }
func (value previewInputError) Unwrap() error          { return capability.ErrInvalidInput }
func (value previewInputError) DiagnosticCode() string { return value.code }

func NewPreviewTool(port PreviewPort) (PreviewTool, error) {
	if port == nil {
		return PreviewTool{}, errors.New("preparation agent capability: preview port is required")
	}
	manifest := clonePreviewCatalogManifest(port.PreviewCatalogManifest())
	if !validPreviewCatalogManifest(manifest) {
		return PreviewTool{}, errors.New("preparation agent capability: preview catalog manifest is invalid")
	}
	catalogIDs := make(map[string]struct{}, len(manifest.Scenes))
	for _, item := range manifest.Scenes {
		catalogIDs[item.SceneID] = struct{}{}
	}
	return PreviewTool{port: port, manifest: manifest, catalogIDs: catalogIDs}, nil
}

func (value PreviewTool) AuthorizeExposure(
	ctx context.Context,
	request capability.ExposureRequest,
) (capability.ExposureDecision, error) {
	if value.port == nil {
		return capability.ExposureDecision{}, capability.ErrExecutionRejected
	}
	intent, err := value.port.AuthorizePracticeTurn(ctx, request)
	if err != nil {
		return capability.ExposureDecision{}, err
	}
	if intent == PracticeTurnIntentConverse {
		return capability.ExposureDecision{
			Expose: false, AuditLabel: string(intent),
		}, nil
	}
	if !validPracticeActionIntent(intent) {
		return capability.ExposureDecision{}, capability.ErrExecutionRejected
	}
	authorization, err := json.Marshal(struct {
		Intent PracticeTurnIntent `json:"intent"`
	}{Intent: intent})
	if err != nil {
		return capability.ExposureDecision{}, capability.ErrExecutionRejected
	}
	return capability.ExposureDecision{
		Expose: true, Require: true, Authorization: authorization,
		AuditLabel:  string(intent),
		Instruction: practiceTurnIntentInstruction(intent),
		InputSchema: value.practiceTurnInputSchema(intent),
	}, nil
}

func practiceTurnIntentInstruction(intent PracticeTurnIntent) string {
	switch intent {
	case PracticeTurnIntentRequestCreate:
		return "The server authorized REQUEST_CREATE. Resolve the scene, but separate creation authorization from scene specificity: if the user has not provided enough detail to choose one scene, use NEEDS_CLARIFICATION; do not silently choose an experience default. Use CUSTOM only for a concrete unmatched scenario in a supported generic experience."
	case PracticeTurnIntentProposeCreate:
		return "The server authorized PROPOSE_CREATE. Identify exactly one proposed Catalog or Custom scene, so the server can ask once before creating. Do not use NONE or NEEDS_CLARIFICATION."
	case PracticeTurnIntentConfirmPending:
		return "The server authorized CONFIRM_PENDING. Use resolution_kind NONE, an empty catalog_scene_ids array, an empty custom_scenario, and custom_experience_hint NONE."
	case PracticeTurnIntentRejectPending:
		return "The server authorized REJECT_PENDING. Use resolution_kind NONE, an empty catalog_scene_ids array, an empty custom_scenario, and custom_experience_hint NONE."
	default:
		return ""
	}
}

func (value PreviewTool) practiceTurnInputSchema(
	intent PracticeTurnIntent,
) map[string]any {
	if intent == PracticeTurnIntentRequestCreate {
		return value.Definition().InputSchema
	}
	if intent == PracticeTurnIntentProposeCreate {
		schema := value.Definition().InputSchema
		properties := schema["properties"].(map[string]any)
		properties["resolution_kind"] = capability.StringEnumSchema(
			"Propose exactly one Catalog or Custom scene; the server will ask for confirmation before writing.",
			string(SceneResolutionKindCatalog),
			string(SceneResolutionKindCustom),
		)
		return schema
	}
	// A pending reply is resolved exclusively from trusted authorization and
	// the persisted pending action. The model contributes no business fields.
	return capability.ObjectSchema(map[string]any{}, nil)
}
func (value PreviewTool) Definition() capability.Definition {
	catalogIDs := make([]string, len(value.manifest.Scenes))
	for index, item := range value.manifest.Scenes {
		catalogIDs[index] = item.SceneID
	}
	resolutionKindSchema := capability.StringEnumSchema(
		"Use CATALOG for one trusted manifest scene, CUSTOM only when no listed interaction pattern can host the request, or NEEDS_CLARIFICATION for one to three plausible listed scenes.",
		string(SceneResolutionKindCatalog),
		string(SceneResolutionKindCustom),
		string(SceneResolutionKindNeedsClarification),
		string(SceneResolutionKindNone),
	)
	catalogSceneIDsSchema := map[string]any{
		"type":        "array",
		"description": "For CATALOG include exactly one trusted scene id; for NEEDS_CLARIFICATION include one to three; for CUSTOM use an empty array.",
		"items": capability.StringEnumSchema(
			"A trusted Catalog scene id.",
			catalogIDs...,
		),
		"minItems": 0,
		"maxItems": 3,
	}
	return capability.Definition{
		Name: PracticePreviewToolName,
		Description: "Preview the server-authorized practice action for the current user message. The behavior intent was already decided by a blocking server gate and is not model input. Resolve the proposed scene using the frozen trusted Catalog manifest below. " +
			catalogTemplateSelectionPolicy +
			"Use CATALOG with exactly one catalog_scene_ids entry when one listed scene matches; personal details stay in background_summary and never change the Catalog identity. " +
			"Use CUSTOM with an empty catalog_scene_ids array only when no listed interaction pattern can host the concrete request; this generic custom branch currently supports WORKPLACE and LIFE_AND_TRAVEL. " +
			"For INTERVIEW and IELTS_SPEAKING, use their listed or specialized preparation flow instead of inventing a generic custom scene. " +
			"Use NEEDS_CLARIFICATION with one to three catalog_scene_ids entries when the request cannot safely choose one listed scene. " +
			"When the authorized intent confirms or rejects a pending action, use resolution_kind NONE, an empty catalog_scene_ids array, empty custom_scenario, and custom_experience_hint NONE. The server resolves the immediately preceding pending action; never invent its scene. " +
			"For an IELTS Catalog scene, FULL_MOCK means a complete/full/完整模考 request and requires no topic choice. Use PART_1, PART_2, or PART_3 only when the current message explicitly names that Part; never infer a specialty Part from a general IELTS request. A specialty Part requires its topic choice. " +
			"All five discriminator fields are required; non-applicable fields must use [], \"\", or NONE exactly. Structural examples: " +
			`CATALOG={"resolution_kind":"CATALOG","catalog_scene_ids":["<one manifest scene id>"],"custom_scenario":"","custom_experience_hint":"NONE"}; ` +
			`NEEDS_CLARIFICATION={"resolution_kind":"NEEDS_CLARIFICATION","catalog_scene_ids":["<one to three manifest scene ids>"],"custom_scenario":"","custom_experience_hint":"NONE"}; omit background_summary, IELTS fields, and role fields for this branch. ` +
			`PENDING_REPLY={"resolution_kind":"NONE","catalog_scene_ids":[],"custom_scenario":"","custom_experience_hint":"NONE"}. ` +
			"A successful tool result completes this turn; never call the tool repeatedly in the same turn.\n\nTrusted Catalog manifest:\n" +
			formatPreviewCatalogManifest(value.manifest),
		InputSchema: capability.ObjectSchema(map[string]any{
			"resolution_kind":   resolutionKindSchema,
			"catalog_scene_ids": catalogSceneIDsSchema,
			"custom_scenario": map[string]any{
				"type":        "string",
				"description": "For CUSTOM, the concrete directory-external situation. For other branches, use an empty string.",
				"maxLength":   200,
			},
			"custom_experience_hint": capability.StringEnumSchema(
				"For CUSTOM, the product experience. For other branches, use NONE.",
				noCustomExperienceHint,
				"WORKPLACE", "LIFE_AND_TRAVEL", "INTERVIEW", "IELTS_SPEAKING",
			),
			"user_role": capability.OptionalTextSchema(
				"Optional user-authored learner role for CUSTOM only. Omit it or use an empty string when it does not apply.",
				200,
			),
			"ai_role": capability.OptionalTextSchema(
				"Optional user-authored counterpart role for CUSTOM only. Omit it or use an empty string when it does not apply.",
				200,
			),
			"practice_goal": capability.OptionalTextSchema(
				"Optional user-authored practice goal for CUSTOM only. Omit it or use an empty string when it does not apply.",
				500,
			),
			"background_summary": capability.OptionalTextSchema(
				"Optional concise user-authored preparation facts. Omit it or use an empty string when there are no facts to preserve.",
				6000,
			),
			"ielts_practice_mode": capability.StringEnumSchema(
				"IELTS Speaking mode explicitly requested. Use FULL_MOCK for complete/full/完整模考 or a general IELTS full-practice request. Use a PART mode only when that Part is explicitly named.",
				"FULL_MOCK", "PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"Topic choice only for an explicitly requested IELTS Part 1, Part 2, or Part 3; omit for FULL_MOCK.",
				"random", "person", "place", "thing", "experience",
			),
		}, []string{
			"resolution_kind", "catalog_scene_ids",
			"custom_scenario", "custom_experience_hint",
		}),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (value PreviewTool) ClassifyInvocationEffect(
	input json.RawMessage,
) (capability.InvocationEffect, error) {
	parsed, err := parseUnifiedPreviewToolInput(input)
	if err != nil {
		return 0, err
	}
	if !value.validCatalogIDs(parsed.CatalogSceneIDs) {
		return 0, capability.ErrInvalidInput
	}
	if parsed.ResolutionKind == SceneResolutionKindNeedsClarification {
		return capability.InvocationEffectReadOnly, nil
	}
	return capability.InvocationEffectMayWrite, nil
}

func (value PreviewTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if value.port == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	intent, err := decodePracticeTurnAuthorization(call.Authorization)
	if err != nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	if intent == PracticeTurnIntentConfirmPending ||
		intent == PracticeTurnIntentRejectPending {
		previewInput := PreviewInput{
			ActionIntent: intent,
			SceneResolution: SceneResolutionInput{
				Kind: SceneResolutionKindNone,
			},
		}
		result, err := value.port.PreviewPractice(ctx, call, previewInput)
		if err != nil {
			return capability.Result{}, err
		}
		return previewToolResult(result)
	}
	parsed, err := parseUnifiedPreviewToolInput(input)
	if err != nil {
		return capability.Result{}, err
	}
	if !value.validCatalogIDs(parsed.CatalogSceneIDs) {
		return capability.Result{}, previewInputError{code: "catalog_membership"}
	}
	previewInput := parsed.previewInput(intent)
	if !validPreviewInputShape(previewInput) {
		return capability.Result{}, previewInputError{code: "authorized_intent_shape"}
	}
	result, err := value.port.PreviewPractice(ctx, call, previewInput)
	if err != nil {
		return capability.Result{}, err
	}
	return previewToolResult(result)
}

func parseUnifiedPreviewToolInput(
	input json.RawMessage,
) (UnifiedPreviewToolInput, error) {
	var parsed UnifiedPreviewToolInput
	if err := decodePreviewToolInput(input, &parsed); err != nil {
		return UnifiedPreviewToolInput{}, previewInputError{code: "decode"}
	}
	if !hasRequiredUnifiedPreviewFields(input) {
		return UnifiedPreviewToolInput{}, previewInputError{code: "required_fields"}
	}
	parsed = canonicalPreviewToolInput(parsed)
	if code := invalidUnifiedPreviewToolInput(parsed); code != "" {
		return UnifiedPreviewToolInput{}, previewInputError{code: code}
	}
	return parsed, nil
}

// canonicalPreviewToolInput discards contextual fields on the read-only
// clarification branch. Those fields cannot influence candidate selection or
// create a plan, and tool-calling models commonly repeat conversation context
// there even though the branch contract asks them to omit it.
func canonicalPreviewToolInput(
	input UnifiedPreviewToolInput,
) UnifiedPreviewToolInput {
	if input.ResolutionKind != SceneResolutionKindNeedsClarification {
		return input
	}
	input.UserRole = ""
	input.AIRole = ""
	input.PracticeGoal = ""
	input.BackgroundSummary = ""
	input.IELTSPracticeMode = ""
	input.IELTSTopicChoice = ""
	return input
}

func hasRequiredUnifiedPreviewFields(input json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return false
	}
	for _, name := range []string{
		"resolution_kind",
		"catalog_scene_ids",
		"custom_scenario",
		"custom_experience_hint",
	} {
		raw, found := fields[name]
		if !found || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	return true
}

func decodePreviewToolInput(input json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return capability.ErrInvalidInput
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return capability.ErrInvalidInput
	}
	return nil
}

func invalidUnifiedPreviewToolInput(input UnifiedPreviewToolInput) string {
	if !validOptionalInputText(input.CustomScenario, 200) ||
		!validOptionalInputText(input.UserRole, 200) ||
		!validOptionalInputText(input.AIRole, 200) ||
		!validOptionalInputText(input.PracticeGoal, 500) ||
		!validOptionalInputText(input.BackgroundSummary, 6000) ||
		!validOptionalIELTSPracticeMode(input.IELTSPracticeMode) ||
		!validOptionalIELTSTopicChoice(input.IELTSTopicChoice) ||
		!validToolCatalogSceneIDs(input.CatalogSceneIDs) {
		return "field_constraints"
	}
	customFieldsEmpty := input.CustomScenario == "" &&
		input.CustomExperienceHint == noCustomExperienceHint &&
		input.UserRole == "" && input.AIRole == "" && input.PracticeGoal == ""
	if input.ResolutionKind == SceneResolutionKindNone {
		if input.ResolutionKind == SceneResolutionKindNone &&
			len(input.CatalogSceneIDs) == 0 && customFieldsEmpty &&
			input.BackgroundSummary == "" && input.IELTSPracticeMode == "" &&
			input.IELTSTopicChoice == "" {
			return ""
		}
		return "none_branch"
	}
	switch input.ResolutionKind {
	case SceneResolutionKindCatalog:
		if len(input.CatalogSceneIDs) == 1 && customFieldsEmpty {
			return ""
		}
		return "catalog_branch"
	case SceneResolutionKindCustom:
		if len(input.CatalogSceneIDs) == 0 &&
			validInputText(input.CustomScenario, 200) &&
			validCustomExperienceHint(input.CustomExperienceHint) &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == "" {
			return ""
		}
		return "custom_branch"
	case SceneResolutionKindNeedsClarification:
		if len(input.CatalogSceneIDs) >= 1 && customFieldsEmpty &&
			input.BackgroundSummary == "" && input.IELTSPracticeMode == "" &&
			input.IELTSTopicChoice == "" {
			return ""
		}
		return "clarification_branch"
	default:
		return "resolution_kind"
	}
}

func validCustomExperienceHint(value string) bool {
	switch value {
	case "WORKPLACE", "LIFE_AND_TRAVEL", "INTERVIEW", "IELTS_SPEAKING":
		return true
	default:
		return false
	}
}

func validToolCatalogSceneIDs(ids []string) bool {
	if len(ids) > 3 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !validSceneID(id) {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func (input UnifiedPreviewToolInput) previewInput(
	intents ...PracticeTurnIntent,
) PreviewInput {
	intent := PracticeTurnIntentRequestCreate
	if len(intents) == 1 {
		intent = intents[0]
	}
	resolution := SceneResolutionInput{Kind: input.ResolutionKind}
	if input.ResolutionKind == SceneResolutionKindCatalog {
		resolution.CatalogSceneID = input.CatalogSceneIDs[0]
	} else if input.ResolutionKind == SceneResolutionKindNeedsClarification {
		resolution.CandidateSceneIDs = append(
			[]string(nil),
			input.CatalogSceneIDs...,
		)
	}
	result := PreviewInput{
		ActionIntent:      intent,
		SceneResolution:   resolution,
		BackgroundSummary: input.BackgroundSummary,
		IELTSPracticeMode: input.IELTSPracticeMode,
		IELTSTopicChoice:  input.IELTSTopicChoice,
	}
	if input.ResolutionKind == SceneResolutionKindCustom {
		result.SceneIntent = &SceneIntent{
			Scenario:       input.CustomScenario,
			UserRole:       input.UserRole,
			AIRole:         input.AIRole,
			PracticeGoal:   input.PracticeGoal,
			ExperienceHint: input.CustomExperienceHint,
		}
	}
	return result
}

func validOptionalIELTSPracticeMode(value string) bool {
	switch value {
	case "", "FULL_MOCK", "PART_1", "PART_2", "PART_3":
		return true
	default:
		return false
	}
}

func validOptionalIELTSTopicChoice(value string) bool {
	switch value {
	case "", "random", "person", "place", "thing", "experience":
		return true
	default:
		return false
	}
}

func validPreviewInputShape(input PreviewInput) bool {
	if !validOptionalInputText(input.BackgroundSummary, 6000) ||
		!validSceneIntentText(input.SceneIntent) {
		return false
	}
	if input.ActionIntent == PracticeTurnIntentConfirmPending ||
		input.ActionIntent == PracticeTurnIntentRejectPending {
		return input.SceneResolution.Kind == SceneResolutionKindNone &&
			input.SceneResolution.CatalogSceneID == "" &&
			len(input.SceneResolution.CandidateSceneIDs) == 0 &&
			input.SceneIntent == nil && input.BackgroundSummary == "" &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == ""
	}
	if input.ActionIntent != PracticeTurnIntentRequestCreate &&
		input.ActionIntent != PracticeTurnIntentProposeCreate {
		return false
	}
	switch input.SceneResolution.Kind {
	case SceneResolutionKindCatalog:
		return validSceneID(input.SceneResolution.CatalogSceneID) &&
			len(input.SceneResolution.CandidateSceneIDs) == 0 &&
			input.SceneIntent == nil
	case SceneResolutionKindCustom:
		return input.SceneResolution.CatalogSceneID == "" &&
			len(input.SceneResolution.CandidateSceneIDs) == 0 &&
			input.SceneIntent != nil &&
			validInputText(input.SceneIntent.Scenario, 200) &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == ""
	case SceneResolutionKindNeedsClarification:
		return input.ActionIntent == PracticeTurnIntentRequestCreate &&
			input.SceneResolution.CatalogSceneID == "" &&
			validCandidateSceneIDs(input.SceneResolution.CandidateSceneIDs) &&
			input.SceneIntent == nil && input.BackgroundSummary == "" &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == ""
	default:
		return false
	}
}

func decodePracticeTurnAuthorization(
	raw json.RawMessage,
) (PracticeTurnIntent, error) {
	var authorization struct {
		Intent PracticeTurnIntent `json:"intent"`
	}
	if err := decodePreviewToolInput(raw, &authorization); err != nil ||
		!validPracticeActionIntent(authorization.Intent) {
		return "", capability.ErrExecutionRejected
	}
	return authorization.Intent, nil
}

func validPracticeActionIntent(intent PracticeTurnIntent) bool {
	switch intent {
	case PracticeTurnIntentRequestCreate, PracticeTurnIntentProposeCreate,
		PracticeTurnIntentConfirmPending, PracticeTurnIntentRejectPending:
		return true
	default:
		return false
	}
}

func validSceneIntentText(intent *SceneIntent) bool {
	if intent == nil {
		return true
	}
	if !validOptionalInputText(intent.Scenario, 200) ||
		!validOptionalInputText(intent.UserRole, 200) ||
		!validOptionalInputText(intent.AIRole, 200) ||
		!validOptionalInputText(intent.PracticeGoal, 500) {
		return false
	}
	switch intent.ExperienceHint {
	case "", "WORKPLACE", "LIFE_AND_TRAVEL", "INTERVIEW", "IELTS_SPEAKING":
		return true
	default:
		return false
	}
}

func validCandidateSceneIDs(ids []string) bool {
	if len(ids) < 1 || len(ids) > 3 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !validSceneID(id) {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func validSceneID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128 &&
		!strings.ContainsRune(value, '\x00')
}

func validInputText(value string, maximumRunes int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= maximumRunes &&
		!strings.ContainsRune(value, '\x00')
}

func validOptionalInputText(value string, maximumRunes int) bool {
	return value == "" || validInputText(value, maximumRunes)
}

func (value PreviewTool) validCatalogIDs(ids []string) bool {
	for _, id := range ids {
		if _, found := value.catalogIDs[id]; !found {
			return false
		}
	}
	return true
}

func validPreviewCatalogManifest(manifest PreviewCatalogManifest) bool {
	if len(manifest.Experiences) == 0 || len(manifest.Scenes) == 0 {
		return false
	}
	sceneIDs := make(map[string]struct{}, len(manifest.Scenes))
	previousID := ""
	for _, item := range manifest.Scenes {
		if !validSceneID(item.SceneID) || item.SceneID <= previousID ||
			!validInputText(item.Name, 200) ||
			!validInputText(item.PracticeExperience, 100) ||
			!validInputText(item.PublicSceneBrief, 500) ||
			!validInputText(item.PracticeGoal, 500) {
			return false
		}
		previousID = item.SceneID
		for _, alias := range item.Aliases {
			if !validInputText(alias, 200) {
				return false
			}
		}
		sceneIDs[item.SceneID] = struct{}{}
	}
	previousExperience := ""
	for _, item := range manifest.Experiences {
		if !validInputText(item.PracticeExperience, 100) ||
			item.PracticeExperience <= previousExperience ||
			!validSceneID(item.DefaultSceneID) ||
			!validSceneID(item.DefaultPracticeOptionID) {
			return false
		}
		if _, found := sceneIDs[item.DefaultSceneID]; !found {
			return false
		}
		for _, alias := range item.Aliases {
			if !validInputText(alias, 200) {
				return false
			}
		}
		previousExperience = item.PracticeExperience
	}
	return true
}

func clonePreviewCatalogManifest(source PreviewCatalogManifest) PreviewCatalogManifest {
	cloned := PreviewCatalogManifest{
		Experiences: make(
			[]PreviewCatalogManifestExperience,
			len(source.Experiences),
		),
		Scenes: make([]PreviewCatalogManifestScene, len(source.Scenes)),
	}
	copy(cloned.Experiences, source.Experiences)
	for index := range cloned.Experiences {
		cloned.Experiences[index].Aliases = append(
			[]string(nil),
			source.Experiences[index].Aliases...,
		)
	}
	copy(cloned.Scenes, source.Scenes)
	for index := range cloned.Scenes {
		cloned.Scenes[index].Aliases = append([]string(nil), source.Scenes[index].Aliases...)
	}
	return cloned
}

func formatPreviewCatalogManifest(manifest PreviewCatalogManifest) string {
	lines := make([]string, 0, len(manifest.Experiences)+len(manifest.Scenes)+2)
	lines = append(lines, "Broad experience categories (not selectable scenes):")
	for _, item := range manifest.Experiences {
		aliases := append([]string(nil), item.Aliases...)
		sort.Strings(aliases)
		lines = append(lines, fmt.Sprintf(
			"- %s | aliases: %s | choose a concrete scene below; do not auto-select from this category alone",
			item.PracticeExperience,
			strings.Join(aliases, ", "),
		))
	}
	lines = append(lines, "Scenes:")
	for _, item := range manifest.Scenes {
		aliases := append([]string(nil), item.Aliases...)
		sort.Strings(aliases)
		lines = append(lines, fmt.Sprintf(
			"- %s | %s | %s | aliases: %s | situation: %s | goal: %s",
			item.SceneID,
			item.PracticeExperience,
			item.Name,
			strings.Join(aliases, ", "),
			item.PublicSceneBrief,
			item.PracticeGoal,
		))
	}
	return strings.Join(lines, "\n")
}

func previewToolResult(preview PreviewResult) (capability.Result, error) {
	if !validPreviewResolution(preview.Status, preview.SceneResolution, preview.ResolutionReason) ||
		preview.CatalogCandidateCount < 0 || !validPreviewPlanMetadata(preview) {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	content := map[string]any{
		"status":                  preview.Status,
		"scene_resolution":        preview.SceneResolution,
		"catalog_candidate_count": preview.CatalogCandidateCount,
	}
	if preview.ResolutionReason != "" {
		content["resolution_reason"] = preview.ResolutionReason
	}
	clientActions := []agentclientaction.Action(nil)
	switch preview.Status {
	case PreviewOutcomeReady:
		content["confirmation_required"] = true
		content["replayed"] = preview.Replayed
		content["plan_id"] = preview.PlanID
		content["plan_source"] = preview.PlanSource
		clientActions = []agentclientaction.Action{preview.ClientAction}
	case PreviewOutcomeNeedsDetails,
		PreviewOutcomeAmbiguous,
		PreviewOutcomeRequiresSpecializedFlow:
		content["required_missing_fields"] = preview.RequiredMissingFields
		content["catalog_candidates"] = preview.Candidates
	case PreviewOutcomeActionPending, PreviewOutcomeActionDeclined:
		content["replayed"] = preview.Replayed
	default:
		return capability.Result{}, capability.ErrExecutionRejected
	}
	return capability.Result{
		Content:       content,
		SourceRefs:    preview.SourceRefs,
		ClientActions: clientActions,
		TurnOutcome:   capability.TurnOutcomeCompleted,
		AssistantText: preview.AssistantText,
	}, nil
}

func validPreviewPlanMetadata(preview PreviewResult) bool {
	if preview.Status == PreviewOutcomeReady {
		payload, validAction := decodeConfirmPracticePlanClientAction(preview.ClientAction)
		return practicePlanUUIDPattern.MatchString(preview.PlanID) &&
			validPreviewPlanSource(preview.SceneResolution, preview.PlanSource) &&
			validAction && payload.PracticePlanID == preview.PlanID
	}
	return preview.PlanID == "" && preview.PlanSource == "" &&
		preview.ClientAction.Type == "" && len(preview.ClientAction.Payload) == 0
}

func validPreviewPlanSource(resolution ResolvedSceneStatus, source PreviewPlanSource) bool {
	switch resolution {
	case SceneResolutionCatalogResolved:
		return source == PreviewPlanSourceCatalog
	case SceneResolutionCustomResolved:
		return source == PreviewPlanSourceCustom
	default:
		return false
	}
}

func validPreviewResolution(
	outcome PreviewOutcome,
	resolution ResolvedSceneStatus,
	reason ResolutionReason,
) bool {
	switch outcome {
	case PreviewOutcomeReady:
		return reason == "" &&
			(resolution == SceneResolutionCatalogResolved || resolution == SceneResolutionCustomResolved)
	case PreviewOutcomeNeedsDetails:
		return reason == "" && resolution == SceneResolutionNeedsDetails
	case PreviewOutcomeAmbiguous:
		return reason == "" && resolution == SceneResolutionAmbiguous
	case PreviewOutcomeRequiresSpecializedFlow:
		return resolution == SceneResolutionRejected &&
			reason == ResolutionReasonSpecializedFlowRequired
	case PreviewOutcomeActionPending, PreviewOutcomeActionDeclined:
		return reason == "" && resolution == SceneResolutionNotRequested
	default:
		return false
	}
}
