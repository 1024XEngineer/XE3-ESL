package runtime

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	memoryContextLimit        = 6
	memoryContextPolicyV1     = "memory-context-v1"
	memoryScopeUser           = "user"
	memoryScopeMatter         = "matter"
	memoryEmbeddingDimensions = 1024
)

var memoryPolicyVersionPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
)

type MemorySearchRequest struct {
	Actor                 requestcontext.Actor
	Query                 string
	MatterID              string
	ExcludedCanonicalKeys []string
	Limit                 int
}

type MemorySearchHit struct {
	MemoryID               string
	MemoryVersion          int64
	CanonicalKey           string
	Type                   string
	Content                string
	Scope                  string
	MatterID               string
	Similarity             float64
	Score                  float64
	EmbeddingProvider      string
	EmbeddingModel         string
	EmbeddingDimensions    int
	EmbeddingPolicyVersion string
	RetrievalPolicyVersion string
}

func (hit MemorySearchHit) valid(matterID string) bool {
	if !coreValidMemoryHit(hit) {
		return false
	}
	switch hit.Scope {
	case memoryScopeUser:
		return hit.MatterID == ""
	case memoryScopeMatter:
		return matterID != "" && hit.MatterID == matterID
	default:
		return false
	}
}

func coreValidMemoryHit(hit MemorySearchHit) bool {
	return core.ValidUUID(hit.MemoryID) &&
		hit.MemoryVersion > 0 &&
		stableProfileCanonicalKeyPattern.MatchString(hit.CanonicalKey) &&
		hit.Type != "" &&
		hit.Type == strings.TrimSpace(hit.Type) &&
		len(hit.Type) <= 32 &&
		hit.Content != "" &&
		hit.Content == strings.TrimSpace(hit.Content) &&
		len(hit.Content) <= 16384 &&
		!math.IsNaN(hit.Similarity) &&
		!math.IsInf(hit.Similarity, 0) &&
		hit.Similarity >= -1 &&
		hit.Similarity <= 1 &&
		!math.IsNaN(hit.Score) &&
		!math.IsInf(hit.Score, 0) &&
		core.ValidProviderID(hit.EmbeddingProvider) &&
		core.ValidModelID(hit.EmbeddingModel) &&
		hit.EmbeddingDimensions == memoryEmbeddingDimensions &&
		memoryPolicyVersionPattern.MatchString(
			hit.EmbeddingPolicyVersion,
		) &&
		memoryPolicyVersionPattern.MatchString(
			hit.RetrievalPolicyVersion,
		)
}

type MemorySearcher interface {
	Search(
		context.Context,
		MemorySearchRequest,
	) ([]MemorySearchHit, error)
}
