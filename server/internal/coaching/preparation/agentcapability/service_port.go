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

	candidateQuery := input.SceneQuery
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
			IELTSSelection:        cloneIELTSQuestionSelection(input.IELTSSelection),
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
		input.SceneVersion = candidate.SceneVersion
	}
	if len(input.SelectedRoleIDs) == 0 {
		input.SelectedRoleIDs = append([]string(nil), candidate.DefaultRoleIDs...)
	}
	if input.PracticeOptionID == "" {
		input.PracticeOptionID = candidate.DefaultPracticeOptionID
	}
	return input
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
	if input.MaxEffectiveTurns < 1 {
		missing = append(missing, "max_effective_turns")
	}
	if mode, isIELTS := previewIELTSMode(
		input.SceneID,
		input.PracticeOptionID,
		candidates,
	); isIELTS &&
		!validPreviewIELTSSelection(mode, input.IELTSSelection) {
		missing = append(missing, "ielts_selection")
	}
	return missing
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

func validPreviewIELTSSelection(
	mode scene.PracticeMode,
	selection *preparation.IELTSQuestionSelection,
) bool {
	if selection == nil {
		return false
	}
	switch mode {
	case scene.PracticeModeFullMock:
		return selection.Part1SetID != "" && selection.TopicGroupID != ""
	case scene.PracticeModePart1:
		return selection.Part1SetID != "" && selection.TopicGroupID == ""
	case scene.PracticeModePart2, scene.PracticeModePart3:
		return selection.Part1SetID == "" && selection.TopicGroupID != ""
	default:
		return false
	}
}

func cloneIELTSQuestionSelection(
	source *preparation.IELTSQuestionSelection,
) *preparation.IELTSQuestionSelection {
	if source == nil {
		return nil
	}
	result := *source
	return &result
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
