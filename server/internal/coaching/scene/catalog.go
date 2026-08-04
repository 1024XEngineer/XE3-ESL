package scene

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrSceneNotFound            = errors.New("scene not found")
	ErrRoleDefinitionNotFound   = errors.New("role definition not found")
	ErrPracticeOptionNotFound   = errors.New("practice option not found")
	ErrCatalogSelectionInvalid  = errors.New("scene selection is invalid")
	ErrCatalogDefinitionInvalid = errors.New("scene definition is invalid")
	ErrCatalogReadFailed        = errors.New("scene catalog read failed")
	ErrCatalogContextRequired   = errors.New("scene catalog context is required")
)

var (
	roleTypePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	objectiveIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
)

// CatalogReader is Scene's read-only application boundary.
type CatalogReader interface {
	ListActiveScenes(context.Context) ([]SceneDefinition, error)
	GetScene(context.Context, string) (SceneDefinition, error)
	ListRoles(context.Context, string) ([]RoleDefinition, error)
	ResolveSelection(
		ctx context.Context,
		sceneID string,
		sceneVersion int,
		selectedRoleIDs []string,
		practiceOptionID string,
	) (SelectionSnapshot, error)
}

// AccessibleSelectionReader resolves the latest active Scene version visible
// to one user. Public Scenes are visible to every user; private Scenes are
// visible only to their owner. It is separate from CatalogReader because the
// anonymous HTTP catalog intentionally exposes public Scenes only.
type AccessibleSelectionReader interface {
	ResolveAccessibleSelection(
		ctx context.Context,
		ownerUserID string,
		sceneID string,
		sceneVersion int,
		selectedRoleIDs []string,
		practiceOptionID string,
	) (SelectionSnapshot, error)
}

// Catalog is an immutable in-process collection for deterministic tests and
// local smoke composition. Production uses PostgresCatalog.
type Catalog struct {
	scenes            []SceneDefinition
	ieltsQuestionBank *IELTSQuestionBank
}

func NewCatalog(definitions []SceneDefinition) (*Catalog, error) {
	catalog, err := newValidatedCatalog(definitions)
	if err != nil {
		return nil, err
	}
	bank, err := loadEmbeddedIELTSQuestionBank()
	if err != nil {
		return nil, err
	}
	catalog.ieltsQuestionBank = &bank
	return catalog, nil
}

func newValidatedCatalog(definitions []SceneDefinition) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, invalidDefinition("catalog must contain at least one scene")
	}

	scenes := make([]SceneDefinition, 0, len(definitions))
	sceneIDs := make(map[string]struct{}, len(definitions))
	roleIDs := make(map[string]struct{})
	optionIDs := make(map[string]struct{})
	for _, source := range definitions {
		definition := cloneScene(source)
		if err := validateScene(definition, sceneIDs, roleIDs, optionIDs); err != nil {
			return nil, err
		}
		sceneIDs[definition.ID] = struct{}{}
		for _, role := range definition.Roles {
			roleIDs[role.ID] = struct{}{}
		}
		for _, option := range definition.PracticeOptions {
			optionIDs[option.ID] = struct{}{}
		}
		sortRoles(definition.Roles)
		sortPracticeOptions(definition.PracticeOptions)
		scenes = append(scenes, definition)
	}

	sort.Slice(scenes, func(i, j int) bool {
		if scenes[i].DisplayOrder == scenes[j].DisplayOrder {
			return scenes[i].ID < scenes[j].ID
		}
		return scenes[i].DisplayOrder < scenes[j].DisplayOrder
	})
	return &Catalog{scenes: scenes}, nil
}

func (c *Catalog) IELTSQuestionBank() (IELTSQuestionBank, error) {
	if c == nil || c.ieltsQuestionBank == nil {
		return IELTSQuestionBank{}, ErrIELTSQuestionBankUnavailable
	}
	return publishedIELTSQuestionBank(*c.ieltsQuestionBank), nil
}

func (c *Catalog) ResolveIELTSQuestionSet(
	selection IELTSQuestionSetSelection,
) (IELTSResolvedQuestionSet, error) {
	if c == nil || c.ieltsQuestionBank == nil {
		return IELTSResolvedQuestionSet{}, ErrIELTSQuestionBankUnavailable
	}
	return resolveIELTSQuestionSet(*c.ieltsQuestionBank, selection)
}

func (c *Catalog) ListActiveScenes(ctx context.Context) ([]SceneDefinition, error) {
	if err := catalogContextError(ctx); err != nil {
		return nil, err
	}
	result := make([]SceneDefinition, 0, len(c.scenes))
	for _, definition := range c.scenes {
		if definition.Status == SceneStatusActive {
			result = append(result, cloneScene(definition))
		}
	}
	return result, nil
}

func (c *Catalog) GetScene(
	ctx context.Context,
	sceneID string,
) (SceneDefinition, error) {
	if err := catalogContextError(ctx); err != nil {
		return SceneDefinition{}, err
	}
	definition, ok := c.scene(sceneID)
	if !ok || definition.Status != SceneStatusActive {
		return SceneDefinition{}, ErrSceneNotFound
	}
	return cloneScene(definition), nil
}

func (c *Catalog) ListRoles(
	ctx context.Context,
	sceneID string,
) ([]RoleDefinition, error) {
	definition, err := c.GetScene(ctx, sceneID)
	if err != nil {
		return nil, err
	}
	return cloneRoles(definition.Roles), nil
}

func (c *Catalog) ResolveSelection(
	ctx context.Context,
	sceneID string,
	sceneVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
) (SelectionSnapshot, error) {
	if err := catalogContextError(ctx); err != nil {
		return SelectionSnapshot{}, err
	}
	definition, ok := c.scene(sceneID)
	if !ok || definition.Status != SceneStatusActive ||
		definition.Version != sceneVersion {
		return SelectionSnapshot{}, ErrSceneNotFound
	}
	if len(selectedRoleIDs) == 0 {
		return SelectionSnapshot{}, ErrCatalogSelectionInvalid
	}

	seenRoles := make(map[string]struct{}, len(selectedRoleIDs))
	for _, roleID := range selectedRoleIDs {
		if _, duplicate := seenRoles[roleID]; duplicate {
			return SelectionSnapshot{}, ErrCatalogSelectionInvalid
		}
		seenRoles[roleID] = struct{}{}
		if _, found := findRole(definition.Roles, roleID); !found {
			return SelectionSnapshot{}, ErrRoleDefinitionNotFound
		}
	}

	option, found := findPracticeOption(definition.PracticeOptions, practiceOptionID)
	if !found {
		return SelectionSnapshot{}, ErrPracticeOptionNotFound
	}
	if option.Type == PracticeOptionFocus &&
		(len(selectedRoleIDs) != 1 || selectedRoleIDs[0] != option.RoleDefinitionID) {
		return SelectionSnapshot{}, ErrCatalogSelectionInvalid
	}

	return SelectionSnapshot{
		Scene:            cloneScene(definition),
		SelectedRoleIDs:  append([]string(nil), selectedRoleIDs...),
		PracticeOptionID: option.ID,
	}, nil
}

func (c *Catalog) ResolveAccessibleSelection(
	ctx context.Context,
	ownerUserID string,
	sceneID string,
	sceneVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
) (SelectionSnapshot, error) {
	if !validCatalogOwner(ownerUserID) {
		return SelectionSnapshot{}, ErrCatalogSelectionInvalid
	}
	return c.ResolveSelection(
		ctx,
		sceneID,
		sceneVersion,
		selectedRoleIDs,
		practiceOptionID,
	)
}

func validCatalogOwner(ownerUserID string) bool {
	return ownerUserID != "" &&
		strings.TrimSpace(ownerUserID) == ownerUserID &&
		!strings.ContainsRune(ownerUserID, '\x00')
}

func catalogContextError(ctx context.Context) error {
	if ctx == nil {
		return ErrCatalogContextRequired
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrCatalogReadFailed, err)
	}
	return nil
}

func (c *Catalog) scene(id string) (SceneDefinition, bool) {
	if c == nil {
		return SceneDefinition{}, false
	}
	for _, definition := range c.scenes {
		if definition.ID == id {
			return definition, true
		}
	}
	return SceneDefinition{}, false
}

func validateScene(
	definition SceneDefinition,
	sceneIDs map[string]struct{},
	globalRoleIDs map[string]struct{},
	globalOptionIDs map[string]struct{},
) error {
	if !validResourceID(definition.ID) {
		return invalidDefinition("scene_id is invalid")
	}
	if _, duplicate := sceneIDs[definition.ID]; duplicate {
		return invalidDefinition("scene_id %q is duplicated", definition.ID)
	}
	if !validSceneFamilyModel(definition.Family, definition.Model) {
		return invalidDefinition(
			"scene %q has invalid family/model %q/%q",
			definition.ID,
			definition.Family,
			definition.Model,
		)
	}
	if definition.Status != SceneStatusActive && definition.Status != SceneStatusInactive {
		return invalidDefinition("scene %q has invalid status %q", definition.ID, definition.Status)
	}
	if definition.Version < 1 || !nonBlank(definition.Name) || definition.DisplayOrder < 0 {
		return invalidDefinition("scene %q has invalid public fields", definition.ID)
	}
	if !validPolicyRef(definition.TurnPolicyRef, ".turn.v1") ||
		!validPolicyRef(definition.SessionPolicyRef, ".session.v1") {
		return invalidDefinition("scene %q has invalid policy refs", definition.ID)
	}
	if !validScenePrompt(definition.Prompt) {
		return invalidDefinition("scene %q has invalid prompt", definition.ID)
	}
	if len(definition.Roles) == 0 || len(definition.PracticeOptions) == 0 {
		return invalidDefinition("scene %q must contain roles and practice options", definition.ID)
	}

	localRoles := make(map[string]struct{}, len(definition.Roles))
	objectiveDescriptions := make(map[string]string)
	for _, role := range definition.Roles {
		if !validResourceID(role.ID) || role.SceneID != definition.ID ||
			!roleTypePattern.MatchString(role.Type) || !nonBlank(role.DisplayName) ||
			!nonBlank(role.Responsibilities) || !nonBlank(role.Style) ||
			!validPracticeObjectiveDefinitions(role.PracticeObjectives) ||
			role.DisplayOrder < 0 {
			return invalidDefinition("scene %q has invalid role %q", definition.ID, role.ID)
		}
		if _, duplicate := localRoles[role.ID]; duplicate {
			return invalidDefinition("role_definition_id %q is duplicated", role.ID)
		}
		if _, duplicate := globalRoleIDs[role.ID]; duplicate {
			return invalidDefinition("role_definition_id %q is not globally unique", role.ID)
		}
		for _, objective := range role.PracticeObjectives {
			if description, exists := objectiveDescriptions[objective.ID]; exists && description != objective.Description {
				return invalidDefinition(
					"scene %q has conflicting descriptions for objective %q",
					definition.ID,
					objective.ID,
				)
			}
			objectiveDescriptions[objective.ID] = objective.Description
		}
		localRoles[role.ID] = struct{}{}
	}

	focusCount := make(map[string]int, len(definition.Roles))
	fullSimulationCount := 0
	localOptions := make(map[string]struct{}, len(definition.PracticeOptions))
	for _, option := range definition.PracticeOptions {
		if !validResourceID(option.ID) || option.SceneID != definition.ID ||
			!nonBlank(option.DisplayName) || option.DisplayOrder < 0 {
			return invalidDefinition("scene %q has invalid practice option %q", definition.ID, option.ID)
		}
		if _, duplicate := localOptions[option.ID]; duplicate {
			return invalidDefinition("practice_option_id %q is duplicated", option.ID)
		}
		if _, duplicate := globalOptionIDs[option.ID]; duplicate {
			return invalidDefinition("practice_option_id %q is not globally unique", option.ID)
		}
		localOptions[option.ID] = struct{}{}
		switch option.Type {
		case PracticeOptionFullSimulation:
			if option.RoleDefinitionID != "" {
				return invalidDefinition("FULL_SIMULATION option %q must not reference a role", option.ID)
			}
			fullSimulationCount++
		case PracticeOptionFocus:
			if _, exists := localRoles[option.RoleDefinitionID]; !exists {
				return invalidDefinition("FOCUS option %q must reference a role in its scene", option.ID)
			}
			focusCount[option.RoleDefinitionID]++
		default:
			return invalidDefinition("practice option %q has unsupported type %q", option.ID, option.Type)
		}
	}
	if fullSimulationCount != 1 {
		return invalidDefinition("scene %q must contain one FULL_SIMULATION option", definition.ID)
	}
	for roleID := range localRoles {
		if focusCount[roleID] != 1 {
			return invalidDefinition("role %q must have exactly one FOCUS option", roleID)
		}
	}
	return nil
}

func validResourceID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value
}

func nonBlank(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func validPolicyRef(value, suffix string) bool {
	return len(value) <= 128 && nonBlank(value) && strings.HasSuffix(value, suffix)
}

func validStringSet(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !nonBlank(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validPracticeObjectiveDefinitions(
	objectives []PracticeObjectiveDefinition,
) bool {
	if len(objectives) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(objectives))
	for _, objective := range objectives {
		if !objectiveIDPattern.MatchString(objective.ID) ||
			!nonBlank(objective.Description) {
			return false
		}
		if _, duplicate := seen[objective.ID]; duplicate {
			return false
		}
		seen[objective.ID] = struct{}{}
	}
	return true
}

func validSceneFamilyModel(family SceneFamily, model SceneModel) bool {
	switch family {
	case SceneFamilyInterview:
		return model == SceneModelProjectExperienceDeepDive || model == SceneModelInterviewBasicDialogue
	case SceneFamilyExam:
		return model == SceneModelIELTSSpeakingPart1 || model == SceneModelIELTSSpeakingPart2 ||
			model == SceneModelIELTSSpeakingPart3 || model == SceneModelIELTSSpeakingFullMock ||
			model == SceneModelExamBasicDialogue
	case SceneFamilyWorkplace:
		return model == SceneModelProgressAndRiskUpdate || model == SceneModelWorkplaceBasicDialogue
	case SceneFamilyDaily:
		return model == SceneModelHotelCheckinAndIssueHandling || model == SceneModelDailyBasicDialogue
	default:
		return false
	}
}

func validScenePrompt(prompt ScenePrompt) bool {
	return nonBlank(prompt.PublicSceneBrief) && nonBlank(prompt.PracticeGoal) &&
		nonBlank(prompt.UserRole) && nonBlank(prompt.AIRole) &&
		nonBlank(prompt.PersonaSummary) && validStringSet(prompt.FocusAreas) &&
		validStringSet(prompt.TurnBlueprints) && prompt.SuggestedDurationSeconds > 0
}

func invalidDefinition(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCatalogDefinitionInvalid, fmt.Sprintf(format, args...))
}

func findRole(roles []RoleDefinition, id string) (RoleDefinition, bool) {
	for _, role := range roles {
		if role.ID == id {
			return role, true
		}
	}
	return RoleDefinition{}, false
}

func findPracticeOption(options []PracticeOption, id string) (PracticeOption, bool) {
	for _, option := range options {
		if option.ID == id {
			return option, true
		}
	}
	return PracticeOption{}, false
}

func sortRoles(roles []RoleDefinition) {
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].DisplayOrder == roles[j].DisplayOrder {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].DisplayOrder < roles[j].DisplayOrder
	})
}

func sortPracticeOptions(options []PracticeOption) {
	sort.Slice(options, func(i, j int) bool {
		if options[i].DisplayOrder == options[j].DisplayOrder {
			return options[i].ID < options[j].ID
		}
		return options[i].DisplayOrder < options[j].DisplayOrder
	})
}

func cloneScene(source SceneDefinition) SceneDefinition {
	result := source
	result.Prompt.FocusAreas = append([]string(nil), source.Prompt.FocusAreas...)
	result.Prompt.TurnBlueprints = append([]string(nil), source.Prompt.TurnBlueprints...)
	result.Roles = cloneRoles(source.Roles)
	result.PracticeOptions = clonePracticeOptions(source.PracticeOptions)
	return result
}

func cloneRoles(source []RoleDefinition) []RoleDefinition {
	result := make([]RoleDefinition, len(source))
	for index, role := range source {
		result[index] = cloneRole(role)
	}
	return result
}

func cloneRole(source RoleDefinition) RoleDefinition {
	result := source
	result.PracticeObjectives = append(
		[]PracticeObjectiveDefinition(nil),
		source.PracticeObjectives...,
	)
	return result
}

func clonePracticeOptions(source []PracticeOption) []PracticeOption {
	return append([]PracticeOption(nil), source...)
}
