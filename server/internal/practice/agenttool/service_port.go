package agenttool

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

type ServicePort struct {
	practice PreviewApplication
	catalog  preparation.PreviewCatalogResolver
	profiles PreparationProfileApplication
}

type PreviewApplication interface {
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		practice.CreatePlanRequest,
	) (persistence.Plan, bool, error)
}

type PreparationProfileApplication interface {
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
	application PreviewApplication,
	catalog preparation.PreviewCatalogResolver,
	profiles PreparationProfileApplication,
) (*ServicePort, error) {
	if application == nil || catalog == nil || profiles == nil {
		return nil, errors.New(
			"practice agenttool: application, catalog resolver, and profiles are required",
		)
	}
	return &ServicePort{
		practice: application,
		catalog:  catalog,
		profiles: profiles,
	}, nil
}

func (port *ServicePort) PreviewPractice(
	ctx context.Context,
	call tool.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	if port == nil || port.practice == nil || port.catalog == nil ||
		port.profiles == nil ||
		!call.Actor.Valid() || call.ThreadID == "" || call.RequestID == "" {
		return PreviewResult{}, tool.ErrExecutionRejected
	}
	input.BackgroundSummary = strings.TrimSpace(input.BackgroundSummary)

	candidates, err := port.resolveCandidates(input.ScenarioQuery)
	if err != nil {
		return PreviewResult{}, mapPracticeToolError(err)
	}
	input = enrichPreviewInput(input, candidates)
	missing := previewMissingFields(input)
	if len(missing) > 0 {
		return PreviewResult{
			Status:                "needs_input",
			RequiredMissingFields: missing,
			Candidates:            candidates,
		}, nil
	}
	if input.PreparationSnapshotID == "" && input.PreparationProfileID == "" {
		profile, _, createErr := port.profiles.CreateProfile(
			ctx,
			call.Actor,
			call.RequestID,
			preparation.CreateProfileRequest{
				BackgroundSummary: input.BackgroundSummary,
			},
		)
		if createErr != nil {
			return PreviewResult{}, mapPracticeToolError(createErr)
		}
		input.PreparationProfileID = profile.ID
		snapshot, _, createErr := port.profiles.CreateSnapshot(
			ctx,
			call.Actor,
			profile.ID,
			call.RequestID,
			preparation.CreateSnapshotRequest{SourceVersion: profile.Version},
		)
		if createErr != nil {
			return PreviewResult{}, mapPracticeToolError(createErr)
		}
		input.PreparationSnapshotID = snapshot.ID
	}

	plan, replayed, err := port.practice.CreatePlan(
		ctx,
		call.Actor,
		call.RequestID,
		practice.CreatePlanRequest{
			AgentThreadID:             call.ThreadID,
			MatterID:                  input.MatterID,
			PreparationProfileID:      input.PreparationProfileID,
			PreparationSnapshotID:     input.PreparationSnapshotID,
			ScenarioDefinitionID:      input.ScenarioDefinitionID,
			ScenarioDefinitionVersion: input.ScenarioDefinitionVersion,
			ScenarioConfigID:          input.ScenarioConfigID,
			ScenarioConfigVersion:     input.ScenarioConfigVersion,
			SelectedRoleIDs:           append([]string(nil), input.SelectedRoleIDs...),
			PracticeOptionID:          input.PracticeOptionID,
			PracticeOptionVersion:     input.PracticeOptionVersion,
			MaxEffectiveTurns:         input.MaxEffectiveTurns,
		},
	)
	if err != nil {
		return PreviewResult{}, mapPracticeToolError(err)
	}
	if plan.CatalogSnapshot == nil || plan.SessionPolicy == nil {
		return PreviewResult{}, tool.ErrExecutionRejected
	}
	sourceRefs := []tool.SourceRef{{Type: "practice_plan", ID: plan.ID}}
	if plan.PreparationSnapshot != nil {
		sourceRefs = append(sourceRefs, tool.SourceRef{
			Type: "preparation_snapshot",
			ID:   plan.PreparationSnapshot.ID,
		})
	} else {
		sourceRefs = append(sourceRefs, tool.SourceRef{
			Type: "preparation_profile",
			ID:   plan.PreparationProfileID,
		})
	}
	return PreviewResult{
		Status:             "preview_ready",
		PracticePlanID:     plan.ID,
		PlanRevision:       plan.Revision,
		PracticePlanStatus: string(plan.Status),
		ScenarioName:       plan.CatalogSnapshot.ScenarioDefinition.Name,
		ScenarioFamily:     string(plan.ScenarioType),
		ScenarioModel:      string(plan.ScenarioModel),
		SelectedRoleIDs:    append([]string(nil), plan.SelectedRoleIDs...),
		PracticeOptionID:   plan.CatalogSnapshot.PracticeOption.ID,
		MaxEffectiveTurns:  plan.SessionPolicy.MaxEffectiveTurns,
		Replayed:           replayed,
		SourceRefs:         sourceRefs,
	}, nil
}

func (port *ServicePort) resolveCandidates(
	query string,
) ([]CatalogCandidate, error) {
	if query == "" {
		return nil, nil
	}
	items, err := port.catalog.ResolvePreviewCatalog(query)
	if err != nil {
		return nil, err
	}
	result := make([]CatalogCandidate, len(items))
	for index, item := range items {
		result[index] = CatalogCandidate{
			ScenarioDefinitionID:      item.ScenarioDefinition.ID,
			ScenarioDefinitionVersion: item.ScenarioDefinition.Version,
			Name:                      item.ScenarioDefinition.Name,
			ScenarioFamily:            string(item.ScenarioDefinition.Type),
			ScenarioModel:             string(item.ScenarioDefinition.Model),
			ScenarioConfigID:          item.ScenarioConfig.ID,
			ScenarioConfigVersion:     item.ScenarioConfig.Version,
			DefaultRoleIDs: append(
				[]string(nil),
				item.DefaultRoleIDs...,
			),
			DefaultPracticeOptionID:      item.DefaultOption.ID,
			DefaultPracticeOptionVersion: item.DefaultOption.Version,
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
	if input.ScenarioDefinitionID == "" {
		input.ScenarioDefinitionID = candidate.ScenarioDefinitionID
		input.ScenarioDefinitionVersion = candidate.ScenarioDefinitionVersion
	}
	if input.ScenarioConfigID == "" {
		input.ScenarioConfigID = candidate.ScenarioConfigID
		input.ScenarioConfigVersion = candidate.ScenarioConfigVersion
	}
	if len(input.SelectedRoleIDs) == 0 {
		input.SelectedRoleIDs = append([]string(nil), candidate.DefaultRoleIDs...)
	}
	if input.PracticeOptionID == "" {
		input.PracticeOptionID = candidate.DefaultPracticeOptionID
		input.PracticeOptionVersion = candidate.
			DefaultPracticeOptionVersion
	}
	return input
}

func previewMissingFields(input PreviewInput) []string {
	missing := make([]string, 0, 5)
	if input.PreparationSnapshotID == "" &&
		input.PreparationProfileID == "" &&
		input.BackgroundSummary == "" {
		missing = append(missing, "background_summary")
	}
	if input.ScenarioDefinitionID == "" ||
		input.ScenarioDefinitionVersion < 1 ||
		input.ScenarioConfigID == "" ||
		input.ScenarioConfigVersion < 1 {
		missing = append(missing, "scenario_selection")
	}
	if len(input.SelectedRoleIDs) == 0 {
		missing = append(missing, "role_selection")
	}
	if input.PracticeOptionID == "" || input.PracticeOptionVersion < 1 {
		missing = append(missing, "practice_option")
	}
	if input.MaxEffectiveTurns < 1 {
		missing = append(missing, "max_effective_turns")
	}
	return missing
}

func mapPracticeToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, persistence.ErrInvalidArgument),
		errors.Is(err, preparation.ErrCatalogSelectionInvalid),
		errors.Is(err, preparation.ErrProfileInvalid):
		return tool.ErrInvalidInput
	case errors.Is(err, persistence.ErrNotFound),
		errors.Is(err, persistence.ErrConflict),
		errors.Is(err, persistence.ErrIdempotencyConflict),
		errors.Is(err, preparation.ErrProfileNotFound),
		errors.Is(err, preparation.ErrProfileConflict),
		errors.Is(err, preparation.ErrProfileIdempotencyConflict):
		return tool.ErrExecutionRejected
	default:
		return err
	}
}
