package run

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxAssistantMemes = 4

var (
	assistantMemeStableIDPattern = regexp.MustCompile(
		`\A[A-Za-z0-9][A-Za-z0-9._:-]{0,127}\z`,
	)
	assistantMemeChecksumPattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
)

// AssistantEnrichmentRequest is the Run-owned input supplied only after the
// provider returned a valid final assistant reply. Actor is trusted server
// context; all text remains untrusted model or user content.
type AssistantEnrichmentRequest struct {
	Actor            requestcontext.Actor
	RunID            string
	ThreadID         string
	InputMessageID   string
	UserContent      string
	AssistantContent string
}

func (request AssistantEnrichmentRequest) Valid() bool {
	return request.Actor.Valid() &&
		ValidUUID(request.RunID) &&
		ValidUUID(request.ThreadID) &&
		ValidUUID(request.InputMessageID) &&
		conversation.ValidMessageContent(request.UserContent) &&
		conversation.ValidMessageContent(request.AssistantContent)
}

// AssistantMemeDraft is an immutable attachment projection selected by an
// Enricher. It deliberately stores an opaque relative asset key rather than a
// local path or expiring content URL.
type AssistantMemeDraft struct {
	MemeID                      string
	PackID                      string
	PackVersion                 string
	Category                    string
	AssetKey                    string
	ContentType                 string
	SizeBytes                   int64
	Width                       int
	Height                      int
	ChecksumSHA256              string
	Position                    int
	ClassificationPolicyVersion string
	SelectionPolicyVersion      string
	ClassifierProvider          string
	ClassifierModel             string
}

func (draft AssistantMemeDraft) Valid() bool {
	return assistantMemeStableIDPattern.MatchString(draft.MemeID) &&
		assistantMemeStableIDPattern.MatchString(draft.PackID) &&
		assistantMemeStableIDPattern.MatchString(draft.PackVersion) &&
		assistantMemeStableIDPattern.MatchString(draft.Category) &&
		validAssistantMemeAssetKey(draft.AssetKey) &&
		validAssistantMemeContentType(draft.ContentType) &&
		draft.SizeBytes > 0 &&
		draft.Width > 0 && draft.Width <= 16384 &&
		draft.Height > 0 && draft.Height <= 16384 &&
		assistantMemeChecksumPattern.MatchString(draft.ChecksumSHA256) &&
		draft.Position >= 0 && draft.Position < maxAssistantMemes &&
		assistantMemeStableIDPattern.MatchString(
			draft.ClassificationPolicyVersion,
		) &&
		assistantMemeStableIDPattern.MatchString(
			draft.SelectionPolicyVersion,
		) &&
		ValidProviderID(draft.ClassifierProvider) &&
		ValidModelID(draft.ClassifierModel)
}

func validAssistantMemeAssetKey(value string) bool {
	return value != "" &&
		value[0] != '/' &&
		!strings.Contains(value, `\`) &&
		path.Clean(value) == value &&
		value != "." && value != ".." &&
		!strings.HasPrefix(value, "../") &&
		len(value) <= 1024
}

func validAssistantMemeContentType(value string) bool {
	switch value {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

// AssistantEnrichment is the complete optional projection passed to the
// message persistence boundary. Empty enrichment is the valid no-op result.
type AssistantEnrichment struct {
	Memes []AssistantMemeDraft
}

func (enrichment AssistantEnrichment) Valid() bool {
	if len(enrichment.Memes) > maxAssistantMemes {
		return false
	}
	seen := make(map[string]struct{}, len(enrichment.Memes))
	for index, draft := range enrichment.Memes {
		if !draft.Valid() || draft.Position != index {
			return false
		}
		if _, duplicate := seen[draft.MemeID]; duplicate {
			return false
		}
		seen[draft.MemeID] = struct{}{}
	}
	return true
}

// AssistantEnricher is implemented by optional Agent-owned output modules.
// It must be deterministic for the same request and must not mutate the
// conversation, Run, or object storage itself.
type AssistantEnricher interface {
	Enrich(context.Context, AssistantEnrichmentRequest) (AssistantEnrichment, error)
}

// Completion is the single repository command for atomically committing the
// final assistant text, provider audit, and future structured attachments.
type Completion struct {
	Content    string
	Result     TextResult
	Enrichment AssistantEnrichment
}

func (completion Completion) Valid() bool {
	return completion.Content == completion.Result.Content &&
		validFinalTextResult(completion.Result) &&
		completion.Enrichment.Valid()
}
