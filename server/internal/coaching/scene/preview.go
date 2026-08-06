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
	ctx context.Context,
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
	definitions, err := resolver.catalog.ListActiveScenes(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		score := previewCatalogScore(query, definition)
		if score == 0 {
			continue
		}
		defaultOption, found := defaultPracticeOption(definition.PracticeOptions)
		if !found || len(definition.Roles) == 0 {
			continue
		}
		candidate := PreviewCatalogCandidate{
			Scene:          definition,
			DefaultRoleIDs: []string{definition.Roles[0].ID},
			DefaultOption:  defaultOption,
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
			return scored[left].candidate.Scene.ID <
				scored[right].candidate.Scene.ID
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
	definition SceneDefinition,
) int {
	if query == strings.ToLower(definition.ID) {
		return 100
	}
	if query == strings.ToLower(definition.Name) {
		return 90
	}
	fields := []string{
		definition.Name,
		string(definition.Experience),
		string(definition.Category),
		definition.Prompt.PublicSceneBrief,
		definition.Prompt.PracticeGoal,
		definition.Prompt.UserRole,
		definition.Prompt.AIRole,
	}
	for _, role := range definition.Roles {
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
