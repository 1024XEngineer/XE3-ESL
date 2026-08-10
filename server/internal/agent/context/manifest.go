package context

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

type MessageSource struct {
	MessageID string                   `json:"message_id"`
	Sequence  int64                    `json:"sequence"`
	Role      conversation.MessageRole `json:"role"`
}

type MemorySource struct {
	MemoryID               string  `json:"memory_id"`
	MemoryVersion          int64   `json:"memory_version"`
	Type                   string  `json:"type"`
	Scope                  string  `json:"scope"`
	GoalID                 string  `json:"goal_id,omitempty"`
	Similarity             float64 `json:"similarity"`
	Score                  float64 `json:"score"`
	EmbeddingProvider      string  `json:"embedding_provider"`
	EmbeddingModel         string  `json:"embedding_model"`
	EmbeddingDimensions    int     `json:"embedding_dimensions"`
	EmbeddingPolicyVersion string  `json:"embedding_policy_version"`
	RetrievalPolicyVersion string  `json:"retrieval_policy_version"`
}

type StableProfileSource struct {
	MemoryID      string `json:"memory_id"`
	MemoryVersion int64  `json:"memory_version"`
	CanonicalKey  string `json:"canonical_key"`
	Type          string `json:"type"`
	Scope         string `json:"scope"`
}

type LearningProfileSource struct {
	DimensionKey                string    `json:"dimension_key"`
	StrategyVersion             string    `json:"strategy_version"`
	UpdatedAt                   time.Time `json:"updated_at"`
	EvaluationRevisionSourceIDs []string  `json:"evaluation_revision_source_ids"`
}

type SummarySource struct {
	CheckpointID           string `json:"checkpoint_id"`
	SourceFromSequence     int64  `json:"source_from_sequence"`
	CoveredThroughSequence int64  `json:"covered_through_sequence"`
	PolicyVersion          string `json:"policy_version"`
	PromptVersion          string `json:"prompt_version"`
	Provider               string `json:"provider"`
	Model                  string `json:"model"`
}

type Manifest struct {
	RunID                                     string
	OwnerID                                   string
	ThreadID                                  string
	InputMessageID                            string
	ActiveGoalID                              string
	ActiveGoalVersion                         int64
	InstructionVersion                        string
	LearningProfileContextPolicyVersion       string
	SelectedLearningProfile                   []LearningProfileSource
	StableProfileContextPolicyVersion         string
	SelectedStableProfile                     []StableProfileSource
	MemoryContextPolicyVersion                string
	SelectedMemories                          []MemorySource
	MemoryExtractionBarrierPolicyVersion      string
	MemoryExtractionBarrierCutoff             time.Time
	MemoryExtractionBarrierStatus             string
	MemoryExtractionBarrierWaitedMilliseconds int64
	MemoryExtractionBarrierCoveredThrough     time.Time
	SummaryContextPolicyVersion               string
	SummaryContextStatus                      string
	SelectedSummary                           *SummarySource
	SelectedMessages                          []MessageSource
	OmittedMessageCount                       int
	TrimReason                                string
	MaxInputCharacters                        int
	UsedInputCharacters                       int
	RequestedProvider                         string
	RequestedModel                            string
	MaxOutputTokens                           int
	ExposedTools                              []string
	ToolSchemaHashes                          map[string]string
	CreatedAt                                 time.Time
}

func (manifest Manifest) Valid() bool {
	return uuidPattern.MatchString(manifest.RunID) &&
		uuidPattern.MatchString(manifest.OwnerID) &&
		uuidPattern.MatchString(manifest.ThreadID) &&
		uuidPattern.MatchString(manifest.InputMessageID) &&
		providerPattern.MatchString(manifest.RequestedProvider) &&
		validModelID(manifest.RequestedModel) &&
		manifest.MaxOutputTokens > 0 &&
		manifest.MaxOutputTokens <= maxBudget &&
		manifest.MaxInputCharacters >= 5000 &&
		manifest.MaxInputCharacters <= maxBudget &&
		manifest.UsedInputCharacters >= 0 &&
		manifest.UsedInputCharacters <= manifest.MaxInputCharacters &&
		manifest.validMemoryExtractionBarrier()
}

func (manifest Manifest) validMemoryExtractionBarrier() bool {
	if manifest.MemoryExtractionBarrierPolicyVersion !=
		MemoryExtractionBarrierPolicyV1 ||
		manifest.MemoryExtractionBarrierCutoff.IsZero() ||
		manifest.MemoryExtractionBarrierCutoff.Location() != time.UTC ||
		manifest.MemoryExtractionBarrierWaitedMilliseconds < 0 ||
		manifest.MemoryExtractionBarrierWaitedMilliseconds > 5000 ||
		(!manifest.MemoryExtractionBarrierCoveredThrough.IsZero() &&
			(manifest.MemoryExtractionBarrierCoveredThrough.Location() != time.UTC ||
				manifest.MemoryExtractionBarrierCoveredThrough.After(
					manifest.MemoryExtractionBarrierCutoff,
				))) {
		return false
	}
	switch manifest.MemoryExtractionBarrierStatus {
	case memoryExtractionBarrierNotRequired:
		return manifest.MemoryExtractionBarrierWaitedMilliseconds == 0 &&
			manifest.MemoryExtractionBarrierCoveredThrough.IsZero()
	case string(MemoryExtractionBarrierReady):
		return manifest.MemoryExtractionBarrierWaitedMilliseconds == 0
	case string(MemoryExtractionBarrierWaited):
		return manifest.MemoryExtractionBarrierWaitedMilliseconds > 0 &&
			!manifest.MemoryExtractionBarrierCoveredThrough.IsZero()
	default:
		return false
	}
}
