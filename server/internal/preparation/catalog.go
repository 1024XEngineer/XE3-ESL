package preparation

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrScenarioDefinitionNotFound = errors.New("scenario definition not found")
	ErrRoleDefinitionNotFound     = errors.New("role definition not found")
	ErrPracticeOptionNotFound     = errors.New("practice option definition not found")
	ErrCatalogSelectionInvalid    = errors.New("catalog selection is invalid")
	ErrCatalogDefinitionInvalid   = errors.New("catalog definition is invalid")
)

var roleTypePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// CatalogReader is Preparation's narrow, read-only boundary for HTTP delivery
// and future Practice adapters.
type CatalogReader interface {
	ListActiveScenarios() []ScenarioDefinition
	GetScenarioDetail(scenarioDefinitionID string) (ScenarioDetail, error)
	ListRoles(scenarioDefinitionID string) ([]RoleDefinition, error)
	GetCatalogSnapshot(
		scenarioDefinitionID string,
		scenarioVersion int,
		selectedRoleIDs []string,
		practiceOptionID string,
		practiceOptionVersion int,
	) (CatalogSnapshot, error)
}

type catalogScenario struct {
	definition      ScenarioDefinition
	config          ScenarioConfig
	roles           []RoleDefinition
	practiceOptions []PracticeOptionDefinition
}

// Catalog is an immutable in-process collection of versioned definitions.
type Catalog struct {
	scenarios []catalogScenario
}

func newCatalog(definitions []catalogScenario) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, invalidDefinition("catalog must contain at least one scenario")
	}

	scenarios := make([]catalogScenario, 0, len(definitions))
	scenarioIDs := make(map[string]struct{}, len(definitions))
	configIDs := make(map[string]struct{}, len(definitions))
	roleIDs := make(map[string]struct{})
	optionIDs := make(map[string]struct{})

	for _, source := range definitions {
		scenario := cloneCatalogScenario(source)
		if err := validateCatalogScenario(
			scenario,
			scenarioIDs,
			configIDs,
			roleIDs,
			optionIDs,
		); err != nil {
			return nil, err
		}
		scenarioIDs[scenario.definition.ID] = struct{}{}
		configIDs[scenario.config.ID] = struct{}{}
		for _, role := range scenario.roles {
			roleIDs[role.ID] = struct{}{}
		}
		for _, option := range scenario.practiceOptions {
			optionIDs[option.ID] = struct{}{}
		}
		sortRoles(scenario.roles)
		sortPracticeOptions(scenario.practiceOptions)
		scenarios = append(scenarios, scenario)
	}

	sort.Slice(scenarios, func(i, j int) bool {
		left := scenarios[i].definition
		right := scenarios[j].definition
		if left.DisplayOrder == right.DisplayOrder {
			return left.ID < right.ID
		}
		return left.DisplayOrder < right.DisplayOrder
	})
	return &Catalog{scenarios: scenarios}, nil
}

// ListActiveScenarios returns independent copies in stable display order.
func (c *Catalog) ListActiveScenarios() []ScenarioDefinition {
	result := make([]ScenarioDefinition, 0, len(c.scenarios))
	for _, scenario := range c.scenarios {
		if scenario.definition.Status == ScenarioStatusActive {
			result = append(result, scenario.definition)
		}
	}
	return result
}

// GetScenarioDetail returns the current active definition and copies of its
// config and options.
func (c *Catalog) GetScenarioDetail(scenarioDefinitionID string) (ScenarioDetail, error) {
	scenario, ok := c.activeScenario(scenarioDefinitionID)
	if !ok {
		return ScenarioDetail{}, ErrScenarioDefinitionNotFound
	}
	return ScenarioDetail{
		ScenarioDefinition: scenario.definition,
		ScenarioConfig:     cloneScenarioConfig(scenario.config),
		PracticeOptions:    clonePracticeOptions(scenario.practiceOptions),
	}, nil
}

// ListRoles returns copies in stable display order.
func (c *Catalog) ListRoles(scenarioDefinitionID string) ([]RoleDefinition, error) {
	scenario, ok := c.activeScenario(scenarioDefinitionID)
	if !ok {
		return nil, ErrScenarioDefinitionNotFound
	}
	return cloneRoles(scenario.roles), nil
}

// GetCatalogSnapshot validates an exact, versioned selection. It never falls
// back to the latest version and never exposes mutable catalog slices.
func (c *Catalog) GetCatalogSnapshot(
	scenarioDefinitionID string,
	scenarioVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
	practiceOptionVersion int,
) (CatalogSnapshot, error) {
	scenario, ok := c.activeScenario(scenarioDefinitionID)
	if !ok || scenario.definition.Version != scenarioVersion {
		return CatalogSnapshot{}, ErrScenarioDefinitionNotFound
	}

	if len(selectedRoleIDs) == 0 {
		return CatalogSnapshot{}, ErrCatalogSelectionInvalid
	}
	selectedRoles := make([]RoleDefinition, 0, len(selectedRoleIDs))
	seenRoles := make(map[string]struct{}, len(selectedRoleIDs))
	for _, roleID := range selectedRoleIDs {
		if _, duplicate := seenRoles[roleID]; duplicate {
			return CatalogSnapshot{}, ErrCatalogSelectionInvalid
		}
		seenRoles[roleID] = struct{}{}

		role, found := findRole(scenario.roles, roleID)
		if !found {
			return CatalogSnapshot{}, ErrRoleDefinitionNotFound
		}
		selectedRoles = append(selectedRoles, cloneRole(role))
	}

	option, found := findPracticeOption(scenario.practiceOptions, practiceOptionID)
	if !found || option.Version != practiceOptionVersion {
		return CatalogSnapshot{}, ErrPracticeOptionNotFound
	}
	if option.Type == PracticeOptionFocus &&
		(len(selectedRoles) != 1 || selectedRoles[0].ID != option.RoleDefinitionID) {
		return CatalogSnapshot{}, ErrCatalogSelectionInvalid
	}

	return CatalogSnapshot{
		ScenarioDefinition: scenario.definition,
		ScenarioConfig:     cloneScenarioConfig(scenario.config),
		SelectedRoles:      selectedRoles,
		PracticeOption:     option,
	}, nil
}

func (c *Catalog) activeScenario(id string) (catalogScenario, bool) {
	for _, scenario := range c.scenarios {
		if scenario.definition.ID == id &&
			scenario.definition.Status == ScenarioStatusActive {
			return scenario, true
		}
	}
	return catalogScenario{}, false
}

func validateCatalogScenario(
	scenario catalogScenario,
	scenarioIDs map[string]struct{},
	globalConfigIDs map[string]struct{},
	globalRoleIDs map[string]struct{},
	globalOptionIDs map[string]struct{},
) error {
	definition := scenario.definition
	if !validResourceID(definition.ID) {
		return invalidDefinition("scenario_definition_id is invalid")
	}
	if _, duplicate := scenarioIDs[definition.ID]; duplicate {
		return invalidDefinition("scenario_definition_id %q is duplicated", definition.ID)
	}
	if definition.Type != ScenarioTypeInterview {
		return invalidDefinition("scenario %q has unsupported type %q", definition.ID, definition.Type)
	}
	if definition.Status != ScenarioStatusActive && definition.Status != ScenarioStatusInactive {
		return invalidDefinition("scenario %q has invalid status %q", definition.ID, definition.Status)
	}
	if definition.Version < 1 || !nonBlank(definition.Name) || definition.DisplayOrder < 0 {
		return invalidDefinition("scenario %q has invalid public fields", definition.ID)
	}

	config := scenario.config
	if !validResourceID(config.ID) ||
		config.ScenarioDefinitionID != definition.ID ||
		config.Type != definition.Type ||
		config.Version < 1 ||
		!nonBlank(config.JobTitle) ||
		!nonBlank(config.JobDescription) ||
		!validStringSet(config.FocusAreas) {
		return invalidDefinition("scenario %q has invalid config", definition.ID)
	}
	if _, duplicate := globalConfigIDs[config.ID]; duplicate {
		return invalidDefinition("scenario_config_id %q is not globally unique", config.ID)
	}
	if len(scenario.roles) == 0 {
		return invalidDefinition("scenario %q must contain roles", definition.ID)
	}
	if len(scenario.practiceOptions) == 0 {
		return invalidDefinition("scenario %q must contain practice options", definition.ID)
	}

	localRoles := make(map[string]struct{}, len(scenario.roles))
	for _, role := range scenario.roles {
		if !validResourceID(role.ID) ||
			role.ScenarioDefinitionID != definition.ID ||
			!roleTypePattern.MatchString(role.Type) ||
			!nonBlank(role.DisplayName) ||
			!nonBlank(role.Responsibilities) ||
			!nonBlank(role.Style) ||
			!validStringSet(role.FocusAreas) ||
			role.Version < 1 ||
			role.DisplayOrder < 0 {
			return invalidDefinition("scenario %q has invalid role %q", definition.ID, role.ID)
		}
		if _, duplicate := localRoles[role.ID]; duplicate {
			return invalidDefinition("role_definition_id %q is duplicated", role.ID)
		}
		if _, duplicate := globalRoleIDs[role.ID]; duplicate {
			return invalidDefinition("role_definition_id %q is not globally unique", role.ID)
		}
		localRoles[role.ID] = struct{}{}
	}

	focusCount := make(map[string]int, len(scenario.roles))
	fullSimulationCount := 0
	localOptions := make(map[string]struct{}, len(scenario.practiceOptions))
	for _, option := range scenario.practiceOptions {
		if !validResourceID(option.ID) ||
			option.ScenarioDefinitionID != definition.ID ||
			!nonBlank(option.DisplayName) ||
			option.Version < 1 ||
			option.DisplayOrder < 0 {
			return invalidDefinition("scenario %q has invalid practice option %q", definition.ID, option.ID)
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
				return invalidDefinition("FOCUS option %q must reference a role in its scenario", option.ID)
			}
			focusCount[option.RoleDefinitionID]++
		default:
			return invalidDefinition("practice option %q has unsupported type %q", option.ID, option.Type)
		}
	}
	if fullSimulationCount == 0 {
		return invalidDefinition("scenario %q must contain a FULL_SIMULATION option", definition.ID)
	}
	for roleID := range localRoles {
		if focusCount[roleID] != 1 {
			return invalidDefinition("role %q must have exactly one FOCUS option", roleID)
		}
	}
	return nil
}

func validResourceID(value string) bool {
	return value != "" &&
		len(value) <= 128 &&
		strings.TrimSpace(value) == value
}

func nonBlank(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
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

func findPracticeOption(
	options []PracticeOptionDefinition,
	id string,
) (PracticeOptionDefinition, bool) {
	for _, option := range options {
		if option.ID == id {
			return option, true
		}
	}
	return PracticeOptionDefinition{}, false
}

func sortRoles(roles []RoleDefinition) {
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].DisplayOrder == roles[j].DisplayOrder {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].DisplayOrder < roles[j].DisplayOrder
	})
}

func sortPracticeOptions(options []PracticeOptionDefinition) {
	sort.Slice(options, func(i, j int) bool {
		if options[i].DisplayOrder == options[j].DisplayOrder {
			return options[i].ID < options[j].ID
		}
		return options[i].DisplayOrder < options[j].DisplayOrder
	})
}

func cloneCatalogScenario(source catalogScenario) catalogScenario {
	return catalogScenario{
		definition:      source.definition,
		config:          cloneScenarioConfig(source.config),
		roles:           cloneRoles(source.roles),
		practiceOptions: clonePracticeOptions(source.practiceOptions),
	}
}

func cloneScenarioConfig(source ScenarioConfig) ScenarioConfig {
	result := source
	result.FocusAreas = append([]string(nil), source.FocusAreas...)
	return result
}

func cloneRole(source RoleDefinition) RoleDefinition {
	result := source
	result.FocusAreas = append([]string(nil), source.FocusAreas...)
	return result
}

func cloneRoles(source []RoleDefinition) []RoleDefinition {
	result := make([]RoleDefinition, len(source))
	for index, role := range source {
		result[index] = cloneRole(role)
	}
	return result
}

func clonePracticeOptions(
	source []PracticeOptionDefinition,
) []PracticeOptionDefinition {
	return append([]PracticeOptionDefinition(nil), source...)
}
