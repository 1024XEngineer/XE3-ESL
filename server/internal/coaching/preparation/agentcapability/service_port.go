package agentcapability

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ServicePort struct {
	plans    PlanApplication
	catalog  scene.PreviewCatalogResolver
	profiles ProfileApplication
}

type PlanApplication interface {
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		preparation.CreatePlanRequest,
	) (preparation.PracticePlan, bool, error)
}

type ProfileApplication interface {
	CreateProfile(
		context.Context,
		requestcontext.Actor,
		string,
		preparation.CreateProfileRequest,
	) (preparation.Profile, bool, error)
	CreateSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		preparation.CreateSnapshotRequest,
	) (preparation.Snapshot, bool, error)
}

func NewServicePort(
	plans PlanApplication,
	catalog scene.PreviewCatalogResolver,
	profiles ProfileApplication,
) (*ServicePort, error) {
	if plans == nil || catalog == nil || profiles == nil {
		return nil, errors.New(
			"preparation agent capability: plans, catalog, and profiles are required",
		)
	}
	return &ServicePort{plans: plans, catalog: catalog, profiles: profiles}, nil
}

func (port *ServicePort) PreviewPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	if port == nil || port.plans == nil || port.catalog == nil ||
		port.profiles == nil || ctx == nil || !call.Actor.Valid() ||
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

	profile, _, err := port.profiles.CreateProfile(
		ctx,
		call.Actor,
		call.RequestID,
		preparation.CreateProfileRequest{
			BackgroundSummary: input.BackgroundSummary,
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	snapshot, _, err := port.profiles.CreateSnapshot(
		ctx,
		call.Actor,
		profile.ID,
		call.RequestID,
		preparation.CreateSnapshotRequest{SourceVersion: profile.Version},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}

	plan, replayed, err := port.plans.CreatePlan(
		ctx,
		call.Actor,
		call.RequestID,
		preparation.CreatePlanRequest{
			SourceThreadID:        call.ThreadID,
			GoalID:                input.GoalID,
			PreparationSnapshotID: snapshot.ID,
			SceneID:               input.SceneID,
			SceneVersion:          input.SceneVersion,
			SelectedRoleIDs:       append([]string(nil), input.SelectedRoleIDs...),
			PracticeOptionID:      input.PracticeOptionID,
			MaxEffectiveTurns:     input.MaxEffectiveTurns,
			IELTSSelection:        previewIELTSQuestionSelection(input),
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	handoff, err := practicePlanHandoff(plan)
	if err != nil {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	return PreviewResult{
		Status:   "preview_ready",
		Replayed: replayed,
		Handoff:  handoff,
		SourceRefs: []capability.SourceRef{
			{Type: "practice_plan", ID: plan.ID},
			{Type: "preparation_snapshot", ID: plan.PreparationSnapshot.ID},
		},
	}, nil
}

func practicePlanHandoff(
	plan preparation.PracticePlan,
) (agenthandoff.Item, error) {
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return agenthandoff.Item{}, agenthandoff.ErrInvalid
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		return agenthandoff.Item{}, agenthandoff.ErrInvalid
	}
	roleNames := make([]string, len(roles))
	for index, role := range roles {
		roleNames[index] = role.DisplayName
	}
	target := strings.TrimSpace(plan.SceneSelection.Scene.Prompt.PracticeGoal)
	if plan.GoalSnapshot != nil {
		target = strings.TrimSpace(plan.GoalSnapshot.Title)
	}
	return agenthandoff.NewConfirmPracticePlan(agenthandoff.Item{
		Label:                    "确认并开始练习",
		PracticePlanID:           plan.ID,
		PlanRevision:             plan.Revision,
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
		ExecutableStatus:         string(plan.Status),
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	})
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
		errors.Is(err, preparation.ErrProfileInvalid),
		errors.Is(err, preparation.ErrPlanInvalid):
		return capability.ErrInvalidInput
	case errors.Is(err, preparation.ErrProfileNotFound),
		errors.Is(err, preparation.ErrProfileConflict),
		errors.Is(err, preparation.ErrProfileIdempotencyConflict),
		errors.Is(err, preparation.ErrPlanNotFound),
		errors.Is(err, preparation.ErrPlanConflict),
		errors.Is(err, preparation.ErrPlanIdempotencyConflict):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
