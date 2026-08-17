package agentcapability

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ServicePort struct {
	plans   PlanApplication
	catalog scene.PreviewCatalogResolver
}

type PlanApplication interface {
	PreviewPlan(
		context.Context,
		requestcontext.Actor,
		string,
		preparation.CreatePlanRequest,
	) (preparation.PracticePlan, bool, error)
}

func NewServicePort(
	plans PlanApplication,
	catalog scene.PreviewCatalogResolver,
) (*ServicePort, error) {
	if plans == nil || catalog == nil {
		return nil, errors.New(
			"preparation agent capability: plans and catalog are required",
		)
	}
	return &ServicePort{plans: plans, catalog: catalog}, nil
}

func (port *ServicePort) PreviewPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	if port == nil || port.plans == nil || port.catalog == nil ||
		ctx == nil || !call.Actor.Valid() ||
		call.ThreadID == "" || call.RequestID == "" {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	input.BackgroundSummary = strings.TrimSpace(input.BackgroundSummary)
	if input.IELTSPracticeMode != "" {
		input.BackgroundSummary = previewIELTSBackgroundSummary(input)
	}

	candidateQuery := input.SceneQuery
	if input.IELTSPracticeMode != "" && input.SceneID == "" {
		candidateQuery = "IELTS"
	}
	if strings.TrimSpace(candidateQuery) == "" && input.SceneID != "" {
		candidateQuery = input.SceneID
	}
	candidates, err := port.resolveCandidates(ctx, candidateQuery)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	input = enrichPreviewInput(input, candidates)
	validationCandidates := candidates
	if input.SceneID != "" &&
		!containsPreviewScene(validationCandidates, input.SceneID) {
		exact, exactErr := port.resolveCandidates(ctx, input.SceneID)
		if exactErr != nil {
			return PreviewResult{}, mapPreparationToolError(exactErr)
		}
		validationCandidates = append(validationCandidates, exact...)
	}
	missing := previewMissingFields(input, validationCandidates)
	if len(missing) > 0 {
		return PreviewResult{
			Status:                "needs_input",
			RequiredMissingFields: missing,
			Candidates:            candidates,
		}, nil
	}

	plan, replayed, err := port.plans.PreviewPlan(
		ctx,
		call.Actor,
		call.RequestID,
		preparation.CreatePlanRequest{
			SourceThreadID:    call.ThreadID,
			BackgroundSummary: input.BackgroundSummary,
			SceneID:           input.SceneID,
			SceneVersion:      input.SceneVersion,
			SelectedRoleIDs:   append([]string(nil), input.SelectedRoleIDs...),
			PracticeOptionID:  input.PracticeOptionID,
			MaxEffectiveTurns: input.MaxEffectiveTurns,
			IELTSSelection:    previewIELTSQuestionSelection(input),
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	clientAction, err := practicePlanClientAction(plan)
	if err != nil {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	return PreviewResult{
		Status:       "preview_ready",
		Replayed:     replayed,
		ClientAction: clientAction,
		AssistantText: "已为您准备好“" + plan.SceneSelection.Scene.Name +
			"”练习，请确认开始。",
		SourceRefs: []capability.SourceRef{
			{Type: "practice_plan", ID: plan.ID},
		},
	}, nil
}

type confirmPracticePlanActionPayload struct {
	Label                    string   `json:"label"`
	PracticePlanID           string   `json:"practice_plan_id"`
	PlanVersion              int      `json:"plan_version"`
	Target                   string   `json:"target"`
	SceneName                string   `json:"scene_name"`
	PracticeExperience       string   `json:"practice_experience"`
	SceneCategory            string   `json:"scene_category"`
	PracticeMode             string   `json:"practice_mode"`
	Roles                    []string `json:"roles"`
	PracticeScope            string   `json:"practice_scope"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int      `json:"min_effective_turns"`
	MaxEffectiveTurns        int      `json:"max_effective_turns"`
	ConfirmationPrompt       string   `json:"confirmation_prompt"`
}

var practicePlanUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func practicePlanClientAction(
	plan preparation.PracticePlan,
) (agentclientaction.Action, error) {
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	roleNames := make([]string, len(roles))
	for index, role := range roles {
		roleNames[index] = role.DisplayName
	}
	target := strings.TrimSpace(plan.SceneSelection.Scene.Prompt.PracticeGoal)
	payload := confirmPracticePlanActionPayload{
		Label:                    "确认并开始练习",
		PracticePlanID:           plan.ID,
		PlanVersion:              plan.Version,
		Target:                   target,
		SceneName:                plan.SceneSelection.Scene.Name,
		PracticeExperience:       string(plan.SceneSelection.Scene.Experience),
		SceneCategory:            string(plan.SceneSelection.Scene.Category),
		PracticeMode:             string(option.Mode),
		Roles:                    roleNames,
		PracticeScope:            option.DisplayName,
		SuggestedDurationSeconds: plan.SessionPolicy.SuggestedDurationSeconds,
		MinEffectiveTurns:        plan.SessionPolicy.MinEffectiveTurns,
		MaxEffectiveTurns:        plan.SessionPolicy.MaxEffectiveTurns,
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	}
	if !validConfirmPracticePlanActionPayload(payload) {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	action, err := agentclientaction.New(ConfirmPracticePlanActionType, raw)
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	return action, nil
}

func validConfirmPracticePlanActionPayload(
	payload confirmPracticePlanActionPayload,
) bool {
	if !validActionText(payload.Label, 100) ||
		!practicePlanUUIDPattern.MatchString(payload.PracticePlanID) ||
		payload.PlanVersion < 1 ||
		!validActionText(payload.Target, 500) ||
		!validActionText(payload.SceneName, 200) ||
		!validActionText(payload.PracticeExperience, 100) ||
		!validActionText(payload.SceneCategory, 200) ||
		!validActionText(payload.PracticeMode, 100) ||
		!validActionText(payload.PracticeScope, 200) ||
		payload.SuggestedDurationSeconds < 1 ||
		payload.MinEffectiveTurns < 1 ||
		(payload.MaxEffectiveTurns != 0 &&
			payload.MaxEffectiveTurns < payload.MinEffectiveTurns) ||
		payload.MaxEffectiveTurns > 100 ||
		!validActionText(payload.ConfirmationPrompt, 300) ||
		len(payload.Roles) < 1 || len(payload.Roles) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(payload.Roles))
	for _, role := range payload.Roles {
		if !validActionText(role, 200) {
			return false
		}
		if _, duplicate := seen[role]; duplicate {
			return false
		}
		seen[role] = struct{}{}
	}
	return true
}

func validActionText(value string, maxRunes int) bool {
	return value == strings.TrimSpace(value) && value != "" &&
		utf8.RuneCountInString(value) <= maxRunes
}

func (port *ServicePort) resolveCandidates(
	ctx context.Context,
	query string,
) ([]CatalogCandidate, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	items, err := port.catalog.ResolvePreviewCatalog(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]CatalogCandidate, len(items))
	for index, item := range items {
		options := make([]CatalogPracticeOption, len(item.Scene.PracticeOptions))
		for optionIndex, option := range item.Scene.PracticeOptions {
			options[optionIndex] = CatalogPracticeOption{
				ID:          option.ID,
				DisplayName: option.DisplayName,
				Mode:        string(option.Mode),
			}
		}
		result[index] = CatalogCandidate{
			SceneID:            item.Scene.ID,
			SceneVersion:       item.Scene.Version,
			Name:               item.Scene.Name,
			PracticeExperience: string(item.Scene.Experience),
			SceneCategory:      string(item.Scene.Category),
			DefaultRoleIDs: append(
				[]string(nil),
				item.DefaultRoleIDs...,
			),
			DefaultPracticeOptionID: item.DefaultOption.ID,
			PracticeOptions:         options,
		}
	}
	return result, nil
}

func enrichPreviewInput(
	input PreviewInput,
	candidates []CatalogCandidate,
) PreviewInput {
	if len(candidates) != 1 {
		return input
	}
	candidate := candidates[0]
	if input.SceneID == "" {
		input.SceneID = candidate.SceneID
	}
	if input.SceneID == candidate.SceneID && input.SceneVersion < 1 {
		input.SceneVersion = candidate.SceneVersion
	}
	if len(input.SelectedRoleIDs) == 0 {
		input.SelectedRoleIDs = append([]string(nil), candidate.DefaultRoleIDs...)
	}
	if candidate.PracticeExperience ==
		string(scene.PracticeExperienceIELTSSpeaking) {
		if optionID, found := previewOptionForMode(
			candidate,
			scene.PracticeMode(input.IELTSPracticeMode),
		); found {
			input.PracticeOptionID = optionID
		}
		input.MaxEffectiveTurns = 0
	} else {
		if input.PracticeOptionID == "" {
			input.PracticeOptionID = preferredPreviewOption(input, candidate)
		}
		if input.MaxEffectiveTurns < 1 {
			input.MaxEffectiveTurns = 5
		}
	}
	return input
}

func preferredPreviewOption(
	input PreviewInput,
	candidate CatalogCandidate,
) string {
	summary := strings.ToLower(input.BackgroundSummary)
	if strings.Contains(summary, "重点练习") ||
		strings.Contains(summary, "focus") {
		if optionID, found := previewOptionForMode(
			candidate,
			scene.PracticeModeFocus,
		); found {
			return optionID
		}
	}
	return candidate.DefaultPracticeOptionID
}

func previewMissingFields(
	input PreviewInput,
	candidates []CatalogCandidate,
) []string {
	missing := make([]string, 0, 6)
	if input.BackgroundSummary == "" {
		missing = append(missing, "background_summary")
	}
	if input.SceneID == "" || input.SceneVersion < 1 ||
		!containsPreviewSceneVersion(
			candidates,
			input.SceneID,
			input.SceneVersion,
		) {
		missing = append(missing, "scene_selection")
	}
	if len(input.SelectedRoleIDs) == 0 {
		missing = append(missing, "role_selection")
	}
	if input.PracticeOptionID == "" {
		missing = append(missing, "practice_option")
	}
	mode, isIELTS := previewIELTSMode(
		input.SceneID,
		input.PracticeOptionID,
		candidates,
	)
	if isIELTS {
		requestedMode := scene.PracticeMode(input.IELTSPracticeMode)
		if !validPreviewIELTSMode(requestedMode) || requestedMode != mode {
			missing = append(missing, "ielts_practice_mode")
		}
		if !validPreviewIELTSTopicChoice(requestedMode, input.IELTSTopicChoice) {
			missing = append(missing, "ielts_topic_choice")
		}
	} else {
		if input.MaxEffectiveTurns < 1 {
			missing = append(missing, "max_effective_turns")
		}
		if input.IELTSPracticeMode != "" || input.IELTSTopicChoice != "" {
			missing = append(missing, "ielts_practice_mode")
		}
	}
	return missing
}

func previewOptionForMode(
	candidate CatalogCandidate,
	mode scene.PracticeMode,
) (string, bool) {
	for _, option := range candidate.PracticeOptions {
		if scene.PracticeMode(option.Mode) == mode {
			return option.ID, true
		}
	}
	return "", false
}

func containsPreviewScene(candidates []CatalogCandidate, sceneID string) bool {
	for _, candidate := range candidates {
		if candidate.SceneID == sceneID {
			return true
		}
	}
	return false
}

func containsPreviewSceneVersion(
	candidates []CatalogCandidate,
	sceneID string,
	sceneVersion int,
) bool {
	for _, candidate := range candidates {
		if candidate.SceneID == sceneID &&
			candidate.SceneVersion == sceneVersion {
			return true
		}
	}
	return false
}

func previewIELTSMode(
	sceneID string,
	practiceOptionID string,
	candidates []CatalogCandidate,
) (scene.PracticeMode, bool) {
	for _, candidate := range candidates {
		if candidate.SceneID != sceneID ||
			candidate.PracticeExperience !=
				string(scene.PracticeExperienceIELTSSpeaking) {
			continue
		}
		for _, option := range candidate.PracticeOptions {
			if option.ID == practiceOptionID {
				return scene.PracticeMode(option.Mode), true
			}
		}
		return "", true
	}
	return "", false
}

func validPreviewIELTSMode(mode scene.PracticeMode) bool {
	switch mode {
	case scene.PracticeModeFullMock,
		scene.PracticeModePart1,
		scene.PracticeModePart2,
		scene.PracticeModePart3:
		return true
	default:
		return false
	}
}

func validPreviewIELTSTopicChoice(
	mode scene.PracticeMode,
	choice string,
) bool {
	if mode == scene.PracticeModeFullMock {
		return choice == ""
	}
	if mode != scene.PracticeModePart1 &&
		mode != scene.PracticeModePart2 &&
		mode != scene.PracticeModePart3 {
		return false
	}
	switch choice {
	case "random", "person", "place", "thing", "experience":
		return true
	default:
		return false
	}
}

func previewIELTSQuestionSelection(
	input PreviewInput,
) *preparation.IELTSQuestionSelection {
	if input.IELTSTopicChoice == "" || input.IELTSTopicChoice == "random" {
		return nil
	}
	return &preparation.IELTSQuestionSelection{
		CueCardType: input.IELTSTopicChoice,
	}
}

func previewIELTSBackgroundSummary(input PreviewInput) string {
	mode := scene.PracticeMode(input.IELTSPracticeMode)
	if !validPreviewIELTSMode(mode) {
		return ""
	}
	if mode == scene.PracticeModeFullMock {
		return "User requested an IELTS Speaking full mock."
	}
	if input.IELTSTopicChoice == "" {
		return "User requested IELTS Speaking " + string(mode) + "."
	}
	if !validPreviewIELTSTopicChoice(mode, input.IELTSTopicChoice) {
		return ""
	}
	choice := input.IELTSTopicChoice
	if choice == "random" {
		choice = "a random published topic"
	} else {
		choice += " topic"
	}
	return "User requested IELTS Speaking " + string(mode) + " with " + choice + "."
}

func mapPreparationToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, scene.ErrCatalogSelectionInvalid),
		errors.Is(err, preparation.ErrPlanInvalid):
		return capability.ErrInvalidInput
	case errors.Is(err, preparation.ErrPlanNotFound),
		errors.Is(err, preparation.ErrPlanConflict),
		errors.Is(err, preparation.ErrPlanIdempotencyConflict):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
