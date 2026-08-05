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
	definition scene.SceneDefinition,
	option scene.PracticeOption,
	requestedMaxEffectiveTurns int,
) (preparation.SessionPolicy, error) {
	policy, err := practice.ResolveSessionPolicy(
		definition.SessionPolicyRef,
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
			SuggestedDurationSeconds: definition.Prompt.
				SuggestedDurationSeconds,
		},
		practice.PracticeOption{
			ID:               option.ID,
			SceneID:          option.SceneID,
			RoleDefinitionID: option.RoleDefinitionID,
			Type:             practice.PracticeOptionType(option.Type),
			DisplayName:      option.DisplayName,
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
	}, nil
}

var _ preparation.PolicyResolver = (*Resolver)(nil)
