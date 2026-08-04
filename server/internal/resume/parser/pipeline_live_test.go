package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/document"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/parser"
)

// TestPipelineLiveQianwen opt-in 验证真实文本 PDF 和服务端模型配置的完整链路。
func TestPipelineLiveQianwen(t *testing.T) {
	if os.Getenv("QIANWEN_RESUME_LIVE_TEST") != "1" {
		t.Skip("set QIANWEN_RESUME_LIVE_TEST=1 to run")
	}
	path := os.Getenv("QIANWEN_RESUME_LIVE_TEST_PDF")
	if path == "" {
		t.Fatal("QIANWEN_RESUME_LIVE_TEST_PDF is required")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open live PDF: %v", err)
	}
	defer file.Close()
	textConfiguration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation configuration: %v", err)
	}
	resumeTextConfiguration := textConfiguration
	if resumeTextConfiguration.MaxOutputTokens < 4096 {
		resumeTextConfiguration.MaxOutputTokens = 4096
	}
	generator, err := bootstrap.NewTextGenerator(resumeTextConfiguration)
	if err != nil {
		t.Fatalf("new text generator: %v", err)
	}
	recorder := &liveGeneratorRecorder{delegate: generator}
	fields, err := fieldextractor.NewLLMExtractor(
		recorder,
		fieldextractor.Config{
			Provider:              textConfiguration.Provider,
			Model:                 textConfiguration.Model,
			MaxDocumentCharacters: textConfiguration.MaxContextChars,
		},
	)
	if err != nil {
		t.Fatalf("new field extractor: %v", err)
	}
	pipeline, err := parser.NewPipeline(document.NewTextPDFParser(), fields)
	if err != nil {
		t.Fatalf("new parser pipeline: %v", err)
	}
	content, err := pipeline.Parse(context.Background(), file)
	if err != nil {
		t.Logf(
			"provider_match=%v model_match=%v finish=%q response_bytes=%d trimmed=%v starts_object=%v code_fence=%v ends_object=%v json_keys=%v",
			recorder.result.Provider == textConfiguration.Provider,
			recorder.result.Model == textConfiguration.Model,
			recorder.result.FinishReason,
			len(recorder.result.Content),
			recorder.result.Content == strings.TrimSpace(recorder.result.Content),
			strings.HasPrefix(strings.TrimSpace(recorder.result.Content), "{"),
			strings.HasPrefix(strings.TrimSpace(recorder.result.Content), "```"),
			strings.HasSuffix(strings.TrimSpace(recorder.result.Content), "}"),
			topLevelJSONKeys(recorder.result.Content),
		)
		t.Fatalf("parse live PDF: %v", err)
	}
	if content.TargetPosition == "" && content.ProfessionalSummary == "" &&
		len(content.WorkExperiences) == 0 &&
		len(content.ProjectExperiences) == 0 &&
		len(content.EducationExperiences) == 0 && len(content.Skills) == 0 {
		t.Fatal("live extraction returned empty content")
	}
}

type liveGeneratorRecorder struct {
	delegate ai.TextGenerator
	result   ai.TextResult
}

func (recorder *liveGeneratorRecorder) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	result, err := recorder.delegate.Generate(ctx, request)
	recorder.result = result
	return result, err
}

func topLevelJSONKeys(content string) []string {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &object) != nil {
		return []string{"<invalid-json>"}
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
