package scene

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxPreviewCatalogCandidates = 5

var previewCatalogGenericQueryTerms = map[string]struct{}{
	"一下": {},
	"场景": {},
	"对话": {},
	"想练": {},
	"模拟": {},
	"练习": {},
	"英语": {},
}

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
		return nil, errors.New("scene: catalog is required")
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
	queryTerms := previewCatalogQueryTerms(query)
	definitions, err := resolver.catalog.ListActiveScenes(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		score := previewCatalogScore(query, queryTerms, definition)
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
	queryTerms []string,
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
			continue
		}
		for _, term := range queryTerms {
			if strings.Contains(normalized, term) {
				score++
				break
			}
		}
	}
	return score
}

// previewCatalogQueryTerms extracts meaningful Chinese fragments from a
// sentence-shaped query. The catalog remains the source of searchable text;
// this only lets a request such as “我想练习看房” match the concise catalog
// phrase “看房与租赁咨询”. Terms shorter than two runes are intentionally
// ignored to avoid broad matches on individual Chinese characters.
func previewCatalogQueryTerms(query string) []string {
	const maxTermRunes = 6
	seen := make(map[string]struct{})
	terms := make([]string, 0)
	appendSequence := func(sequence []rune) {
		for width := min(maxTermRunes, len(sequence)); width >= 2; width-- {
			for start := 0; start+width <= len(sequence); start++ {
				term := string(sequence[start : start+width])
				if _, generic := previewCatalogGenericQueryTerms[term]; generic {
					continue
				}
				if _, duplicate := seen[term]; duplicate {
					continue
				}
				seen[term] = struct{}{}
				terms = append(terms, term)
			}
		}
	}

	sequence := make([]rune, 0)
	for _, character := range []rune(query) {
		if unicode.Is(unicode.Han, character) {
			sequence = append(sequence, character)
			continue
		}
		appendSequence(sequence)
		sequence = sequence[:0]
	}
	appendSequence(sequence)
	return terms
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
