package textprovider

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

const MaxGenerationTimeout = 45 * time.Second

type interviewShadowTextProvider struct {
	generator ai.TextGenerator
	timeout   time.Duration
}

func NewInterviewShadowProvider(
	generator ai.TextGenerator,
	timeout time.Duration,
) (evaluation.InterviewShadowProvider, error) {
	if generator == nil || timeout <= 0 ||
		timeout > MaxGenerationTimeout {
		return nil, errors.New(
			"evaluation: Interview shadow provider dependencies are required",
		)
	}
	return &interviewShadowTextProvider{
		generator: generator,
		timeout:   timeout,
	}, nil
}

func (provider *interviewShadowTextProvider) AnalyzeInterview(
	ctx context.Context,
	input evaluation.InterviewShadowProviderInput,
) (evaluation.InterviewShadowProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return evaluation.InterviewShadowProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return evaluation.InterviewShadowProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		ai.TextRequest{
			Messages: []ai.TextMessage{
				{
					Role:    ai.TextRoleSystem,
					Content: evaluation.InterviewShadowSystemContract,
				},
				{
					Role:    ai.TextRoleUser,
					Content: string(evidenceJSON),
				},
			},
			ResponseFormat: ai.TextResponseFormatJSON,
		},
	)
	if err != nil {
		return evaluation.InterviewShadowProviderResult{}, err
	}
	return evaluation.InterviewShadowProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.ID,
	}, nil
}

var _ evaluation.InterviewShadowProvider = (*interviewShadowTextProvider)(nil)

type ieltsSpeakingShadowTextProvider struct {
	generator ai.TextGenerator
	timeout   time.Duration
}

func NewIELTSSpeakingShadowProvider(
	generator ai.TextGenerator,
	timeout time.Duration,
) (evaluation.IELTSSpeakingShadowProvider, error) {
	if generator == nil || timeout <= 0 ||
		timeout > MaxGenerationTimeout {
		return nil, errors.New(
			"evaluation: IELTS Speaking shadow provider dependencies are required",
		)
	}
	return &ieltsSpeakingShadowTextProvider{
		generator: generator,
		timeout:   timeout,
	}, nil
}

func (provider *ieltsSpeakingShadowTextProvider) AnalyzeIELTSSpeaking(
	ctx context.Context,
	input evaluation.IELTSSpeakingShadowProviderInput,
) (evaluation.IELTSSpeakingShadowProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return evaluation.IELTSSpeakingShadowProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return evaluation.IELTSSpeakingShadowProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		ai.TextRequest{
			Messages: []ai.TextMessage{
				{
					Role:    ai.TextRoleSystem,
					Content: evaluation.IELTSSpeakingShadowSystemContract,
				},
				{
					Role:    ai.TextRoleUser,
					Content: string(evidenceJSON),
				},
			},
			ResponseFormat: ai.TextResponseFormatJSON,
		},
	)
	if err != nil {
		return evaluation.IELTSSpeakingShadowProviderResult{}, err
	}
	return evaluation.IELTSSpeakingShadowProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.ID,
	}, nil
}

var _ evaluation.IELTSSpeakingShadowProvider = (*ieltsSpeakingShadowTextProvider)(
	nil,
)

type generalSceneTextProvider struct {
	generator ai.TextGenerator
	timeout   time.Duration
}

func NewGeneralSceneProvider(
	generator ai.TextGenerator,
	timeout time.Duration,
) (evaluation.GeneralSceneProvider, error) {
	if generator == nil || timeout <= 0 ||
		timeout > MaxGenerationTimeout {
		return nil, errors.New(
			"evaluation: general Scene provider dependencies are required",
		)
	}
	return &generalSceneTextProvider{generator: generator, timeout: timeout}, nil
}

func (provider *generalSceneTextProvider) AnalyzeGeneralScene(
	ctx context.Context,
	input evaluation.GeneralSceneProviderInput,
) (evaluation.GeneralSceneProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return evaluation.GeneralSceneProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return evaluation.GeneralSceneProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		ai.TextRequest{
			Messages: []ai.TextMessage{
				{Role: ai.TextRoleSystem, Content: evaluation.GeneralSceneSystemContract},
				{Role: ai.TextRoleUser, Content: string(evidenceJSON)},
			},
			ResponseFormat: ai.TextResponseFormatJSON,
		},
	)
	if err != nil {
		return evaluation.GeneralSceneProviderResult{}, err
	}
	return evaluation.GeneralSceneProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.ID,
	}, nil
}

var _ evaluation.GeneralSceneProvider = (*generalSceneTextProvider)(nil)
