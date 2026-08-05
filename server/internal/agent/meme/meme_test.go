package meme

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestBundledOfficialSamplePackPassesManifestValidation(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Join(filepath.Dir(current), "..", "..", "..", "assets", "memes")
	catalog, err := NewFileCatalog(root, "official-001", "1.0.0")
	if err != nil {
		t.Fatalf("NewFileCatalog: %v", err)
	}
	categories, err := catalog.Categories(context.Background(), "official-001", "1.0.0")
	if err != nil || len(categories) != 18 {
		t.Fatalf("Categories = %v, %v", categories, err)
	}
	assets, err := catalog.Candidates(context.Background(), "official-001", "1.0.0", "happy")
	if err != nil || len(assets) != 1 {
		t.Fatalf("happy assets = %v, %v", assets, err)
	}
	file, opened, err := catalog.Open(assets[0].AssetKey)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	if opened.ChecksumSHA256 != assets[0].ChecksumSHA256 {
		t.Fatal("opened asset differs from admitted manifest asset")
	}
}

func TestDeterministicSelectorAvoidsRecentAndIsRetryStable(t *testing.T) {
	request := SelectionRequest{
		RunID: "run-1", ThreadID: "thread-1", Category: "happy",
		Candidates: []Asset{
			{MemeID: "a", Category: "happy", Weight: 1},
			{MemeID: "b", Category: "happy", Weight: 1},
		},
		RecentMemeIDs: []string{"a"}, Maximum: 1,
		PolicyVersion: SelectionPolicyVersion,
	}
	first, err := (DeterministicSelector{}).Select(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (DeterministicSelector{}).Select(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].MemeID != "b" || second[0].MemeID != first[0].MemeID {
		t.Fatalf("selection is not recent-safe and stable: %v / %v", first, second)
	}
}

func TestToolClassifierUsesOnlySpecificMemeTool(t *testing.T) {
	generator := &capturingGenerator{result: agentrun.TextResult{
		Provider: "qianwen", Model: "qwen-plus",
		ToolCalls: []agentrun.ModelToolCall{{
			ID: "call-1", Name: classificationToolName,
			Arguments: json.RawMessage(`{"category":"happy"}`),
		}},
	}}
	classifier, err := NewToolClassifier(generator)
	if err != nil {
		t.Fatal(err)
	}
	result, err := classifier.Classify(context.Background(), ClassificationRequest{
		Actor: requestcontext.Actor{UserID: "owner", SessionID: "session"},
		RunID: "run", ThreadID: "thread", InputMessageID: "message",
		UserContent: "I passed!", AssistantContent: "Fantastic work!",
		Categories: []CategoryDefinition{
			{Category: "happy", Description: "celebration"},
			{Category: "sad", Description: "regret"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Category != "happy" || generator.request.ToolChoice.Mode != agentrun.ToolChoiceSpecific ||
		generator.request.ToolChoice.Name != classificationToolName || len(generator.request.Tools) != 1 {
		t.Fatalf("classifier request/result = %#v / %#v", generator.request, result)
	}
	if len(generator.request.Messages) != 2 || generator.request.Messages[0].Role != agentrun.TextRoleSystem {
		t.Fatal("classifier must use its own isolated prompt")
	}
}

func TestEnricherBuildsOneDraftAndZeroProbabilitySkipsClassifier(t *testing.T) {
	asset := Asset{
		MemeID: "official-001:happy:01", PackID: "official-001",
		PackVersion: "1.0.0", Category: "happy",
		AssetKey:    "official-001/1.0.0/memes/happy/one.jpg",
		ContentType: "image/jpeg", SizeBytes: 12, Width: 2, Height: 3,
		ChecksumSHA256: strings.Repeat("a", 64), Weight: 1,
	}
	classifier := &fixedClassifier{result: Classification{
		Category: "happy", PolicyVersion: ClassificationPolicyVersion,
		Provider: "qianwen", Model: "qwen-plus",
	}}
	config := Config{
		Enabled: true, SendProbability: 1, MaxPerMessage: 1,
		AvoidRecentCount: 3, ClassificationLimit: time.Second,
		DefaultCategory: "happy", PackID: "official-001", PackVersion: "1.0.0",
	}
	enricher, err := NewEnricher(
		config, classifier, &fixedCatalog{asset: asset},
		DeterministicSelector{}, fixedRecentReader{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := enricher.Enrich(context.Background(), validEnrichmentRequest())
	if err != nil || len(result.Memes) != 1 || result.Memes[0].MemeID != asset.MemeID {
		t.Fatalf("Enrich = %#v, %v", result, err)
	}
	config.SendProbability = 0
	skipping, err := NewEnricher(
		config, classifier, &fixedCatalog{asset: asset},
		DeterministicSelector{}, fixedRecentReader{},
	)
	if err != nil {
		t.Fatal(err)
	}
	before := classifier.calls
	result, err = skipping.Enrich(context.Background(), validEnrichmentRequest())
	if err != nil || len(result.Memes) != 0 || classifier.calls != before {
		t.Fatalf("zero probability did not skip: %#v, %v", result, err)
	}
}

func validEnrichmentRequest() agentrun.AssistantEnrichmentRequest {
	return agentrun.AssistantEnrichmentRequest{
		Actor:          requestcontext.Actor{UserID: "owner", SessionID: "session"},
		RunID:          "10000000-0000-4000-8000-000000000001",
		ThreadID:       "10000000-0000-4000-8000-000000000002",
		InputMessageID: "10000000-0000-4000-8000-000000000003",
		UserContent:    "I passed!", AssistantContent: "Fantastic work!",
	}
}

type capturingGenerator struct {
	request agentrun.TextRequest
	result  agentrun.TextResult
}

func (generator *capturingGenerator) Generate(
	_ context.Context,
	request agentrun.TextRequest,
) (agentrun.TextResult, error) {
	generator.request = request
	return generator.result, nil
}

type fixedClassifier struct {
	result Classification
	calls  int
}

func (classifier *fixedClassifier) Classify(
	context.Context,
	ClassificationRequest,
) (Classification, error) {
	classifier.calls++
	return classifier.result, nil
}

type fixedCatalog struct{ asset Asset }

func (catalog *fixedCatalog) Categories(context.Context, string, string) ([]CategoryDefinition, error) {
	return []CategoryDefinition{{Category: catalog.asset.Category, Description: "celebration"}}, nil
}

func (catalog *fixedCatalog) Candidates(context.Context, string, string, Category) ([]Asset, error) {
	return []Asset{catalog.asset}, nil
}

type fixedRecentReader struct{}

func (fixedRecentReader) RecentMemeIDs(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
