// Package planpolicy exposes Practice-owned execution policy values to
// Preparation while one confirmed Plan revision is being created.
package planpolicy

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

// Resolver lets Preparation freeze the policy values resolved by Practice's
// single registry without owning or duplicating execution behavior.
type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

func (*Resolver) ResolveSessionPolicy(
	definition scene.ExecutableSceneSnapshot,
	option scene.PracticeOptionSnapshot,
	requestedMaxEffectiveTurns int,
) (preparation.SessionPolicy, error) {
	policy, err := practice.ResolveSessionPolicy(
		option.SessionPolicyRef,
		practice.ScenePrompt{
			PublicSceneBrief: definition.Prompt.PublicSceneBrief,
			PracticeGoal:     definition.Prompt.PracticeGoal,
			UserRole:         definition.Prompt.UserRole,
			AIRole:           definition.Prompt.AIRole,
			PersonaSummary:   definition.Prompt.PersonaSummary,
			FocusAreas: append(
				[]string(nil), definition.Prompt.FocusAreas...,
			),
			TurnBlueprints: append(
				[]string(nil), definition.Prompt.TurnBlueprints...,
			),
		},
		practice.PracticeOption{
			ID:                       option.ID,
			SceneID:                  option.SceneKey,
			RoleDefinitionID:         option.RoleDefinitionID,
			Mode:                     practice.PracticeMode(option.Mode),
			DisplayName:              option.DisplayName,
			SuggestedDurationSeconds: option.SuggestedDurationSeconds,
			TurnPolicyRef:            option.TurnPolicyRef,
			SessionPolicyRef:         option.SessionPolicyRef,
			EvaluationPolicyRef:      option.EvaluationPolicyRef,
		},
		requestedMaxEffectiveTurns,
	)
	if err != nil {
		switch {
		case errors.Is(err, practice.ErrInvalidArgument):
			return preparation.SessionPolicy{}, preparation.ErrPlanInvalid
		case errors.Is(err, practice.ErrConflict),
			errors.Is(err, practice.ErrExecutionPolicyNotFound):
			return preparation.SessionPolicy{}, preparation.ErrPlanConflict
		default:
			return preparation.SessionPolicy{}, err
		}
	}
	return preparation.SessionPolicy{
		CompletionMode:           preparation.CompletionMode(policy.CompletionMode),
		SuggestedDurationSeconds: policy.SuggestedDurationSeconds,
		MinEffectiveTurns:        policy.MinEffectiveTurns,
		MaxEffectiveTurns:        policy.MaxEffectiveTurns,
		CoverageCheckpointTurn:   policy.CoverageCheckpointTurn,
		MaxFollowUpsPerQuestion:  policy.MaxFollowUpsPerQuestion,
		EarlyCompletionRule: preparation.EarlyCompletionRule(
			policy.EarlyCompletionRule,
		),
		RetryAllowed:               policy.RetryAllowed,
		QuestionTranslationAllowed: policy.QuestionTranslationAllowed,
		QuestionTipsAllowed:        policy.QuestionTipsAllowed,
		SpeechFeedbackAllowed:      policy.SpeechFeedbackAllowed,
	}, nil
}

var _ preparation.PolicyResolver = (*Resolver)(nil)
