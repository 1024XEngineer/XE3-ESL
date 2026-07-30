package preparation

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxPreviewCatalogCandidates = 5

type PreviewCatalogCandidate struct {
	ScenarioDefinition ScenarioDefinition
	ScenarioConfig     ScenarioConfig
	DefaultRoleIDs     []string
	DefaultOption      PracticeOptionDefinition
}

type PreviewCatalogResolver interface {
	ResolvePreviewCatalog(query string) ([]PreviewCatalogCandidate, error)
}

type CatalogPreviewResolver struct {
	catalog CatalogReader
}

func NewCatalogPreviewResolver(
	catalog CatalogReader,
) (*CatalogPreviewResolver, error) {
	if catalog == nil {
		return nil, errors.New("preparation: catalog is required")
	}
	return &CatalogPreviewResolver{catalog: catalog}, nil
}

func (resolver *CatalogPreviewResolver) ResolvePreviewCatalog(
	query string,
) ([]PreviewCatalogCandidate, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if resolver == nil || resolver.catalog == nil || query == "" ||
		!utf8.ValidString(query) || utf8.RuneCountInString(query) > 500 {
		return nil, ErrCatalogSelectionInvalid
	}

	type scoredCandidate struct {
		candidate PreviewCatalogCandidate
		score     int
	}
	scored := make([]scoredCandidate, 0)
	for _, definition := range resolver.catalog.ListActiveScenarios() {
		detail, err := resolver.catalog.GetScenarioDetail(definition.ID)
		if err != nil {
			return nil, err
		}
		roles, err := resolver.catalog.ListRoles(definition.ID)
		if err != nil {
			return nil, err
		}
		score := previewCatalogScore(query, detail, roles)
		if score == 0 {
			continue
		}
		defaultOption, found := fullSimulationOption(detail.PracticeOptions)
		if !found || len(roles) == 0 {
			continue
		}
		candidate := PreviewCatalogCandidate{
			ScenarioDefinition: detail.ScenarioDefinition,
			ScenarioConfig:     detail.ScenarioConfig,
			DefaultRoleIDs:     []string{roles[0].ID},
			DefaultOption:      defaultOption,
		}
		if score == 100 {
			return []PreviewCatalogCandidate{candidate}, nil
		}
		scored = append(scored, scoredCandidate{
			candidate: candidate,
			score:     score,
		})
	}
	sort.Slice(scored, func(left, right int) bool {
		if scored[left].score == scored[right].score {
			return scored[left].candidate.ScenarioDefinition.ID <
				scored[right].candidate.ScenarioDefinition.ID
		}
		return scored[left].score > scored[right].score
	})
	if len(scored) > MaxPreviewCatalogCandidates {
		scored = scored[:MaxPreviewCatalogCandidates]
	}
	result := make([]PreviewCatalogCandidate, len(scored))
	for index := range scored {
		result[index] = scored[index].candidate
	}
	return result, nil
}

func previewCatalogScore(
	query string,
	detail ScenarioDetail,
	roles []RoleDefinition,
) int {
	definition := detail.ScenarioDefinition
	if query == strings.ToLower(definition.ID) {
		return 100
	}
	if query == strings.ToLower(definition.Name) {
		return 90
	}
	fields := []string{
		definition.Name,
		string(definition.Type),
		string(definition.Model),
		detail.ScenarioConfig.PromptModel.PublicSceneBrief,
		detail.ScenarioConfig.PromptModel.PracticeGoal,
		detail.ScenarioConfig.PromptModel.UserRole,
		detail.ScenarioConfig.PromptModel.AIRole,
	}
	for _, role := range roles {
		fields = append(fields, role.DisplayName, role.Type)
	}
	score := 0
	for _, field := range fields {
		normalized := strings.ToLower(strings.TrimSpace(field))
		if normalized != "" &&
			(strings.Contains(normalized, query) ||
				strings.Contains(query, normalized)) {
			score++
		}
	}
	return score
}

func fullSimulationOption(
	options []PracticeOptionDefinition,
) (PracticeOptionDefinition, bool) {
	for _, option := range options {
		if option.Type == PracticeOptionFullSimulation {
			return option, true
		}
	}
	return PracticeOptionDefinition{}, false
}
