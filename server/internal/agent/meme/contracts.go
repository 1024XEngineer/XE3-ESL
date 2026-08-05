// Package meme defines the provider-neutral contracts for enriching a
// completed Agent reply with one or more reaction images. Concrete model,
// catalog, selection, and storage implementations live in later slices.
package meme

import (
	"context"
	"os"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Category is the stable machine name selected from a versioned meme pack.
type Category string

type CategoryDefinition struct {
	Category    Category
	Description string
}

// Config controls the future meme enrichment coordinator. It intentionally
// contains no provider credentials or filesystem paths.
type Config struct {
	Enabled             bool
	SendProbability     float64
	MaxPerMessage       int
	AvoidRecentCount    int
	ClassificationLimit time.Duration
	DefaultCategory     Category
	PackID              string
	PackVersion         string
}

// Valid rejects configurations that cannot produce a bounded, deterministic
// enrichment decision. A disabled configuration remains valid without a pack.
func (config Config) Valid() bool {
	if config.SendProbability < 0 || config.SendProbability > 1 ||
		config.MaxPerMessage < 0 || config.MaxPerMessage > 4 ||
		config.AvoidRecentCount < 0 || config.AvoidRecentCount > 100 ||
		config.ClassificationLimit < 0 {
		return false
	}
	if !config.Enabled {
		return true
	}
	return config.MaxPerMessage > 0 &&
		config.ClassificationLimit > 0 &&
		config.DefaultCategory != "" &&
		config.PackID != "" &&
		config.PackVersion != ""
}

// ClassificationRequest contains the completed reply and the trusted turn
// identity. Implementations classify the assistant's intended expression, not
// the user's emotion in isolation.
type ClassificationRequest struct {
	Actor            requestcontext.Actor
	RunID            string
	ThreadID         string
	InputMessageID   string
	UserContent      string
	AssistantContent string
	Categories       []CategoryDefinition
}

// Classification is the validated output of the future forced meme.select
// model tool call.
type Classification struct {
	Category      Category
	PolicyVersion string
	Provider      string
	Model         string
}

// Classifier is the model-facing boundary. Its implementation will expose
// only meme.select with a specific tool choice and must not generate user text.
type Classifier interface {
	Classify(context.Context, ClassificationRequest) (Classification, error)
}

// Asset is one immutable entry in a versioned meme pack.
type Asset struct {
	MemeID         string
	PackID         string
	PackVersion    string
	Category       Category
	AssetKey       string
	ContentType    string
	SizeBytes      int64
	Width          int
	Height         int
	ChecksumSHA256 string
	Weight         int
}

// Catalog is the read-only pack boundary. Implementations may use an embedded
// manifest or another immutable source without changing the coordinator.
type Catalog interface {
	Categories(context.Context, string, string) ([]CategoryDefinition, error)
	Candidates(context.Context, string, string, Category) ([]Asset, error)
}

// SelectionRequest contains all deterministic inputs required to choose
// assets without relying on process-local random state.
type SelectionRequest struct {
	RunID         string
	ThreadID      string
	Category      Category
	Candidates    []Asset
	RecentMemeIDs []string
	Maximum       int
	PolicyVersion string
}

// Selector chooses immutable assets from one category. A retry of the same
// request must return the same ordered result.
type Selector interface {
	Select(context.Context, SelectionRequest) ([]Asset, error)
}

// RecentReader supplies the bounded thread-local exclusion window used by the
// selector. It never owns message history.
type RecentReader interface {
	RecentMemeIDs(context.Context, string, string, int) ([]string, error)
}

// Attachment is the durable projection attached to one Assistant Message.
type Attachment struct {
	ID        string
	OwnerID   string
	ThreadID  string
	MessageID string
	RunID     string
	Asset
	Position                    int
	ClassificationPolicyVersion string
	SelectionPolicyVersion      string
	ClassifierProvider          string
	ClassifierModel             string
	CreatedAt                   time.Time
}

type AttachmentReader interface {
	MessageAttachments(context.Context, string, string, string) ([]Attachment, error)
	FindAttachment(context.Context, string, string) (Attachment, error)
}

type LocalAssetReader interface {
	Open(string) (*os.File, Asset, error)
}
