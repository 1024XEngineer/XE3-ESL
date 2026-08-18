package scene

import (
	"errors"
	"fmt"
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

// CustomSceneDraft contains user-authored facts plus an experience resolved by
// the application layer. Role and goal are optional user facts; they are not
// required questions for creating a one-off scene.
type CustomSceneDraft struct {
	Scenario       string
	UserRole       string
	AIRole         string
	PracticeGoal   string
	ExperienceHint PracticeExperience
}

// CustomSceneCompiler is the explicit boundary between user-authored intent
// and the complete executable CustomSceneSpec frozen into a Plan. Its neutral
// role rules do not pretend to know scenario-specific facts the user did not
// provide.
type CustomSceneCompiler struct{}

func (CustomSceneCompiler) Compile(
	draft CustomSceneDraft,
) (CustomSceneSpec, error) {
	if !validCustomText(draft.Scenario, 200) ||
		!validOptionalCustomText(draft.UserRole, 200) ||
		!validOptionalCustomText(draft.AIRole, 200) ||
		!validOptionalCustomText(draft.PracticeGoal, 500) ||
		(draft.ExperienceHint != PracticeExperienceWorkplace &&
			draft.ExperienceHint != PracticeExperienceLifeAndTravel) {
		return CustomSceneSpec{}, ErrCustomSceneInvalid
	}
	spec := CustomSceneSpec{
		Scenario:       draft.Scenario,
		UserRole:       draft.UserRole,
		AIRole:         draft.AIRole,
		PracticeGoal:   draft.PracticeGoal,
		ExperienceHint: draft.ExperienceHint,
	}
	if spec.UserRole == "" {
		spec.UserRole = "场景中的英语学习者"
	}
	if spec.AIRole == "" {
		spec.AIRole = "场景中的对话方"
	}
	if spec.PracticeGoal == "" {
		spec.PracticeGoal = fmt.Sprintf(
			"用英语清晰、自然地完成“%s”场景中的沟通",
			spec.Scenario,
		)
	}
	if !validCustomSceneSpec(spec) {
		return CustomSceneSpec{}, ErrCustomSceneInvalid
	}
	return spec, nil
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

func validOptionalCustomText(value string, maxRunes int) bool {
	return value == "" || validCustomText(value, maxRunes)
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
