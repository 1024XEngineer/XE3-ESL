package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
)

func TestInterviewShadowTextProviderUsesStrictJSONRequest(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{
		result: ai.TextResult{
			ID:       "request-1",
			Provider: "qianwen",
			Model:    "qwen-plus",
			Content:  `{"schema_version":"interview-scene-shadow-provider/v2","dimensions":[]}`,
		},
	}
	provider, err := newInterviewShadowTextProvider(
		generator,
		time.Second,
	)
	if err != nil {
		t.Fatalf("newInterviewShadowTextProvider: %v", err)
	}
	input := evaluation.InterviewShadowProviderInput{
		SchemaVersion: evaluation.InterviewShadowProviderSchemaVersion,
		PromptVersion: evaluation.InterviewShadowPromptVersion,
		SceneType:     evaluation.SceneInterview,
		PracticeGoal:  "Explain relevant experience.",
		TaskBlueprints: []string{
			"motivation",
		},
		AssessableDimensions: []evaluation.InterviewDimension{
			evaluation.InterviewDimensionRelevance,
			evaluation.InterviewDimensionStructure,
			evaluation.InterviewDimensionEvidence,
			evaluation.InterviewDimensionProfessional,
		},
		Opportunities: []evaluation.InterviewProviderOpportunity{{
			QuestionID:   "question_1",
			QuestionType: "PRIMARY",
			QuestionText: "Why this role?",
			Response: &evaluation.InterviewProviderResponse{
				TurnID:        "turn_1",
				EvidenceRefID: "evidence_1",
				Transcript:    "I enjoy this work.",
			},
		}},
	}
	result, err := provider.AnalyzeInterview(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("AnalyzeInterview: %v", err)
	}
	if result.Provider != generator.result.Provider ||
		result.Model != generator.result.Model ||
		result.RequestID != generator.result.ID ||
		string(result.Payload) != generator.result.Content {
		t.Fatalf("provider result = %#v", result)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("request count = %d", len(generator.requests))
	}
	request := generator.requests[0]
	if err := ai.ValidateTextRequest(request); err != nil {
		t.Fatalf("generated request is invalid: %v", err)
	}
	if request.ResponseFormat != ai.TextResponseFormatJSON ||
		len(request.Messages) != 2 ||
		request.Messages[0].Role != ai.TextRoleSystem ||
		request.Messages[0].Content != interviewShadowSystemContract ||
		request.Messages[1].Role != ai.TextRoleUser {
		t.Fatalf("request = %#v", request)
	}
	if request.Messages[1].Content == "" ||
		request.Messages[1].Content == interviewShadowSystemContract {
		t.Fatalf("evidence payload was not isolated: %#v", request.Messages)
	}
}

func TestInterviewShadowTextProviderRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{}
	for _, test := range []struct {
		name      string
		generator ai.TextGenerator
		timeout   time.Duration
	}{
		{name: "nil generator", timeout: time.Second},
		{name: "zero timeout", generator: generator},
		{
			name:      "timeout above bound",
			generator: generator,
			timeout:   interviewShadowGenerationTimeout + time.Second,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newInterviewShadowTextProvider(
				test.generator,
				test.timeout,
			); err == nil {
				t.Fatal("invalid dependency was accepted")
			}
		})
	}
}

func TestInterviewShadowTextProviderPropagatesGenerationFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("provider unavailable")
	provider, err := newInterviewShadowTextProvider(
		&evaluationTextGenerator{err: want},
		time.Second,
	)
	if err != nil {
		t.Fatalf("newInterviewShadowTextProvider: %v", err)
	}
	_, err = provider.AnalyzeInterview(
		context.Background(),
		evaluation.InterviewShadowProviderInput{},
	)
	if !errors.Is(err, want) {
		t.Fatalf("AnalyzeInterview error = %v", err)
	}
}

type evaluationTextGenerator struct {
	result   ai.TextResult
	err      error
	requests []ai.TextRequest
}

func (generator *evaluationTextGenerator) Generate(
	_ context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	generator.requests = append(generator.requests, request)
	return generator.result, generator.err
}
