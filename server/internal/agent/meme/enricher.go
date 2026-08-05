package meme

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

type Enricher struct {
	config     Config
	classifier Classifier
	catalog    Catalog
	selector   Selector
	recent     RecentReader
}

func NewEnricher(
	config Config,
	classifier Classifier,
	catalog Catalog,
	selector Selector,
	recent RecentReader,
) (*Enricher, error) {
	if !config.Valid() {
		return nil, ErrInvalidRequest
	}
	if config.Enabled && (classifier == nil || catalog == nil || selector == nil || recent == nil) {
		return nil, ErrInvalidRequest
	}
	return &Enricher{config: config, classifier: classifier, catalog: catalog, selector: selector, recent: recent}, nil
}

func (enricher *Enricher) Enrich(
	ctx context.Context,
	request agentrun.AssistantEnrichmentRequest,
) (agentrun.AssistantEnrichment, error) {
	if enricher == nil || !request.Valid() {
		return agentrun.AssistantEnrichment{}, ErrInvalidRequest
	}
	if !enricher.config.Enabled || !passesProbability(request.RunID, enricher.config.SendProbability) {
		return agentrun.AssistantEnrichment{}, nil
	}
	categories, err := enricher.catalog.Categories(ctx, enricher.config.PackID, enricher.config.PackVersion)
	if err != nil {
		return agentrun.AssistantEnrichment{}, err
	}
	classificationCtx, cancel := context.WithTimeout(ctx, enricher.config.ClassificationLimit)
	classification, err := enricher.classifier.Classify(classificationCtx, ClassificationRequest{
		Actor: request.Actor, RunID: request.RunID, ThreadID: request.ThreadID,
		InputMessageID: request.InputMessageID, UserContent: request.UserContent,
		AssistantContent: request.AssistantContent, Categories: categories,
	})
	cancel()
	if err != nil {
		return agentrun.AssistantEnrichment{}, err
	}
	candidates, err := enricher.catalog.Candidates(
		ctx, enricher.config.PackID, enricher.config.PackVersion, classification.Category,
	)
	if errors.Is(err, ErrNotFound) && classification.Category != enricher.config.DefaultCategory {
		classification.Category = enricher.config.DefaultCategory
		candidates, err = enricher.catalog.Candidates(
			ctx, enricher.config.PackID, enricher.config.PackVersion, classification.Category,
		)
	}
	if err != nil {
		return agentrun.AssistantEnrichment{}, err
	}
	recent, err := enricher.recent.RecentMemeIDs(
		ctx, request.Actor.UserID, request.ThreadID, enricher.config.AvoidRecentCount,
	)
	if err != nil {
		return agentrun.AssistantEnrichment{}, err
	}
	selected, err := enricher.selector.Select(ctx, SelectionRequest{
		RunID: request.RunID, ThreadID: request.ThreadID,
		Category: classification.Category, Candidates: candidates,
		RecentMemeIDs: recent, Maximum: enricher.config.MaxPerMessage,
		PolicyVersion: SelectionPolicyVersion,
	})
	if err != nil {
		return agentrun.AssistantEnrichment{}, err
	}
	result := agentrun.AssistantEnrichment{Memes: make([]agentrun.AssistantMemeDraft, 0, len(selected))}
	for position, asset := range selected {
		result.Memes = append(result.Memes, agentrun.AssistantMemeDraft{
			MemeID: asset.MemeID, PackID: asset.PackID, PackVersion: asset.PackVersion,
			Category: string(asset.Category), AssetKey: asset.AssetKey,
			ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
			Width: asset.Width, Height: asset.Height, ChecksumSHA256: asset.ChecksumSHA256,
			Position: position, ClassificationPolicyVersion: classification.PolicyVersion,
			SelectionPolicyVersion: SelectionPolicyVersion,
			ClassifierProvider:     classification.Provider, ClassifierModel: classification.Model,
		})
	}
	if !result.Valid() {
		return agentrun.AssistantEnrichment{}, ErrInvalidRequest
	}
	return result, nil
}

func passesProbability(runID string, probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	digest := sha256.Sum256([]byte(runID + "\x00meme-probability-v1"))
	value := binary.BigEndian.Uint64(digest[:8])
	return float64(value)/float64(^uint64(0)) < probability
}

var _ agentrun.AssistantEnricher = (*Enricher)(nil)
