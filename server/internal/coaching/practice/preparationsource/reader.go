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
	exactVersion int,
) (practice.PlanProjection, error) {
	plan, err := reader.plans.ReadExecutablePlan(
		ctx,
		actor,
		planID,
		exactVersion,
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
		projectPracticeOption(option),
		selection.Scene.Prompt,
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
		Version:            plan.Version,
		Preparation:        projectPreparation(plan.PreparationSnapshot),
		SceneSelection:     selection,
		SessionPolicy:      policy,
		PracticeObjectives: objectives,
		IELTSAssignment:    projectIELTSAssignment(plan.IELTSAssignment),
	}, nil
}

func projectSceneSelection(
	selection scene.SelectionSnapshot,
	selectedRoles []scene.RoleSnapshot,
	selectedOption scene.PracticeOptionSnapshot,
) practice.SceneSelection {
	roles := make([]practice.RoleDefinition, len(selectedRoles))
	for index, role := range selectedRoles {
		roles[index] = projectRole(role)
	}
	return practice.SceneSelection{
		Scene: practice.SceneDefinition{
			ID: selection.Scene.Key,
			Experience: practice.PracticeExperience(
				selection.Scene.Experience,
			),
			Category: practice.SceneCategory(selection.Scene.Category),
			Name:     selection.Scene.Name,
			Version:  selection.Scene.Revision,
			Status:   practice.SceneStatusActive,
			Prompt:   projectPrompt(selection.Scene.Prompt),
			Roles:    roles,
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
	}
}

func projectRole(role scene.RoleSnapshot) practice.RoleDefinition {
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
		SceneID:            role.SceneKey,
		Type:               role.Type,
		DisplayName:        role.DisplayName,
		Responsibilities:   role.Responsibilities,
		Style:              role.Style,
		PracticeObjectives: objectives,
		VoiceConfigRef:     role.VoiceConfigRef,
	}
}

func projectPracticeOption(
	option scene.PracticeOptionSnapshot,
) practice.PracticeOption {
	return practice.PracticeOption{
		ID:                       option.ID,
		SceneID:                  option.SceneKey,
		RoleDefinitionID:         option.RoleDefinitionID,
		Mode:                     practice.PracticeMode(option.Mode),
		DisplayName:              option.DisplayName,
		SuggestedDurationSeconds: option.SuggestedDurationSeconds,
		TurnPolicyRef:            option.TurnPolicyRef,
		SessionPolicyRef:         option.SessionPolicyRef,
		EvaluationPolicyRef:      option.EvaluationPolicyRef,
	}
}

func projectSessionPolicy(
	policy preparation.SessionPolicy,
	option practice.PracticeOption,
	prompt practice.ScenePrompt,
) (practice.SessionPolicy, error) {
	registered, err := practice.ResolveSessionPolicy(
		option.SessionPolicyRef,
		prompt,
		option,
		policy.MaxEffectiveTurns,
	)
	if err != nil {
		return practice.SessionPolicy{}, practice.ErrConflict
	}
	projected := practice.SessionPolicy{
		CompletionMode:           practice.CompletionMode(policy.CompletionMode),
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
		QuestionTipsAllowed:        policy.QuestionTipsAllowed,
		SpeechFeedbackAllowed:      policy.SpeechFeedbackAllowed,
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
		BackgroundSnapshot: snapshot.BackgroundSummary,
	}
	if snapshot.Interview == nil {
		return result
	}
	interview := snapshot.Interview
	result.InterviewPreparationID = interview.ID
	result.InterviewPreparationVersion = interview.Version
	result.ResumeMaterial = projectResumeMaterial(interview.ResumeContent)
	input := interview.Input
	result.JobTargetInputSnapshot = &practice.JobTargetInput{
		Source:              string(input.Source),
		JobTitle:            input.JobTitle,
		JobDescription:      input.JobDescription,
		Company:             input.Company,
		Seniority:           input.Seniority,
		CandidateBackground: input.CandidateBackground,
		PracticeFocus:       input.PracticeFocus,
	}
	candidate := interview.Candidate
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
			SceneID:          candidate.CatalogRecommendation.SceneID,
			SceneVersion:     candidate.CatalogRecommendation.SceneVersion,
			SelectedRoleIDs:  append([]string(nil), candidate.CatalogRecommendation.SelectedRoleIDs...),
			PracticeOptionID: candidate.CatalogRecommendation.PracticeOptionID,
		},
	}
	return result
}

func projectResumeMaterial(
	snapshot *preparation.ResumeMaterial,
) *practice.ResumeMaterial {
	if snapshot == nil {
		return nil
	}
	material := practice.ResumeMaterial{
		TargetPosition:       snapshot.TargetPosition,
		ProfessionalSummary:  snapshot.ProfessionalSummary,
		WorkExperiences:      make([]practice.ResumeWorkExperience, len(snapshot.WorkExperiences)),
		ProjectExperiences:   make([]practice.ResumeProjectExperience, len(snapshot.ProjectExperiences)),
		EducationExperiences: make([]practice.ResumeEducationExperience, len(snapshot.EducationExperiences)),
		Skills:               append([]string(nil), snapshot.Skills...),
		Awards:               append([]string(nil), snapshot.Awards...),
	}
	for index, item := range snapshot.WorkExperiences {
		material.WorkExperiences[index] = practice.ResumeWorkExperience{
			Company: item.Company, Position: item.Position,
			StartDate: item.StartDate, EndDate: item.EndDate,
			Duties:       append([]string(nil), item.Duties...),
			Achievements: append([]string(nil), item.Achievements...),
		}
	}
	for index, item := range snapshot.ProjectExperiences {
		material.ProjectExperiences[index] = practice.ResumeProjectExperience{
			ProjectName: item.ProjectName, Role: item.Role,
			Description:  item.Description,
			Technologies: append([]string(nil), item.Technologies...),
			Duties:       append([]string(nil), item.Duties...),
			Achievements: append([]string(nil), item.Achievements...),
		}
	}
	for index, item := range snapshot.EducationExperiences {
		material.EducationExperiences[index] = practice.ResumeEducationExperience{
			School: item.School, Major: item.Major, Degree: item.Degree,
			GPA: item.GPA, StartDate: item.StartDate, EndDate: item.EndDate,
		}
	}
	return &material
}

func projectIELTSAssignment(
	assignment *preparation.IELTSAssignmentSnapshot,
) *practice.IELTSAssignment {
	if assignment == nil {
		return nil
	}
	result := &practice.IELTSAssignment{
		BankID: assignment.BankID,
		Season: assignment.Season,
		Mode:   practice.PracticeMode(assignment.Mode),
		Parts:  make([]practice.IELTSPart, len(assignment.Parts)),
	}
	for index, part := range assignment.Parts {
		result.Parts[index] = practice.IELTSPart{
			Part:            practice.PracticeMode(part.Part),
			SourceID:        part.SourceID,
			TopicTitle:      part.TopicTitle,
			CueCard:         part.CueCard,
			TurnBlueprints:  append([]string(nil), part.TurnBlueprints...),
			PreparedAnswers: make([]practice.IELTSPreparedAnswer, len(part.PreparedAnswers)),
		}
		for answerIndex, answer := range part.PreparedAnswers {
			result.Parts[index].PreparedAnswers[answerIndex] = practice.IELTSPreparedAnswer{
				QuestionPosition: answer.QuestionPosition,
				Answer:           answer.Answer,
				Personalized:     answer.Personalized,
			}
		}
	}
	return result
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
