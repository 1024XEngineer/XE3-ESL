package qiniu

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTextConstructorsUseQiniuCompatibilityBoundary(t *testing.T) {
	configuration := TextConfig{
		BaseURL: "https://api.qnaigc.com/v1", Model: "moonshotai/kimi-k2.6",
		Timeout: time.Second, MaxOutputTokens: 8192,
	}
	constructors := []struct {
		name string
		new  func() (any, error)
	}{
		{name: "agent run", new: func() (any, error) { return NewAgentRunGenerator(configuration, "qiniu-key") }},
		{name: "memory", new: func() (any, error) { return NewMemoryGenerator(configuration, "qiniu-key") }},
		{name: "summary", new: func() (any, error) { return NewSummaryGenerator(configuration, "qiniu-key") }},
		{name: "title", new: func() (any, error) { return NewTitleGenerator(configuration, "qiniu-key") }},
		{name: "translation", new: func() (any, error) { return NewTranslator(configuration, "qiniu-key") }},
		{name: "preparation", new: func() (any, error) { return NewPreparationJobTargetGenerator(configuration, "qiniu-key") }},
		{name: "IELTS answer", new: func() (any, error) { return NewIELTSAnswerPreparationGenerator(configuration, "qiniu-key") }},
		{name: "evaluation", new: func() (any, error) { return NewEvaluationScoringGenerator(configuration, "qiniu-key") }},
		{name: "speech feedback", new: func() (any, error) { return NewEvaluationSpeechFeedbackGenerator(configuration, "qiniu-key") }},
		{name: "resume", new: func() (any, error) { return NewResumeFieldGenerator(configuration, "qiniu-key") }},
	}
	for _, constructor := range constructors {
		created, err := constructor.new()
		if err != nil || created == nil {
			t.Fatalf("Qiniu %s constructor = %T, %v", constructor.name, created, err)
		}
	}
}

func TestTextConstructorRejectsUnsafeEndpointWithoutLeakingAPIKey(t *testing.T) {
	const apiKey = "must-never-appear"
	generator, err := NewAgentRunGenerator(TextConfig{
		BaseURL: "https://example.com/v1", Model: "moonshotai/kimi-k2.6",
		Timeout: time.Second, MaxOutputTokens: 512,
	}, apiKey)
	if err == nil || generator != nil {
		t.Fatalf("unsafe Qiniu endpoint returned generator=%T error=%v", generator, err)
	}
	if strings.Contains(fmt.Sprint(err), apiKey) || !strings.Contains(fmt.Sprint(err), "Qiniu") {
		t.Fatalf("unsafe Qiniu endpoint error = %q", err)
	}
}
