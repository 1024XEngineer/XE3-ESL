package textprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
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
	provider, err := NewInterviewShadowProvider(
		generator,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowProvider: %v", err)
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
		request.Messages[0].Content != evaluation.InterviewShadowSystemContract ||
		request.Messages[1].Role != ai.TextRoleUser {
		t.Fatalf("request = %#v", request)
	}
	if request.Messages[1].Content == "" ||
		request.Messages[1].Content == evaluation.InterviewShadowSystemContract {
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
			timeout:   MaxGenerationTimeout + time.Second,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewInterviewShadowProvider(
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
	provider, err := NewInterviewShadowProvider(
		&evaluationTextGenerator{err: want},
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowProvider: %v", err)
	}
	_, err = provider.AnalyzeInterview(
		context.Background(),
		evaluation.InterviewShadowProviderInput{},
	)
	if !errors.Is(err, want) {
		t.Fatalf("AnalyzeInterview error = %v", err)
	}
}

func TestIELTSSpeakingShadowTextProviderUsesStrictJSONRequest(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{
		result: ai.TextResult{
			ID:       "request-ielts-1",
			Provider: "qianwen",
			Model:    "qwen-plus",
			Content:  `{"schema_version":"ielts-speaking-full-mock-shadow-provider/v1","criteria":[]}`,
		},
	}
	provider, err := NewIELTSSpeakingShadowProvider(
		generator,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowProvider: %v", err)
	}
	input := evaluation.IELTSSpeakingShadowProviderInput{
		SchemaVersion: evaluation.IELTSSpeakingShadowProviderSchemaVersion,
		PromptVersion: evaluation.IELTSSpeakingShadowPromptVersion,
		RubricVersion: evaluation.IELTSSpeakingShadowRubricVersion,
		SceneType:     evaluation.SceneIELTSSpeaking,
		SceneModel:    "IELTS_SPEAKING_FULL_MOCK",
		AssessableCriteria: []evaluation.IELTSCriterion{
			evaluation.IELTSCriterionFC,
			evaluation.IELTSCriterionLR,
			evaluation.IELTSCriterionGRA,
		},
		RubricDescriptors: []evaluation.IELTSRubricDescriptorSet{{
			CriterionID: evaluation.IELTSCriterionLR,
			Descriptors: []evaluation.IELTSRubricDescriptor{
				"LR_PRACTICE_BAND_1",
			},
		}},
		Questions: []evaluation.IELTSSpeakingProviderQuestion{{
			QuestionID:   "question_1",
			PartID:       evaluation.IELTSPart1,
			Index:        1,
			QuestionText: "Where do you live?",
			Response: &evaluation.IELTSSpeakingProviderResponse{
				TurnID:        "turn_1",
				EvidenceRefID: "evidence_1",
				Transcript:    "I live in Shanghai.",
			},
		}},
	}
	result, err := provider.AnalyzeIELTSSpeaking(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("AnalyzeIELTSSpeaking: %v", err)
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
		request.Messages[0].Content !=
			evaluation.IELTSSpeakingShadowSystemContract ||
		request.Messages[1].Role != ai.TextRoleUser ||
		request.Messages[1].Content == "" ||
		request.Messages[1].Content ==
			evaluation.IELTSSpeakingShadowSystemContract {
		t.Fatalf("request = %#v", request)
	}
}

func TestIELTSSpeakingShadowTextProviderRejectsInvalidDependencies(
	t *testing.T,
) {
	t.Parallel()
	if _, err := NewIELTSSpeakingShadowProvider(
		nil,
		time.Second,
	); err == nil {
		t.Fatal("nil generator was accepted")
	}
	if _, err := NewIELTSSpeakingShadowProvider(
		&evaluationTextGenerator{},
		MaxGenerationTimeout+time.Second,
	); err == nil {
		t.Fatal("timeout above bound was accepted")
	}
}

func TestGeneralSceneTextProviderUsesEvaluationContract(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{result: ai.TextResult{
		ID:       "request-general-1",
		Provider: "qianwen",
		Model:    "qwen-plus",
		Content:  `{"schema_version":"general-scene-evaluation-provider/v1","dimensions":[]}`,
	}}
	provider, err := NewGeneralSceneProvider(generator, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.AnalyzeGeneralScene(
		context.Background(),
		evaluation.GeneralSceneProviderInput{
			SchemaVersion:        evaluation.GeneralSceneProviderSchemaVersion,
			PromptVersion:        evaluation.GeneralScenePromptVersion,
			SceneType:            evaluation.SceneOverseasDaily,
			SceneModel:           "DAILY_BASIC_DIALOGUE",
			AssessableDimensions: evaluation.GeneralSceneDimensions(),
			Opportunities: []evaluation.GeneralSceneOpportunity{{
				QuestionID:   "question-1",
				QuestionType: "PRIMARY",
				QuestionText: "How can I help?",
				Response: &evaluation.GeneralSceneResponse{
					TurnID:        "turn-1",
					EvidenceRefID: "evidence-1",
					Transcript:    "I need a quieter room.",
				},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != generator.result.ID || len(generator.requests) != 1 {
		t.Fatalf("result=%#v requests=%#v", result, generator.requests)
	}
	request := generator.requests[0]
	if err := ai.ValidateTextRequest(request); err != nil {
		t.Fatal(err)
	}
	if request.ResponseFormat != ai.TextResponseFormatJSON ||
		len(request.Messages) != 2 ||
		request.Messages[0].Content != evaluation.GeneralSceneSystemContract ||
		request.Messages[1].Role != ai.TextRoleUser ||
		request.Messages[1].Content == "" {
		t.Fatalf("request = %#v", request)
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
