package resume_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
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
	generator, err := bootstrap.NewResumeFieldGenerator(textConfiguration)
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
			"provider_match=%v model_match=%v response_bytes=%d trimmed=%v starts_object=%v code_fence=%v ends_object=%v json_keys=%v",
			recorder.result.Provider == textConfiguration.Provider,
			recorder.result.Model == textConfiguration.Model,
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
		len(content.EducationExperiences) == 0 && len(content.Skills) == 0 &&
		len(content.Awards) == 0 {
		t.Fatal("live extraction returned empty content")
	}
	if expected := os.Getenv("QIANWEN_RESUME_EXPECT_GPA"); expected != "" &&
		!containsGPA(content, expected) {
		t.Fatalf("live extraction did not preserve expected GPA %q", expected)
	}
	for _, expected := range strings.Split(os.Getenv("QIANWEN_RESUME_EXPECT_AWARDS"), "|") {
		if expected != "" && !containsString(content.Awards, expected) {
			t.Fatalf("live extraction did not preserve expected award %q", expected)
		}
	}
}

func containsGPA(content resume.Content, expected string) bool {
	for _, education := range content.EducationExperiences {
		if education.GPA == expected {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type liveGeneratorRecorder struct {
	delegate fieldextractor.Generator
	result   fieldextractor.GenerationResult
}

func (recorder *liveGeneratorRecorder) GenerateJSON(
	ctx context.Context,
	request fieldextractor.GenerationRequest,
) (fieldextractor.GenerationResult, error) {
	result, err := recorder.delegate.GenerateJSON(ctx, request)
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
