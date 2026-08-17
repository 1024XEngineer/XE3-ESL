package scene

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxPreviewCatalogCandidates = 5

type PreviewCatalogCandidate struct {
	Scene          SceneDefinition
	DefaultRoleIDs []string
	DefaultOption  PracticeOption
}

type PreviewCatalogResolver interface {
	ResolvePreviewCatalog(
		context.Context,
		string,
	) ([]PreviewCatalogCandidate, error)
}

type CatalogPreviewResolver struct {
	catalog *Catalog
}

// ResolvePreviewCatalog exposes discovery as a first-class Catalog port so
// application composition does not need to recover a concrete Catalog from a
// narrower interface.
func (catalog *Catalog) ResolvePreviewCatalog(
	ctx context.Context,
	query string,
) ([]PreviewCatalogCandidate, error) {
	return (&CatalogPreviewResolver{catalog: catalog}).ResolvePreviewCatalog(ctx, query)
}

func NewCatalogPreviewResolver(
	catalog *Catalog,
) (*CatalogPreviewResolver, error) {
	if catalog == nil {
		return nil, errors.New("scene: catalog is required")
	}
	return &CatalogPreviewResolver{catalog: catalog}, nil
}

func (resolver *CatalogPreviewResolver) ResolvePreviewCatalog(
	ctx context.Context,
	query string,
) ([]PreviewCatalogCandidate, error) {
	query = normalizeDiscoveryText(query)
	if resolver == nil || resolver.catalog == nil || query == "" ||
		!utf8.ValidString(query) || utf8.RuneCountInString(query) > 500 {
		return nil, ErrCatalogSelectionInvalid
	}
	definitions, err := resolver.catalog.ListActiveScenes(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]SceneDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
		if query == normalizeDiscoveryText(definition.ID) {
			candidate, ok := catalogPreviewCandidate(definition, "")
			if !ok {
				return nil, ErrCatalogDefinitionInvalid
			}
			return []PreviewCatalogCandidate{candidate}, nil
		}
	}

	type matchedCandidate struct {
		candidate PreviewCatalogCandidate
		score     int
	}
	matches := make(map[string]matchedCandidate)
	hasExactSceneMatch := false
	for _, definition := range definitions {
		profile, found := resolver.catalog.sceneDiscovery[definition.ID]
		if !found {
			return nil, ErrCatalogDefinitionInvalid
		}
		phrases := append([]string{definition.Name}, profile.Aliases...)
		score := discoveryMatchScore(query, phrases)
		if score == 0 {
			continue
		}
		if score >= 1000 {
			hasExactSceneMatch = true
		}
		candidate, ok := catalogPreviewCandidate(definition, "")
		if !ok {
			return nil, ErrCatalogDefinitionInvalid
		}
		matches[definition.ID] = matchedCandidate{candidate: candidate, score: score}
	}
	if hasExactSceneMatch {
		for sceneID, match := range matches {
			if match.score < 1000 {
				delete(matches, sceneID)
			}
		}
	}
	if len(matches) == 0 {
		for _, profile := range resolver.catalog.experienceDiscovery {
			score := discoveryMatchScore(query, profile.Aliases)
			if score == 0 {
				continue
			}
			definition, found := byID[profile.DefaultSceneID]
			if !found {
				return nil, ErrCatalogDefinitionInvalid
			}
			candidate, ok := catalogPreviewCandidate(
				definition,
				profile.DefaultPracticeOptionID,
			)
			if !ok {
				return nil, ErrCatalogDefinitionInvalid
			}
			matches[definition.ID] = matchedCandidate{candidate: candidate, score: score}
		}
	}

	scored := make([]matchedCandidate, 0, len(matches))
	for _, match := range matches {
		scored = append(scored, match)
	}
	sort.Slice(scored, func(left, right int) bool {
		if scored[left].score == scored[right].score {
			return scored[left].candidate.Scene.ID < scored[right].candidate.Scene.ID
		}
		return scored[left].score > scored[right].score
	})
	if len(scored) > MaxPreviewCatalogCandidates {
		scored = scored[:MaxPreviewCatalogCandidates]
	}
	result := make([]PreviewCatalogCandidate, len(scored))
	for index, match := range scored {
		result[index] = match.candidate
	}
	return result, nil
}

func discoveryMatchScore(query string, phrases []string) int {
	best := 0
	for _, phrase := range phrases {
		normalized := normalizeDiscoveryText(phrase)
		if normalized == "" || !strings.Contains(query, normalized) {
			continue
		}
		score := utf8.RuneCountInString(normalized)
		if query == normalized {
			score += 1000
		}
		if score > best {
			best = score
		}
	}
	return best
}

func catalogPreviewCandidate(
	definition SceneDefinition,
	optionID string,
) (PreviewCatalogCandidate, bool) {
	if len(definition.Roles) == 0 {
		return PreviewCatalogCandidate{}, false
	}
	var option PracticeOption
	var found bool
	if optionID == "" {
		option, found = defaultPracticeOption(definition.PracticeOptions)
	} else {
		option, found = findPracticeOption(definition.PracticeOptions, optionID)
	}
	if !found {
		return PreviewCatalogCandidate{}, false
	}
	return PreviewCatalogCandidate{
		Scene:          definition,
		DefaultRoleIDs: []string{definition.Roles[0].ID},
		DefaultOption:  option,
	}, true
}

func defaultPracticeOption(
	options []PracticeOption,
) (PracticeOption, bool) {
	for _, option := range options {
		if option.Mode == PracticeModeFullSimulation ||
			option.Mode == PracticeModeFullMock {
			return option, true
		}
	}
	return PracticeOption{}, false
}
