// Package preparationsource maps confirmed Preparation Plans into immutable
// Practice-owned execution projections.
package preparationsource

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Reader struct {
	plans preparation.PlanReader
}

func New(plans preparation.PlanReader) (*Reader, error) {
	if plans == nil {
		return nil, errors.New(
			"practice Preparation source: Plan reader is required",
		)
	}
	return &Reader{plans: plans}, nil
}

func (reader *Reader) ReadExecutablePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	exactRevision int,
) (practice.PlanProjection, error) {
	plan, err := reader.plans.ReadExecutablePlan(
		ctx,
		actor,
		planID,
		exactRevision,
	)
	if err != nil {
		return practice.PlanProjection{}, mapReadError(err)
	}
	return ProjectConfirmedPlan(plan)
}

// ProjectConfirmedPlan copies one confirmed Preparation Plan into Practice's
// immutable execution model. Callers must not retain editable Plan values in
// a running Practice Session.
func ProjectConfirmedPlan(
	plan preparation.PracticePlan,
) (practice.PlanProjection, error) {
	if plan.Status != preparation.PlanStatusReady {
		return practice.PlanProjection{}, practice.ErrConflict
	}
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return practice.PlanProjection{}, practice.ErrConflict
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		return practice.PlanProjection{}, practice.ErrConflict
	}
	selection := projectSceneSelection(plan.SceneSelection, roles, option)
	policy, err := projectSessionPolicy(
		plan.SessionPolicy,
		selection.Scene.SessionPolicyRef,
		selection.Scene.Prompt,
		option,
	)
	if err != nil {
		return practice.PlanProjection{}, err
	}
	objectives := make(
		[]practice.PracticeObjective,
		len(plan.PracticeObjectives),
	)
	for index, objective := range plan.PracticeObjectives {
		objectives[index] = practice.PracticeObjective{
			ID: objective.ID, Description: objective.Description,
		}
	}
	return practice.PlanProjection{
		ID:                 plan.ID,
		OwnerUserID:        plan.UserID,
		Revision:           plan.Revision,
		Preparation:        projectPreparation(plan.PreparationSnapshot),
		SceneSelection:     selection,
		SessionPolicy:      policy,
		PracticeObjectives: objectives,
		IELTSAssignment:    projectIELTSAssignment(plan.IELTSAssignment),
	}, nil
}

func projectSceneSelection(
	selection scene.SelectionSnapshot,
	selectedRoles []scene.RoleDefinition,
	selectedOption scene.PracticeOption,
) practice.SceneSelection {
	roles := make([]practice.RoleDefinition, len(selectedRoles))
	for index, role := range selectedRoles {
		roles[index] = projectRole(role)
	}
	return practice.SceneSelection{
		Scene: practice.SceneDefinition{
			ID:                  selection.Scene.ID,
			Family:              practice.SceneFamily(selection.Scene.Family),
			Model:               practice.SceneModel(selection.Scene.Model),
			Name:                selection.Scene.Name,
			Version:             selection.Scene.Version,
			Status:              practice.SceneStatus(selection.Scene.Status),
			TurnPolicyRef:       selection.Scene.TurnPolicyRef,
			SessionPolicyRef:    selection.Scene.SessionPolicyRef,
			EvaluationPolicyRef: selection.Scene.EvaluationPolicyRef,
			Prompt:              projectPrompt(selection.Scene.Prompt),
			Roles:               roles,
			PracticeOptions: []practice.PracticeOption{
				projectPracticeOption(selectedOption),
			},
		},
		SelectedRoleIDs:  append([]string(nil), selection.SelectedRoleIDs...),
		PracticeOptionID: selection.PracticeOptionID,
	}
}

func projectPrompt(prompt scene.ScenePrompt) practice.ScenePrompt {
	return practice.ScenePrompt{
		PublicSceneBrief: prompt.PublicSceneBrief,
		PracticeGoal:     prompt.PracticeGoal,
		UserRole:         prompt.UserRole,
		AIRole:           prompt.AIRole,
		PersonaSummary:   prompt.PersonaSummary,
		FocusAreas:       append([]string(nil), prompt.FocusAreas...),
		TurnBlueprints: append(
			[]string(nil),
			prompt.TurnBlueprints...,
		),
		SuggestedDurationSeconds: prompt.SuggestedDurationSeconds,
	}
}

func projectRole(role scene.RoleDefinition) practice.RoleDefinition {
	objectives := make(
		[]practice.PracticeObjectiveDefinition,
		len(role.PracticeObjectives),
	)
	for index, objective := range role.PracticeObjectives {
		objectives[index] = practice.PracticeObjectiveDefinition{
			ID: objective.ID, Description: objective.Description,
		}
	}
	return practice.RoleDefinition{
		ID:                 role.ID,
		SceneID:            role.SceneID,
		Type:               role.Type,
		DisplayName:        role.DisplayName,
		Responsibilities:   role.Responsibilities,
		Style:              role.Style,
		PracticeObjectives: objectives,
		VoiceConfigRef:     role.VoiceConfigRef,
	}
}

func projectPracticeOption(
	option scene.PracticeOption,
) practice.PracticeOption {
	return practice.PracticeOption{
		ID:               option.ID,
		SceneID:          option.SceneID,
		RoleDefinitionID: option.RoleDefinitionID,
		Type:             practice.PracticeOptionType(option.Type),
		DisplayName:      option.DisplayName,
	}
}

func projectSessionPolicy(
	policy preparation.SessionPolicy,
	reference string,
	prompt practice.ScenePrompt,
	option scene.PracticeOption,
) (practice.SessionPolicy, error) {
	registered, err := practice.ResolveSessionPolicy(
		reference,
		prompt,
		projectPracticeOption(option),
		policy.MaxEffectiveTurns,
	)
	if err != nil {
		return practice.SessionPolicy{}, practice.ErrConflict
	}
	projected := practice.SessionPolicy{
		SuggestedDurationSeconds: policy.SuggestedDurationSeconds,
		MinEffectiveTurns:        policy.MinEffectiveTurns,
		MaxEffectiveTurns:        policy.MaxEffectiveTurns,
		CoverageCheckpointTurn:   policy.CoverageCheckpointTurn,
		MaxFollowUpsPerQuestion:  policy.MaxFollowUpsPerQuestion,
		EarlyCompletionRule: practice.EarlyCompletionRule(
			policy.EarlyCompletionRule,
		),
		RetryAllowed:               policy.RetryAllowed,
		QuestionTranslationAllowed: policy.QuestionTranslationAllowed,
	}
	if projected != registered {
		return practice.SessionPolicy{}, practice.ErrConflict
	}
	return registered, nil
}

func projectPreparation(
	snapshot preparation.Snapshot,
) practice.PreparationSnapshot {
	result := practice.PreparationSnapshot{
		ID:                                 snapshot.ID,
		SourceProfileID:                    snapshot.SourceProfileID,
		SourceVersion:                      snapshot.SourceVersion,
		SourceJobTargetID:                  snapshot.SourceJobTargetID,
		SourceJobTargetConfirmationVersion: snapshot.SourceJobTargetConfirmationVersion,
		ResumeSnapshot:                     projectResumeSnapshot(snapshot.ResumeSnapshot),
		JobDescriptionSnapshot:             snapshot.JobDescriptionSnapshot,
		BackgroundSnapshot:                 snapshot.BackgroundSnapshot,
		CreatedAt:                          snapshot.CreatedAt,
	}
	if input := snapshot.JobTargetInputSnapshot; input != nil {
		result.JobTargetInputSnapshot = &practice.JobTargetInput{
			Source:              string(input.Source),
			JobTitle:            input.JobTitle,
			JobDescription:      input.JobDescription,
			Company:             input.Company,
			Seniority:           input.Seniority,
			CandidateBackground: input.CandidateBackground,
			PracticeFocus:       input.PracticeFocus,
		}
	}
	if candidate := snapshot.JobTargetCandidateSnapshot; candidate != nil {
		result.JobTargetCandidateSnapshot = &practice.JobTargetCandidate{
			Source:             string(candidate.Source),
			GeneralAdviceOnly:  candidate.GeneralAdviceOnly,
			JobTitle:           candidate.JobTitle,
			Seniority:          candidate.Seniority,
			Responsibilities:   append([]string(nil), candidate.Responsibilities...),
			CoreSkills:         append([]string(nil), candidate.CoreSkills...),
			CommunicationFocus: append([]string(nil), candidate.CommunicationFocus...),
			PracticeGoals:      append([]string(nil), candidate.PracticeGoals...),
			ScopeNotice:        candidate.ScopeNotice,
			CatalogRecommendation: practice.JobTargetCatalogRecommendation{
				SceneID: candidate.CatalogRecommendation.SceneID,
				SceneVersion: candidate.CatalogRecommendation.
					SceneVersion,
				SelectedRoleIDs: append(
					[]string(nil),
					candidate.CatalogRecommendation.SelectedRoleIDs...,
				),
				PracticeOptionID: candidate.CatalogRecommendation.
					PracticeOptionID,
			},
		}
	}
	return result
}

func projectResumeSnapshot(
	snapshot *preparation.ResumeRevisionSnapshot,
) *practice.ResumeRevisionSnapshot {
	if snapshot == nil {
		return nil
	}
	material := practice.ResumeMaterial{
		TargetPosition:       snapshot.Material.TargetPosition,
		ProfessionalSummary:  snapshot.Material.ProfessionalSummary,
		WorkExperiences:      make([]practice.ResumeWorkExperience, len(snapshot.Material.WorkExperiences)),
		ProjectExperiences:   make([]practice.ResumeProjectExperience, len(snapshot.Material.ProjectExperiences)),
		EducationExperiences: make([]practice.ResumeEducationExperience, len(snapshot.Material.EducationExperiences)),
		Skills:               append([]string(nil), snapshot.Material.Skills...),
		Awards:               append([]string(nil), snapshot.Material.Awards...),
	}
	for index, item := range snapshot.Material.WorkExperiences {
		material.WorkExperiences[index] = practice.ResumeWorkExperience{
			Company: item.Company, Position: item.Position,
			StartDate: item.StartDate, EndDate: item.EndDate,
			Duties:       append([]string(nil), item.Duties...),
			Achievements: append([]string(nil), item.Achievements...),
		}
	}
	for index, item := range snapshot.Material.ProjectExperiences {
		material.ProjectExperiences[index] = practice.ResumeProjectExperience{
			ProjectName: item.ProjectName, Role: item.Role,
			Description:  item.Description,
			Technologies: append([]string(nil), item.Technologies...),
			Duties:       append([]string(nil), item.Duties...),
			Achievements: append([]string(nil), item.Achievements...),
		}
	}
	for index, item := range snapshot.Material.EducationExperiences {
		material.EducationExperiences[index] = practice.ResumeEducationExperience{
			School: item.School, Major: item.Major, Degree: item.Degree,
			GPA: item.GPA, StartDate: item.StartDate, EndDate: item.EndDate,
		}
	}
	return &practice.ResumeRevisionSnapshot{
		ResumeID: snapshot.ResumeID,
		Revision: snapshot.Revision,
		Material: material,
	}
}

func projectIELTSAssignment(
	assignment *preparation.IELTSAssignmentSnapshot,
) *practice.IELTSAssignment {
	if assignment == nil {
		return nil
	}
	return &practice.IELTSAssignment{
		BankID:         assignment.BankID,
		Season:         assignment.Season,
		Mode:           practice.IELTSPracticeMode(assignment.Mode),
		Part1SetID:     assignment.Part1SetID,
		TopicGroupID:   assignment.TopicGroupID,
		TopicTitle:     assignment.TopicTitle,
		Part2CueCard:   assignment.Part2CueCard,
		Part1Questions: assignment.Part1Questions,
		Part2Questions: assignment.Part2Questions,
		Part3Questions: assignment.Part3Questions,
		TurnBlueprints: append([]string(nil), assignment.TurnBlueprints...),
	}
}

func mapReadError(err error) error {
	switch {
	case errors.Is(err, preparation.ErrPlanInvalid):
		return practice.ErrInvalidArgument
	case errors.Is(err, preparation.ErrPlanNotFound):
		return practice.ErrNotFound
	case errors.Is(err, preparation.ErrPlanConflict):
		return practice.ErrConflict
	default:
		return err
	}
}

var _ practice.PlanProjectionReader = (*Reader)(nil)
