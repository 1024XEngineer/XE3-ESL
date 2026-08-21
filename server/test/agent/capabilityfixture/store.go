package capabilityfixture

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
)

const (
	MaterialSearchToolName = "material.search.v1"
	MistakeSearchToolName  = "mistake.search.v1"
)

var ErrTemporarilyUnavailable = errors.New(
	"agent capability fixture: temporarily unavailable",
)

type Store struct {
	mu sync.Mutex

	reviews   []reviewcapability.ReviewDetail
	materials []Material
	mistakes  []Mistake

	forbidden   map[string]bool
	unavailable map[string]bool
}

type Material struct {
	ID      string
	Kind    string
	Title   string
	Excerpt string
}

type Mistake struct {
	ID         string
	SceneID    string
	Category   string
	Summary    string
	Suggestion string
}

func NewStore() *Store {
	return &Store{
		reviews: []reviewcapability.ReviewDetail{{
			ID:                 "mock-report-001",
			PracticeSessionID:  "mock-practice-session-001",
			SceneType:          "INTERVIEW",
			PracticeExperience: "INTERVIEW",
			SceneCategory:      "INTERVIEW_PROFESSIONAL",
			PracticeMode:       "FULL_SIMULATION",
			Scoreability:       "PROVISIONAL",
			Summary:            "The answer had clear structure, but the example needed stronger metrics.",
			SourceRefs: []capability.SourceRef{{
				Type: "evaluation_report",
				ID:   "mock-report-001",
			}},
		}, {
			ID:                 "mock-report-002",
			PracticeSessionID:  "mock-practice-session-002",
			SceneType:          "OVERSEAS_WORKPLACE",
			PracticeExperience: "WORKPLACE",
			SceneCategory:      "WORKPLACE_GENERAL",
			PracticeMode:       "FULL_SIMULATION",
			Scoreability:       "PROVISIONAL",
			Summary:            "Tone was calm, but the next action and owner were not explicit enough.",
			SourceRefs: []capability.SourceRef{{
				Type: "evaluation_report",
				ID:   "mock-report-002",
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
			SceneID:    "mock-scene-001",
			Category:   "structure",
			Summary:    "Answer opened with background before stating the decision.",
			Suggestion: "Start with the decision, then add context and trade-offs.",
		}, {
			ID:         "mock-mistake-002",
			SceneID:    "mock-scene-002",
			Category:   "clarity",
			Summary:    "Next steps did not include a specific owner or deadline.",
			Suggestion: "Close with owner, deadline, and expected customer update.",
		}},
		forbidden:   make(map[string]bool),
		unavailable: make(map[string]bool),
	}
}

func Tools(store *Store) []capability.Tool {
	if store == nil {
		store = NewStore()
	}
	return []capability.Tool{
		reviewcapability.NewReviewSearchTool(store),
		reviewcapability.NewReviewGetTool(store),
		NewMaterialSearchTool(store),
		NewMistakeSearchTool(store),
	}
}

func NewRegistry(store *Store) (*capability.Registry, error) {
	return capability.NewRegistry(Tools(store)...)
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

func (s *Store) SearchReviews(
	ctx context.Context,
	call capability.CallContext,
	input reviewcapability.ReviewSearchInput,
) ([]reviewcapability.ReviewSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(reviewcapability.ReviewSearchToolName); err != nil {
		return nil, err
	}
	results := make([]reviewcapability.ReviewSummary, 0)
	for _, review := range s.reviews {
		if containsAny(
			input.Query,
			review.Summary,
			review.PracticeSessionID,
			review.SceneType,
			review.PracticeExperience,
			review.SceneCategory,
			review.PracticeMode,
		) {
			results = append(results, reviewcapability.ReviewSummary{
				ID:                 review.ID,
				PracticeSessionID:  review.PracticeSessionID,
				SceneType:          review.SceneType,
				PracticeExperience: review.PracticeExperience,
				SceneCategory:      review.SceneCategory,
				PracticeMode:       review.PracticeMode,
				Scoreability:       review.Scoreability,
				Summary:            review.Summary,
				CompletedAt:        review.CompletedAt,
				SourceRefs:         review.SourceRefs,
			})
		}
	}
	return limit(results, input.Limit), nil
}

func (s *Store) GetReview(
	ctx context.Context,
	call capability.CallContext,
	input reviewcapability.ReviewGetInput,
) (reviewcapability.ReviewDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.beforeLocked(reviewcapability.ReviewGetToolName); err != nil {
		return reviewcapability.ReviewDetail{}, err
	}
	for _, review := range s.reviews {
		if review.ID == input.ReportID {
			return review, nil
		}
	}
	return reviewcapability.ReviewDetail{}, capability.ErrInvalidInput
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
		if query.SceneID != "" && query.SceneID != mistake.SceneID {
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
		return capability.ErrExecutionRejected
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

type CapabilitySummary struct {
	Name          string   `json:"name"`
	Risk          string   `json:"risk"`
	ReadOnly      bool     `json:"read_only"`
	SchemaFields  []string `json:"schema_fields"`
	RequiredNames []string `json:"required_names"`
}

func CapabilitySummaries(registry *capability.Registry) []CapabilitySummary {
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
