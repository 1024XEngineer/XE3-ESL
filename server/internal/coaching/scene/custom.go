package scene

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrCustomSceneInvalid = errors.New("custom scene is invalid")

type CustomSceneSpec struct {
	Scenario       string             `json:"scenario"`
	UserRole       string             `json:"user_role"`
	AIRole         string             `json:"ai_role"`
	PracticeGoal   string             `json:"practice_goal"`
	ExperienceHint PracticeExperience `json:"experience_hint"`
}

func NewCustomSelection(
	planID string,
	spec CustomSceneSpec,
) (SelectionSnapshot, error) {
	if !validCustomSceneSpec(spec) || !validResourceID(planID) {
		return SelectionSnapshot{}, ErrCustomSceneInvalid
	}
	category, option := customExecutionPolicy(spec.ExperienceHint)
	if category == "" {
		return SelectionSnapshot{}, ErrCustomSceneInvalid
	}
	sceneKey := "custom:" + planID
	role := RoleSnapshot{
		ID:               "custom_counterpart",
		SceneKey:         sceneKey,
		Type:             "CUSTOM_COUNTERPART",
		DisplayName:      spec.AIRole,
		Responsibilities: spec.PracticeGoal,
		Style:            "Stay realistic, respond in role, and adapt to the user's choices.",
		PracticeObjectives: []PracticeObjectiveDefinition{{
			ID:          "custom_practice_goal",
			Description: spec.PracticeGoal,
		}},
	}
	option.SceneKey = sceneKey
	return SelectionSnapshot{
		Source: SceneSource{Type: SceneSourceCustom},
		Scene: ExecutableSceneSnapshot{
			Key:        sceneKey,
			Revision:   1,
			Experience: spec.ExperienceHint,
			Category:   category,
			Name:       spec.Scenario,
			Prompt: ScenePrompt{
				PublicSceneBrief: spec.Scenario,
				PracticeGoal:     spec.PracticeGoal,
				UserRole:         spec.UserRole,
				AIRole:           spec.AIRole,
				PersonaSummary:   spec.AIRole,
				FocusAreas:       []string{spec.PracticeGoal},
				TurnBlueprints: []string{
					"Establish the situation and invite the user to begin.",
					"Respond in role and explore the user's main goal.",
					"Introduce one realistic complication or objection.",
					"Ask one relevant follow-up that requires clarification or support.",
					"Close the interaction with a clear outcome.",
				},
			},
			Roles:           []RoleSnapshot{role},
			PracticeOptions: []PracticeOptionSnapshot{option},
		},
		SelectedRoleIDs:  []string{role.ID},
		PracticeOptionID: option.ID,
	}, nil
}

func validCustomSceneSpec(spec CustomSceneSpec) bool {
	return validCustomText(spec.Scenario, 200) &&
		validCustomText(spec.UserRole, 200) &&
		validCustomText(spec.AIRole, 200) &&
		validCustomText(spec.PracticeGoal, 500) &&
		(spec.ExperienceHint == PracticeExperienceWorkplace ||
			spec.ExperienceHint == PracticeExperienceLifeAndTravel)
}

func validCustomText(value string, maxRunes int) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes &&
		!strings.ContainsRune(value, '\x00')
}

func customExecutionPolicy(
	experience PracticeExperience,
) (SceneCategory, PracticeOptionSnapshot) {
	switch experience {
	case PracticeExperienceWorkplace:
		return SceneCategoryWorkplaceGeneral, PracticeOptionSnapshot{
			ID:                       "custom_full_simulation",
			Mode:                     PracticeModeFullSimulation,
			DisplayName:              "完整模拟",
			SuggestedDurationSeconds: 480,
			TurnPolicyRef:            "generic.practice.turn.v1",
			SessionPolicyRef:         "workplace.practice.session.v1",
			EvaluationPolicyRef:      "workplace.general.evaluation.v1",
		}
	case PracticeExperienceLifeAndTravel:
		return SceneCategoryLifeDaily, PracticeOptionSnapshot{
			ID:                       "custom_full_simulation",
			Mode:                     PracticeModeFullSimulation,
			DisplayName:              "完整模拟",
			SuggestedDurationSeconds: 480,
			TurnPolicyRef:            "generic.practice.turn.v1",
			SessionPolicyRef:         "daily.practice.session.v1",
			EvaluationPolicyRef:      "daily.general.evaluation.v1",
		}
	default:
		return "", PracticeOptionSnapshot{}
	}
}
