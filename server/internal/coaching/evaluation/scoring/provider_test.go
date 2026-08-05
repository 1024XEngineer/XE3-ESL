package scoring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestInterviewShadowTextProviderUsesStrictJSONRequest(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{
		result: TextGenerationResult{
			RequestID: "request-1",
			Provider:  "qianwen",
			Model:     "qwen-plus",
			Content:   `{"schema_version":"interview-scene-shadow-provider/v2","dimensions":[]}`,
		},
	}
	provider, err := NewInterviewShadowProvider(
		generator,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewInterviewShadowProvider: %v", err)
	}
	input := InterviewShadowProviderInput{
		SchemaVersion: InterviewShadowProviderSchemaVersion,
		PromptVersion: InterviewShadowPromptVersion,
		SceneType:     evaluation.SceneInterview,
		PracticeGoal:  "Explain relevant experience.",
		TaskBlueprints: []string{
			"motivation",
		},
		AssessableDimensions: []InterviewDimension{
			InterviewDimensionRelevance,
			InterviewDimensionStructure,
			InterviewDimensionEvidence,
			InterviewDimensionProfessional,
		},
		Opportunities: []InterviewProviderOpportunity{{
			QuestionID:   "question_1",
			QuestionType: "PRIMARY",
			QuestionText: "Why this role?",
			Response: &InterviewProviderResponse{
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
		result.RequestID != generator.result.RequestID ||
		string(result.Payload) != generator.result.Content {
		t.Fatalf("provider result = %#v", result)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("request count = %d", len(generator.requests))
	}
	request := generator.requests[0]
	if request.SystemPrompt != InterviewShadowSystemContract ||
		request.UserPrompt == "" {
		t.Fatalf("request = %#v", request)
	}
	if request.UserPrompt == request.SystemPrompt {
		t.Fatalf("evidence payload was not isolated: %#v", request)
	}
}

func TestInterviewShadowTextProviderRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{}
	for _, test := range []struct {
		name      string
		generator TextGenerator
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
		InterviewShadowProviderInput{},
	)
	if !errors.Is(err, want) {
		t.Fatalf("AnalyzeInterview error = %v", err)
	}
}

func TestIELTSSpeakingShadowTextProviderUsesStrictJSONRequest(t *testing.T) {
	t.Parallel()
	generator := &evaluationTextGenerator{
		result: TextGenerationResult{
			RequestID: "request-ielts-1",
			Provider:  "qianwen",
			Model:     "qwen-plus",
			Content:   `{"schema_version":"ielts-speaking-full-mock-shadow-provider/v1","criteria":[]}`,
		},
	}
	provider, err := NewIELTSSpeakingShadowProvider(
		generator,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewIELTSSpeakingShadowProvider: %v", err)
	}
	input := IELTSSpeakingShadowProviderInput{
		SchemaVersion: IELTSSpeakingShadowProviderSchemaVersion,
		PromptVersion: IELTSSpeakingShadowPromptVersion,
		RubricVersion: IELTSSpeakingShadowRubricVersion,
		SceneType:     evaluation.SceneIELTSSpeaking,
		PracticeMode:  "FULL_MOCK",
		AssessableCriteria: []IELTSCriterion{
			IELTSCriterionFC,
			IELTSCriterionLR,
			IELTSCriterionGRA,
		},
		RubricDescriptors: []IELTSRubricDescriptorSet{{
			CriterionID: IELTSCriterionLR,
			Descriptors: []IELTSRubricDescriptor{
				{
					ID:          "LR_PRACTICE_BAND_1",
					Band:        1,
					Description: "Uses only isolated words or memorised utterances.",
				},
			},
		}},
		Questions: []IELTSSpeakingProviderQuestion{{
			QuestionID:   "question_1",
			PartID:       IELTSPart1,
			Index:        1,
			QuestionText: "Where do you live?",
			Response: &IELTSSpeakingProviderResponse{
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
		result.RequestID != generator.result.RequestID ||
		string(result.Payload) != generator.result.Content {
		t.Fatalf("provider result = %#v", result)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("request count = %d", len(generator.requests))
	}
	request := generator.requests[0]
	if request.SystemPrompt != IELTSSpeakingShadowSystemContract ||
		request.UserPrompt == "" ||
		request.UserPrompt == request.SystemPrompt {
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
	generator := &evaluationTextGenerator{result: TextGenerationResult{
		RequestID: "request-general-1",
		Provider:  "qianwen",
		Model:     "qwen-plus",
		Content:   `{"schema_version":"general-scene-evaluation-provider/v1","dimensions":[]}`,
	}}
	provider, err := NewGeneralSceneProvider(generator, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.AnalyzeGeneralScene(
		context.Background(),
		GeneralSceneProviderInput{
			SchemaVersion:        GeneralSceneProviderSchemaVersion,
			PromptVersion:        GeneralScenePromptVersion,
			SceneType:            evaluation.SceneOverseasDaily,
			PracticeExperience:   "ROLEPLAY",
			SceneCategory:        "ROLEPLAY_DAILY",
			PracticeMode:         "FULL_SIMULATION",
			AssessableDimensions: GeneralSceneDimensions(),
			Opportunities: []GeneralSceneOpportunity{{
				QuestionID:   "question-1",
				QuestionType: "PRIMARY",
				QuestionText: "How can I help?",
				Response: &GeneralSceneResponse{
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
	if result.RequestID != generator.result.RequestID || len(generator.requests) != 1 {
		t.Fatalf("result=%#v requests=%#v", result, generator.requests)
	}
	request := generator.requests[0]
	if request.SystemPrompt != GeneralSceneSystemContract ||
		request.UserPrompt == "" {
		t.Fatalf("request = %#v", request)
	}
}

type evaluationTextGenerator struct {
	result   TextGenerationResult
	err      error
	requests []TextGenerationRequest
}

func (generator *evaluationTextGenerator) Generate(
	_ context.Context,
	request TextGenerationRequest,
) (TextGenerationResult, error) {
	generator.requests = append(generator.requests, request)
	return generator.result, generator.err
}
