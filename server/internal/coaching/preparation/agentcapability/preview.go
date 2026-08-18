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

const PracticePreviewToolName = "practice.preview.v2"

const ConfirmPracticePlanActionType = "practice.plan.confirm.v2"

const noCustomExperienceHint = "NONE"

const catalogTemplateSelectionPolicy = "Treat every Catalog scene as a parameterizable interaction template, not a literal script. " +
	"Choose a Catalog scene whenever its participant roles and core communication task can host the request. " +
	"A company, product, date, room type, incident, constraint, counterpart attitude, or the words custom/定制 are background details and never by themselves make a scene directory-external. "

type SceneResolutionKind string

const (
	SceneResolutionKindCatalog            SceneResolutionKind = "CATALOG"
	SceneResolutionKindCustom             SceneResolutionKind = "CUSTOM"
	SceneResolutionKindNeedsClarification SceneResolutionKind = "NEEDS_CLARIFICATION"
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
	SceneQuery           string              `json:"scene_query"`
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
	SceneQuery        string               `json:"scene_query"`
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
)

type ResolvedSceneStatus string

const (
	SceneResolutionCatalogResolved ResolvedSceneStatus = "CATALOG_RESOLVED"
	SceneResolutionCustomResolved  ResolvedSceneStatus = "CUSTOM_RESOLVED"
	SceneResolutionAmbiguous       ResolvedSceneStatus = "AMBIGUOUS"
	SceneResolutionNeedsDetails    ResolvedSceneStatus = "NEEDS_DETAILS"
	SceneResolutionRejected        ResolvedSceneStatus = "REJECTED"
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
func (value PreviewTool) Definition() capability.Definition {
	catalogIDs := make([]string, len(value.manifest.Scenes))
	for index, item := range value.manifest.Scenes {
		catalogIDs[index] = item.SceneID
	}
	resolutionKindSchema := capability.StringEnumSchema(
		"Use CATALOG for one trusted manifest scene, CUSTOM only when no listed interaction pattern can host the request, or NEEDS_CLARIFICATION for one to five plausible listed scenes.",
		string(SceneResolutionKindCatalog),
		string(SceneResolutionKindCustom),
		string(SceneResolutionKindNeedsClarification),
	)
	catalogSceneIDsSchema := map[string]any{
		"type":        "array",
		"description": "For CATALOG include exactly one trusted scene id; for NEEDS_CLARIFICATION include one to five; for CUSTOM use an empty array.",
		"items": capability.StringEnumSchema(
			"A trusted Catalog scene id.",
			catalogIDs...,
		),
		"minItems": 0,
		"maxItems": 5,
	}
	return capability.Definition{
		Name: PracticePreviewToolName,
		Description: "Resolve and preview exactly one scene through this single tool, using the frozen trusted Catalog manifest below. " +
			catalogTemplateSelectionPolicy +
			"Use CATALOG with exactly one catalog_scene_ids entry when one listed scene matches; personal details stay in background_summary and never change the Catalog identity. " +
			"Use CUSTOM with an empty catalog_scene_ids array only when no listed interaction pattern can host the request; then provide custom_scenario and custom_experience_hint. " +
			"Use NEEDS_CLARIFICATION with one to five catalog_scene_ids entries when the request cannot safely choose one listed scene. " +
			"Always preserve the user's original wording in scene_query. The server validates the selected branch exactly and never guesses from scene_query. " +
			"For an IELTS Catalog scene, use ielts_practice_mode and the required topic choice. " +
			"All five discriminator fields are required; non-applicable fields must use [], \"\", or NONE exactly. Structural examples: " +
			`CATALOG={"scene_query":"<original request>","resolution_kind":"CATALOG","catalog_scene_ids":["<one manifest scene id>"],"custom_scenario":"","custom_experience_hint":"NONE"}; ` +
			`CUSTOM={"scene_query":"<original request>","resolution_kind":"CUSTOM","catalog_scene_ids":[],"custom_scenario":"<directory-external situation>","custom_experience_hint":"WORKPLACE"}; ` +
			`NEEDS_CLARIFICATION={"scene_query":"<original request>","resolution_kind":"NEEDS_CLARIFICATION","catalog_scene_ids":["<plausible manifest scene id>"],"custom_scenario":"","custom_experience_hint":"NONE"}. ` +
			"A successful tool result completes this turn; never call the tool repeatedly in the same turn.\n\nTrusted Catalog manifest:\n" +
			formatPreviewCatalogManifest(value.manifest),
		InputSchema: capability.ObjectSchema(map[string]any{
			"scene_query": capability.TextSchema(
				"The user's original natural-language scene request, unchanged.",
				500,
			),
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
			"user_role": capability.TextSchema(
				"Optional user-authored learner role for CUSTOM only.",
				200,
			),
			"ai_role": capability.TextSchema(
				"Optional user-authored counterpart role for CUSTOM only.",
				200,
			),
			"practice_goal": capability.TextSchema(
				"Optional user-authored practice goal for CUSTOM only.",
				500,
			),
			"background_summary": capability.TextSchema(
				"Concise user-authored preparation facts, including target, experience, constraints, and focus.",
				6000,
			),
			"ielts_practice_mode": capability.StringEnumSchema(
				"IELTS Speaking mode requested for the IELTS Catalog scene.",
				"FULL_MOCK", "PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"Topic choice for IELTS Part 1, Part 2, or Part 3.",
				"random", "person", "place", "thing", "experience",
			),
		}, []string{
			"scene_query", "resolution_kind", "catalog_scene_ids",
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
	parsed, err := parseUnifiedPreviewToolInput(input)
	if err != nil || !value.validCatalogIDs(parsed.CatalogSceneIDs) {
		return capability.Result{}, capability.ErrInvalidInput
	}
	result, err := value.port.PreviewPractice(ctx, call, parsed.previewInput())
	if err != nil {
		return capability.Result{}, err
	}
	return previewToolResult(result)
}

func parseUnifiedPreviewToolInput(
	input json.RawMessage,
) (UnifiedPreviewToolInput, error) {
	var parsed UnifiedPreviewToolInput
	if err := decodePreviewToolInput(input, &parsed); err != nil ||
		!hasRequiredUnifiedPreviewFields(input) ||
		!validUnifiedPreviewToolInput(parsed) {
		return UnifiedPreviewToolInput{}, capability.ErrInvalidInput
	}
	return parsed, nil
}

func hasRequiredUnifiedPreviewFields(input json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return false
	}
	for _, name := range []string{
		"scene_query",
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

func validUnifiedPreviewToolInput(input UnifiedPreviewToolInput) bool {
	if !validInputText(input.SceneQuery, 500) ||
		!validOptionalInputText(input.CustomScenario, 200) ||
		!validOptionalInputText(input.UserRole, 200) ||
		!validOptionalInputText(input.AIRole, 200) ||
		!validOptionalInputText(input.PracticeGoal, 500) ||
		!validOptionalInputText(input.BackgroundSummary, 6000) ||
		!validOptionalIELTSPracticeMode(input.IELTSPracticeMode) ||
		!validOptionalIELTSTopicChoice(input.IELTSTopicChoice) ||
		!validToolCatalogSceneIDs(input.CatalogSceneIDs) {
		return false
	}
	customFieldsEmpty := input.CustomScenario == "" &&
		input.CustomExperienceHint == noCustomExperienceHint &&
		input.UserRole == "" && input.AIRole == "" && input.PracticeGoal == ""
	switch input.ResolutionKind {
	case SceneResolutionKindCatalog:
		return len(input.CatalogSceneIDs) == 1 && customFieldsEmpty
	case SceneResolutionKindCustom:
		return len(input.CatalogSceneIDs) == 0 &&
			validInputText(input.CustomScenario, 200) &&
			validCustomExperienceHint(input.CustomExperienceHint) &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == ""
	case SceneResolutionKindNeedsClarification:
		return len(input.CatalogSceneIDs) >= 1 && customFieldsEmpty &&
			input.BackgroundSummary == "" && input.IELTSPracticeMode == "" &&
			input.IELTSTopicChoice == ""
	default:
		return false
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
	if len(ids) > 5 {
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

func (input UnifiedPreviewToolInput) previewInput() PreviewInput {
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
		SceneQuery:        input.SceneQuery,
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
	if !validInputText(input.SceneQuery, 500) ||
		!validOptionalInputText(input.BackgroundSummary, 6000) ||
		!validSceneIntentText(input.SceneIntent) {
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
		return input.SceneResolution.CatalogSceneID == "" &&
			validCandidateSceneIDs(input.SceneResolution.CandidateSceneIDs) &&
			input.SceneIntent == nil && input.BackgroundSummary == "" &&
			input.IELTSPracticeMode == "" && input.IELTSTopicChoice == ""
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
	if len(ids) < 1 || len(ids) > 5 {
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
	lines = append(lines, "Broad experience defaults:")
	for _, item := range manifest.Experiences {
		aliases := append([]string(nil), item.Aliases...)
		sort.Strings(aliases)
		lines = append(lines, fmt.Sprintf(
			"- %s | aliases: %s | default_scene_id: %s | default_practice_option_id: %s",
			item.PracticeExperience,
			strings.Join(aliases, ", "),
			item.DefaultSceneID,
			item.DefaultPracticeOptionID,
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
	default:
		return false
	}
}
