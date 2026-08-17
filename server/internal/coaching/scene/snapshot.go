package scene

import "strings"

func ValidSelectionSnapshot(selection SelectionSnapshot) bool {
	snapshot := selection.Scene
	if !validResourceID(snapshot.Key) || snapshot.Revision < 1 ||
		!validExperienceCategory(snapshot.Experience, snapshot.Category) ||
		!nonBlank(snapshot.Name) || !validScenePrompt(snapshot.Prompt) ||
		len(snapshot.Roles) == 0 || len(snapshot.PracticeOptions) == 0 ||
		len(selection.SelectedRoleIDs) == 0 || selection.PracticeOptionID == "" {
		return false
	}
	switch selection.Source.Type {
	case SceneSourceCatalog:
		if !validResourceID(selection.Source.SceneID) ||
			selection.Source.SceneVersion < 1 ||
			snapshot.Key != selection.Source.SceneID ||
			snapshot.Revision != selection.Source.SceneVersion {
			return false
		}
	case SceneSourceCustom:
		if selection.Source.SceneID != "" || selection.Source.SceneVersion != 0 ||
			!strings.HasPrefix(snapshot.Key, "custom:") || snapshot.Revision != 1 ||
			(snapshot.Experience != PracticeExperienceWorkplace &&
				snapshot.Experience != PracticeExperienceLifeAndTravel) {
			return false
		}
	default:
		return false
	}

	roleIDs := make(map[string]struct{}, len(snapshot.Roles))
	for _, role := range snapshot.Roles {
		if !validResourceID(role.ID) || role.SceneKey != snapshot.Key ||
			!roleTypePattern.MatchString(role.Type) || !nonBlank(role.DisplayName) ||
			!nonBlank(role.Responsibilities) || !nonBlank(role.Style) ||
			!validPracticeObjectiveDefinitions(role.PracticeObjectives) {
			return false
		}
		if _, duplicate := roleIDs[role.ID]; duplicate {
			return false
		}
		roleIDs[role.ID] = struct{}{}
	}
	optionIDs := make(map[string]struct{}, len(snapshot.PracticeOptions))
	for _, option := range snapshot.PracticeOptions {
		if !validResourceID(option.ID) || option.SceneKey != snapshot.Key ||
			!nonBlank(option.DisplayName) || option.SuggestedDurationSeconds < 1 ||
			!validPolicyRef(option.TurnPolicyRef, ".turn.v1") ||
			!validPolicyRef(option.SessionPolicyRef, ".session.v1") ||
			!validPolicyRef(option.EvaluationPolicyRef, ".evaluation.v1") {
			return false
		}
		if _, duplicate := optionIDs[option.ID]; duplicate {
			return false
		}
		optionIDs[option.ID] = struct{}{}
		if option.RoleDefinitionID != "" {
			if _, found := roleIDs[option.RoleDefinitionID]; !found {
				return false
			}
		}
	}
	selected := make(map[string]struct{}, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		if _, found := roleIDs[roleID]; !found {
			return false
		}
		if _, duplicate := selected[roleID]; duplicate {
			return false
		}
		selected[roleID] = struct{}{}
	}
	_, found := optionIDs[selection.PracticeOptionID]
	return found
}
