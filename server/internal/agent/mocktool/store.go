package mocktool

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

const (
	MaterialSearchToolName = "material.search.v1"
	MistakeSearchToolName  = "mistake.search.v1"
)

var ErrTemporarilyUnavailable = errors.New("agent mock tool: temporarily unavailable")

type Store struct {
	mu sync.Mutex

	scenarios []mattertool.ScenarioResult
	reviews   []reviewtool.ReviewSummary
	materials []Material
	mistakes  []Mistake

	createdScenarios map[string]mattertool.ScenarioResult
	forbidden        map[string]bool
	unavailable      map[string]bool
}

type Material struct {
	ID      string
	Kind    string
	Title   string
	Excerpt string
}

type Mistake struct {
	ID         string
	ScenarioID string
	Category   string
	Summary    string
	Suggestion string
}

func NewStore() *Store {
	return &Store{
		scenarios: []mattertool.ScenarioResult{{
			ID:      "mock-scenario-001",
			Title:   "English PM interview",
			Type:    "interview",
			Status:  "active",
			Summary: "Practice product sense, self introduction, and trade-off answers.",
			SourceRef: []tool.SourceRef{{
				Type: "mock_scenario",
				ID:   "mock-scenario-001",
			}},
		}, {
			ID:      "mock-scenario-002",
			Title:   "Customer escalation meeting",
			Type:    "client",
			Status:  "active",
			Summary: "Practice explaining a delayed delivery plan to a customer.",
			SourceRef: []tool.SourceRef{{
				Type: "mock_scenario",
				ID:   "mock-scenario-002",
			}},
		}},
		reviews: []reviewtool.ReviewSummary{{
			ID:         "mock-review-001",
			Title:      "PM interview answer review",
			Summary:    "The answer had clear structure, but the example needed stronger metrics.",
			ScenarioID: "mock-scenario-001",
			SourceRefs: []tool.SourceRef{{
				Type: "mock_review",
				ID:   "mock-review-001",
			}},
		}, {
			ID:         "mock-review-002",
			Title:      "Customer escalation review",
			Summary:    "Tone was calm, but the next action and owner were not explicit enough.",
			ScenarioID: "mock-scenario-002",
			SourceRefs: []tool.SourceRef{{
				Type: "mock_review",
				ID:   "mock-review-002",
			}},
		}},
		materials: []Material{{
			ID:      "mock-material-resume-001",
			Kind:    "resume",
			Title:   "Resume backend reliability project",
			Excerpt: "Led a Go API reliability project and reduced incident recovery time.",
		}, {
			ID:      "mock-material-jd-001",
			Kind:    "jd",
			Title:   "Senior backend engineer job description",
			Excerpt: "Own reliable APIs, communicate trade-offs, and collaborate in English.",
		}},
		mistakes: []Mistake{{
			ID:         "mock-mistake-001",
			ScenarioID: "mock-scenario-001",
			Category:   "structure",
			Summary:    "Answer opened with background before stating the decision.",
			Suggestion: "Start with the decision, then add context and trade-offs.",
		}, {
			ID:         "mock-mistake-002",
			ScenarioID: "mock-scenario-002",
			Category:   "clarity",
			Summary:    "Next steps did not include a specific owner or deadline.",
			Suggestion: "Close with owner, deadline, and expected customer update.",
		}},
		createdScenarios: make(map[string]mattertool.ScenarioResult),
		forbidden:        make(map[string]bool),
		unavailable:      make(map[string]bool),
	}
}

func Tools(store *Store) []tool.Tool {
	if store == nil {
		store = NewStore()
	}
	return []tool.Tool{
		mattertool.NewScenarioCreateTool(store),
		mattertool.NewScenarioSearchTool(store),
		reviewtool.NewReviewSearchTool(store),
		reviewtool.NewReviewGetTool(store),
		NewMaterialSearchTool(store),
		NewMistakeSearchTool(store),
	}
}

func NewRegistry(store *Store) (*tool.Registry, error) {
	return tool.NewRegistry(Tools(store)...)
}

func (s *Store) SetForbidden(name string, forbidden bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forbidden[name] = forbidden
}

func (s *Store) SetUnavailable(name string, unavailable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unavailable[name] = unavailable
}

func (s *Store) CreateScenario(
	ctx context.Context,
	call tool.CallContext,
	input mattertool.ScenarioCreateInput,
) (mattertool.ScenarioResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(mattertool.ScenarioCreateToolName); err != nil {
		return mattertool.ScenarioResult{}, err
	}
	if call.RequestID == "" {
		return mattertool.ScenarioResult{}, tool.ErrToolRejected
	}
	if existing, ok := s.createdScenarios[call.RequestID]; ok {
		return existing, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "Mock " + strings.TrimSpace(input.Type) + " scenario"
	}
	id := "mock-created-scenario-" + stableSuffix(call.RequestID)
	result := mattertool.ScenarioResult{
		ID:      id,
		Title:   title,
		Type:    strings.TrimSpace(input.Type),
		Status:  "active",
		Summary: strings.TrimSpace(input.Goal),
		SourceRef: []tool.SourceRef{{
			Type: "mock_scenario",
			ID:   id,
		}},
	}
	s.createdScenarios[call.RequestID] = result
	s.scenarios = append(s.scenarios, result)
	return result, nil
}

func (s *Store) SearchScenarios(
	ctx context.Context,
	call tool.CallContext,
	input mattertool.ScenarioSearchInput,
) ([]mattertool.ScenarioResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(mattertool.ScenarioSearchToolName); err != nil {
		return nil, err
	}
	results := make([]mattertool.ScenarioResult, 0)
	for _, scenario := range s.scenarios {
		if containsAny(input.Query, scenario.Title, scenario.Type, scenario.Summary) {
			results = append(results, scenario)
		}
	}
	return limit(results, input.Limit), nil
}

func (s *Store) SearchReviews(
	ctx context.Context,
	call tool.CallContext,
	input reviewtool.ReviewSearchInput,
) ([]reviewtool.ReviewSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(reviewtool.ReviewSearchToolName); err != nil {
		return nil, err
	}
	results := make([]reviewtool.ReviewSummary, 0)
	for _, review := range s.reviews {
		if input.ScenarioID != "" && input.ScenarioID != review.ScenarioID {
			continue
		}
		if containsAny(input.Query, review.Title, review.Summary, review.ScenarioID) {
			results = append(results, review)
		}
	}
	return limit(results, input.Limit), nil
}

func (s *Store) GetReview(
	ctx context.Context,
	call tool.CallContext,
	input reviewtool.ReviewGetInput,
) (reviewtool.ReviewSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(reviewtool.ReviewGetToolName); err != nil {
		return reviewtool.ReviewSummary{}, err
	}
	for _, review := range s.reviews {
		if review.ID == input.ReviewID {
			return review, nil
		}
	}
	return reviewtool.ReviewSummary{}, tool.ErrInvalidInput
}

func (s *Store) SearchMaterials(name string, query MaterialSearchInput) ([]Material, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(name); err != nil {
		return nil, err
	}
	results := make([]Material, 0)
	for _, material := range s.materials {
		if query.Kind != "" && query.Kind != material.Kind {
			continue
		}
		if containsAny(query.Query, material.Title, material.Excerpt, material.Kind) {
			results = append(results, material)
		}
	}
	return limit(results, query.Limit), nil
}

func (s *Store) SearchMistakes(name string, query MistakeSearchInput) ([]Mistake, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(name); err != nil {
		return nil, err
	}
	results := make([]Mistake, 0)
	for _, mistake := range s.mistakes {
		if query.ScenarioID != "" && query.ScenarioID != mistake.ScenarioID {
			continue
		}
		if containsAny(query.Query, mistake.Category, mistake.Summary, mistake.Suggestion) {
			results = append(results, mistake)
		}
	}
	return limit(results, query.Limit), nil
}

func (s *Store) beforeLocked(name string) error {
	if s.forbidden[name] {
		return tool.ErrToolRejected
	}
	if s.unavailable[name] {
		return ErrTemporarilyUnavailable
	}
	return nil
}

func containsAny(query string, values ...string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func limit[T any](items []T, maximum int) []T {
	if maximum <= 0 || maximum >= len(items) {
		return items
	}
	return items[:maximum]
}

func stableSuffix(value string) string {
	clean := strings.ToLower(strings.TrimSpace(value))
	clean = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, clean)
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "request"
	}
	if len(clean) > 24 {
		return clean[:24]
	}
	return clean
}

type CapabilitySummary struct {
	Name          string   `json:"name"`
	Risk          string   `json:"risk"`
	ReadOnly      bool     `json:"read_only"`
	SchemaFields  []string `json:"schema_fields"`
	RequiredNames []string `json:"required_names"`
}

func CapabilitySummaries(registry *tool.Registry) []CapabilitySummary {
	definitions := registry.Definitions()
	summaries := make([]CapabilitySummary, 0, len(definitions))
	for _, definition := range definitions {
		properties, _ := definition.InputSchema["properties"].(map[string]any)
		fields := make([]string, 0, len(properties))
		for field := range properties {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		required := make([]string, 0)
		for _, field := range readRequired(definition.InputSchema) {
			required = append(required, field)
		}
		sort.Strings(required)
		summaries = append(summaries, CapabilitySummary{
			Name:          definition.Name,
			Risk:          string(definition.Risk),
			ReadOnly:      definition.ReadOnly,
			SchemaFields:  fields,
			RequiredNames: required,
		})
	}
	return summaries
}

func readRequired(schema map[string]any) []string {
	switch value := schema["required"].(type) {
	case []string:
		return append([]string{}, value...)
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if field, ok := item.(string); ok {
				result = append(result, field)
			}
		}
		return result
	default:
		return nil
	}
}
