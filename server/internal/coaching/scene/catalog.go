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
	resourceIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	policyReferencePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	roleTypePattern        = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	objectiveIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
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

// EvaluationPolicyReferenceValidator is provided by Evaluation at composition
// time so Scene can reject definitions that cannot be scored without owning
// Evaluation's reference-to-pipeline registry.
type EvaluationPolicyReferenceValidator interface {
	ValidateEvaluationPolicyReference(string) error
}

// EvaluationPolicyReferenceValidatorFunc adapts composition-local validators
// to Scene's policy-reference port.
type EvaluationPolicyReferenceValidatorFunc func(string) error

func (validate EvaluationPolicyReferenceValidatorFunc) ValidateEvaluationPolicyReference(
	reference string,
) error {
	return validate(reference)
}

// AccessibleSelectionReader resolves an active built-in Scene for an
// authenticated user. The user identifier remains part of the application
// boundary even though built-in Scene content is public.
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

// Catalog is the immutable in-process authority for built-in Scene content.
type Catalog struct {
	scenes              []SceneDefinition
	sceneDiscovery      map[string]SceneDiscoveryProfile
	experienceDiscovery map[PracticeExperience]ExperienceDiscoveryProfile
}

func NewCatalog(
	definitions []SceneDefinition,
	policyValidator EvaluationPolicyReferenceValidator,
) (*Catalog, error) {
	return newValidatedCatalog(definitions, policyValidator)
}

func newValidatedCatalog(
	definitions []SceneDefinition,
	policyValidator EvaluationPolicyReferenceValidator,
) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, invalidDefinition("catalog must contain at least one scene")
	}
	if policyValidator == nil {
		return nil, invalidDefinition(
			"evaluation policy validator is required",
		)
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
		for _, option := range definition.PracticeOptions {
			if err := policyValidator.ValidateEvaluationPolicyReference(
				option.EvaluationPolicyRef,
			); err != nil {
				return nil, invalidDefinition(
					"practice option %q evaluation policy reference is not registered and enabled",
					option.ID,
				)
			}
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
	discovery := make(map[string]SceneDiscoveryProfile, len(scenes))
	for _, definition := range scenes {
		discovery[definition.ID] = SceneDiscoveryProfile{
			SceneID: definition.ID,
			Aliases: []string{definition.Name},
		}
	}
	return &Catalog{
		scenes:              scenes,
		sceneDiscovery:      discovery,
		experienceDiscovery: map[PracticeExperience]ExperienceDiscoveryProfile{},
	}, nil
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
	if option.Mode == PracticeModeFocus &&
		(len(selectedRoleIDs) != 1 || selectedRoleIDs[0] != option.RoleDefinitionID) {
		return SelectionSnapshot{}, ErrCatalogSelectionInvalid
	}

	return SelectionSnapshot{
		Source: SceneSource{
			Type:         SceneSourceCatalog,
			SceneID:      definition.ID,
			SceneVersion: definition.Version,
		},
		Scene:            executableSceneSnapshot(definition),
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
	if !validExperienceCategory(definition.Experience, definition.Category) {
		return invalidDefinition(
			"scene %q has invalid experience/category %q/%q",
			definition.ID,
			definition.Experience,
			definition.Category,
		)
	}
	if definition.Status != SceneStatusActive && definition.Status != SceneStatusInactive {
		return invalidDefinition("scene %q has invalid status %q", definition.ID, definition.Status)
	}
	if definition.Version < 1 || !nonBlank(definition.Name) || definition.DisplayOrder < 0 {
		return invalidDefinition("scene %q has invalid public fields", definition.ID)
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
	modeCount := make(map[PracticeMode]int, len(definition.PracticeOptions))
	localOptions := make(map[string]struct{}, len(definition.PracticeOptions))
	for _, option := range definition.PracticeOptions {
		if !validResourceID(option.ID) || option.SceneID != definition.ID ||
			!nonBlank(option.DisplayName) || option.DisplayOrder < 0 ||
			option.SuggestedDurationSeconds < 1 ||
			!validPolicyRef(option.TurnPolicyRef, ".turn.v1") ||
			!validPolicyRef(option.SessionPolicyRef, ".session.v1") ||
			!validPolicyRef(option.EvaluationPolicyRef, ".evaluation.v1") {
			return invalidDefinition("scene %q has invalid practice option %q", definition.ID, option.ID)
		}
		if _, duplicate := localOptions[option.ID]; duplicate {
			return invalidDefinition("practice_option_id %q is duplicated", option.ID)
		}
		if _, duplicate := globalOptionIDs[option.ID]; duplicate {
			return invalidDefinition("practice_option_id %q is not globally unique", option.ID)
		}
		localOptions[option.ID] = struct{}{}
		modeCount[option.Mode]++
		switch option.Mode {
		case PracticeModeFullSimulation:
			if option.RoleDefinitionID != "" {
				return invalidDefinition("FULL_SIMULATION option %q must not reference a role", option.ID)
			}
		case PracticeModeFocus:
			if _, exists := localRoles[option.RoleDefinitionID]; !exists {
				return invalidDefinition("FOCUS option %q must reference a role in its scene", option.ID)
			}
			focusCount[option.RoleDefinitionID]++
		case PracticeModeFullMock, PracticeModePart1, PracticeModePart2, PracticeModePart3:
			if option.RoleDefinitionID != "" {
				return invalidDefinition("IELTS option %q must not reference a role", option.ID)
			}
		default:
			return invalidDefinition("practice option %q has unsupported mode %q", option.ID, option.Mode)
		}
	}
	if definition.Experience == PracticeExperienceIELTSSpeaking {
		for _, mode := range []PracticeMode{
			PracticeModeFullMock,
			PracticeModePart1,
			PracticeModePart2,
			PracticeModePart3,
		} {
			if modeCount[mode] != 1 {
				return invalidDefinition("IELTS Scene %q must contain exactly one %s option", definition.ID, mode)
			}
		}
		if len(definition.PracticeOptions) != 4 {
			return invalidDefinition("IELTS Scene %q must contain exactly four options", definition.ID)
		}
	} else {
		if modeCount[PracticeModeFullSimulation] != 1 {
			return invalidDefinition("scene %q must contain one FULL_SIMULATION option", definition.ID)
		}
		if len(definition.PracticeOptions) != len(definition.Roles)+1 {
			return invalidDefinition("scene %q has unsupported practice modes", definition.ID)
		}
		for roleID := range localRoles {
			if focusCount[roleID] != 1 {
				return invalidDefinition("role %q must have exactly one FOCUS option", roleID)
			}
		}
	}
	return nil
}

func validResourceID(value string) bool {
	return resourceIDPattern.MatchString(value)
}

func nonBlank(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func validPolicyRef(value, suffix string) bool {
	return len(value) <= 128 && policyReferencePattern.MatchString(value) &&
		strings.HasSuffix(value, suffix)
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

func validExperienceCategory(
	experience PracticeExperience,
	category SceneCategory,
) bool {
	switch experience {
	case PracticeExperienceInterview:
		switch category {
		case SceneCategoryInterviewRecruiter,
			SceneCategoryInterviewBehavioral,
			SceneCategoryInterviewProfessional,
			SceneCategoryInterviewHiringManager,
			SceneCategoryInterviewCustom:
			return true
		}
	case PracticeExperienceIELTSSpeaking:
		return category == SceneCategoryIELTSSpeaking
	case PracticeExperienceWorkplace:
		return category == SceneCategoryWorkplaceGeneral
	case PracticeExperienceLifeAndTravel:
		return category == SceneCategoryLifeTravel ||
			category == SceneCategoryLifeDaily
	default:
		return false
	}
	return false
}

func validScenePrompt(prompt ScenePrompt) bool {
	return nonBlank(prompt.PublicSceneBrief) && nonBlank(prompt.PracticeGoal) &&
		nonBlank(prompt.UserRole) && nonBlank(prompt.AIRole) &&
		nonBlank(prompt.PersonaSummary) && validStringSet(prompt.FocusAreas) &&
		validStringSet(prompt.TurnBlueprints)
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

func findRoleSnapshot(roles []RoleSnapshot, id string) (RoleSnapshot, bool) {
	for _, role := range roles {
		if role.ID == id {
			return role, true
		}
	}
	return RoleSnapshot{}, false
}

func findPracticeOptionSnapshot(
	options []PracticeOptionSnapshot,
	id string,
) (PracticeOptionSnapshot, bool) {
	for _, option := range options {
		if option.ID == id {
			return option, true
		}
	}
	return PracticeOptionSnapshot{}, false
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

func executableSceneSnapshot(source SceneDefinition) ExecutableSceneSnapshot {
	roles := make([]RoleSnapshot, len(source.Roles))
	for index, role := range source.Roles {
		roles[index] = RoleSnapshot{
			ID:                 role.ID,
			SceneKey:           source.ID,
			Type:               role.Type,
			DisplayName:        role.DisplayName,
			Responsibilities:   role.Responsibilities,
			Style:              role.Style,
			PracticeObjectives: append([]PracticeObjectiveDefinition(nil), role.PracticeObjectives...),
			VoiceConfigRef:     role.VoiceConfigRef,
		}
	}
	options := make([]PracticeOptionSnapshot, len(source.PracticeOptions))
	for index, option := range source.PracticeOptions {
		options[index] = PracticeOptionSnapshot{
			ID:                       option.ID,
			SceneKey:                 source.ID,
			RoleDefinitionID:         option.RoleDefinitionID,
			Mode:                     option.Mode,
			DisplayName:              option.DisplayName,
			SuggestedDurationSeconds: option.SuggestedDurationSeconds,
			TurnPolicyRef:            option.TurnPolicyRef,
			SessionPolicyRef:         option.SessionPolicyRef,
			EvaluationPolicyRef:      option.EvaluationPolicyRef,
		}
	}
	result := ExecutableSceneSnapshot{
		Key:             source.ID,
		Revision:        source.Version,
		Experience:      source.Experience,
		Category:        source.Category,
		Name:            source.Name,
		Prompt:          source.Prompt,
		Roles:           roles,
		PracticeOptions: options,
	}
	result.Prompt.FocusAreas = append([]string(nil), source.Prompt.FocusAreas...)
	result.Prompt.TurnBlueprints = append([]string(nil), source.Prompt.TurnBlueprints...)
	return result
}

func cloneRoleSnapshot(source RoleSnapshot) RoleSnapshot {
	result := source
	result.PracticeObjectives = append(
		[]PracticeObjectiveDefinition(nil),
		source.PracticeObjectives...,
	)
	return result
}

func cloneExecutableSceneSnapshot(
	source ExecutableSceneSnapshot,
) ExecutableSceneSnapshot {
	result := source
	result.Prompt.FocusAreas = append([]string(nil), source.Prompt.FocusAreas...)
	result.Prompt.TurnBlueprints = append([]string(nil), source.Prompt.TurnBlueprints...)
	result.Roles = make([]RoleSnapshot, len(source.Roles))
	for index, role := range source.Roles {
		result.Roles[index] = cloneRoleSnapshot(role)
	}
	result.PracticeOptions = append(
		[]PracticeOptionSnapshot(nil),
		source.PracticeOptions...,
	)
	return result
}
