package memory

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxSearchResults    = 10
	maxSearchCandidates = 40
)

type SearchConfig struct {
	Provider               string
	Model                  string
	Dimensions             int
	EmbeddingPolicyVersion string
	RetrievalPolicyVersion string
	CandidateLimit         int
	MinimumSimilarity      float64
}

func (configuration SearchConfig) Valid() bool {
	return providerIdentifierPattern.MatchString(configuration.Provider) &&
		modelIdentifierPattern.MatchString(configuration.Model) &&
		configuration.Dimensions == MemoryEmbeddingDimensions &&
		validPolicyVersion(configuration.EmbeddingPolicyVersion) &&
		validPolicyVersion(configuration.RetrievalPolicyVersion) &&
		configuration.CandidateLimit >= 1 &&
		configuration.CandidateLimit <= maxSearchCandidates &&
		configuration.MinimumSimilarity >= -1 &&
		configuration.MinimumSimilarity <= 1
}

type SearchRequest struct {
	Actor                 requestcontext.Actor
	Query                 string
	GoalID                string
	ExcludedCanonicalKeys []string
	Limit                 int
}

func (request SearchRequest) Valid() bool {
	return request.Actor.Valid() &&
		request.Query != "" &&
		request.Query == strings.TrimSpace(request.Query) &&
		len(request.Query) <= maxEmbeddingInputBytes &&
		(request.GoalID == "" || validUUID(request.GoalID)) &&
		ValidStableProfileCanonicalKeys(request.ExcludedCanonicalKeys) &&
		request.Limit >= 1 &&
		request.Limit <= maxSearchResults
}

type SearchCandidate struct {
	Memory     Memory
	Similarity float64
}

type SearchHit struct {
	MemoryID               string
	MemoryVersion          int64
	CanonicalKey           string
	Type                   Type
	Content                string
	Scope                  ScopeType
	GoalID                 string
	Similarity             float64
	Score                  float64
	EmbeddingProvider      string
	EmbeddingModel         string
	EmbeddingDimensions    int
	EmbeddingPolicyVersion string
	RetrievalPolicyVersion string
}

type SearchCandidateRepository interface {
	SearchCandidates(
		context.Context,
		requestcontext.Actor,
		[]float32,
		string,
		[]string,
		SearchConfig,
	) ([]SearchCandidate, error)
}

type Searcher interface {
	Search(context.Context, SearchRequest) ([]SearchHit, error)
}

type SearchService struct {
	repository SearchCandidateRepository
	embedder   Embedder
	config     SearchConfig
	now        func() time.Time
}

func NewSearchService(
	repository SearchCandidateRepository,
	embedder Embedder,
	configuration SearchConfig,
	now func() time.Time,
) (*SearchService, error) {
	if repository == nil ||
		embedder == nil ||
		!configuration.Valid() ||
		now == nil {
		return nil, ErrInvalidArgument
	}
	return &SearchService{
		repository: repository,
		embedder:   embedder,
		config:     configuration,
		now:        now,
	}, nil
}

func (service *SearchService) Search(
	ctx context.Context,
	request SearchRequest,
) ([]SearchHit, error) {
	if ctx == nil || !request.Valid() {
		return nil, ErrInvalidArgument
	}
	embeddingRequest := EmbeddingRequest{
		Input:      request.Query,
		Dimensions: service.config.Dimensions,
	}
	result, err := service.embedder.Embed(ctx, embeddingRequest)
	if err != nil {
		return nil, err
	}
	if err := ValidateEmbeddingResult(
		embeddingRequest.Dimensions,
		result,
	); err != nil {
		return nil, ErrIndexResponse
	}
	if result.Provider != service.config.Provider ||
		result.Model != service.config.Model ||
		result.Dimensions != service.config.Dimensions ||
		len(result.Vector) != service.config.Dimensions {
		return nil, ErrIndexResponse
	}
	candidates, err := service.repository.SearchCandidates(
		ctx,
		request.Actor,
		result.Vector,
		request.GoalID,
		request.ExcludedCanonicalKeys,
		service.config,
	)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, min(request.Limit, len(candidates)))
	now := service.now().UTC()
	for _, candidate := range candidates {
		if !candidate.Memory.Valid() ||
			candidate.Memory.OwnerID != request.Actor.UserID ||
			candidate.Similarity < service.config.MinimumSimilarity ||
			candidate.Similarity < -1 ||
			candidate.Similarity > 1 {
			continue
		}
		if candidate.Memory.Scope == ScopeGoal &&
			candidate.Memory.GoalID != request.GoalID {
			return nil, ErrRepository
		}
		if containsString(
			request.ExcludedCanonicalKeys,
			candidate.Memory.CanonicalKey,
		) {
			return nil, ErrRepository
		}
		hits = append(hits, SearchHit{
			MemoryID:               candidate.Memory.ID,
			MemoryVersion:          candidate.Memory.Version,
			CanonicalKey:           candidate.Memory.CanonicalKey,
			Type:                   candidate.Memory.Type,
			Content:                candidate.Memory.Content,
			Scope:                  candidate.Memory.Scope,
			GoalID:                 candidate.Memory.GoalID,
			Similarity:             candidate.Similarity,
			Score:                  rerankScore(candidate, request.GoalID, now),
			EmbeddingProvider:      service.config.Provider,
			EmbeddingModel:         service.config.Model,
			EmbeddingDimensions:    service.config.Dimensions,
			EmbeddingPolicyVersion: service.config.EmbeddingPolicyVersion,
			RetrievalPolicyVersion: service.config.RetrievalPolicyVersion,
		})
	}
	sort.Slice(hits, func(left, right int) bool {
		if hits[left].Score != hits[right].Score {
			return hits[left].Score > hits[right].Score
		}
		if hits[left].Similarity != hits[right].Similarity {
			return hits[left].Similarity > hits[right].Similarity
		}
		return hits[left].MemoryID < hits[right].MemoryID
	})
	if len(hits) > request.Limit {
		hits = hits[:request.Limit]
	}
	return hits, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func rerankScore(
	candidate SearchCandidate,
	goalID string,
	now time.Time,
) float64 {
	score := candidate.Similarity * 0.75
	if candidate.Memory.Scope == ScopeGoal &&
		candidate.Memory.GoalID == goalID {
		score += 0.10
	}
	score += memoryTypeWeight(candidate.Memory.Type)
	age := now.Sub(candidate.Memory.UpdatedAt)
	if age < 0 {
		age = 0
	}
	const recencyWindow = 30 * 24 * time.Hour
	if age < recencyWindow {
		score += 0.05 * (1 - float64(age)/float64(recencyWindow))
	}
	return score
}

func memoryTypeWeight(memoryType Type) float64 {
	switch memoryType {
	case TypeGoal, TypePreference:
		return 0.10
	case TypeIdentity, TypeProfile:
		return 0.08
	case TypeInterest:
		return 0.05
	case TypeTopic:
		return 0.03
	default:
		return 0
	}
}

var _ Searcher = (*SearchService)(nil)
