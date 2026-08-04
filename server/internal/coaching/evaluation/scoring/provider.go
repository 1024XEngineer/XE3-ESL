package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

const MaxGenerationTimeout = 45 * time.Second

type interviewShadowTextProvider struct {
	generator TextGenerator
	timeout   time.Duration
}

func NewInterviewShadowProvider(
	generator TextGenerator,
	timeout time.Duration,
) (InterviewShadowProvider, error) {
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
	input InterviewShadowProviderInput,
) (InterviewShadowProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return InterviewShadowProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return InterviewShadowProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		TextGenerationRequest{
			SystemPrompt: InterviewShadowSystemContract,
			UserPrompt:   string(evidenceJSON),
		},
	)
	if err != nil {
		return InterviewShadowProviderResult{}, err
	}
	return InterviewShadowProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.RequestID,
	}, nil
}

var _ InterviewShadowProvider = (*interviewShadowTextProvider)(nil)

type ieltsSpeakingShadowTextProvider struct {
	generator TextGenerator
	timeout   time.Duration
}

func NewIELTSSpeakingShadowProvider(
	generator TextGenerator,
	timeout time.Duration,
) (IELTSSpeakingShadowProvider, error) {
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
	input IELTSSpeakingShadowProviderInput,
) (IELTSSpeakingShadowProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return IELTSSpeakingShadowProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return IELTSSpeakingShadowProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		TextGenerationRequest{
			SystemPrompt: IELTSSpeakingShadowSystemContract,
			UserPrompt:   string(evidenceJSON),
		},
	)
	if err != nil {
		return IELTSSpeakingShadowProviderResult{}, err
	}
	return IELTSSpeakingShadowProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.RequestID,
	}, nil
}

var _ IELTSSpeakingShadowProvider = (*ieltsSpeakingShadowTextProvider)(
	nil,
)

type generalSceneTextProvider struct {
	generator TextGenerator
	timeout   time.Duration
}

func NewGeneralSceneProvider(
	generator TextGenerator,
	timeout time.Duration,
) (GeneralSceneProvider, error) {
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
	input GeneralSceneProviderInput,
) (GeneralSceneProviderResult, error) {
	if provider == nil || provider.generator == nil || ctx == nil {
		return GeneralSceneProviderResult{},
			evaluation.ErrInvalidRequest
	}
	evidenceJSON, err := json.Marshal(input)
	if err != nil {
		return GeneralSceneProviderResult{}, err
	}
	generationContext, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	generated, err := provider.generator.Generate(
		generationContext,
		TextGenerationRequest{
			SystemPrompt: GeneralSceneSystemContract,
			UserPrompt:   string(evidenceJSON),
		},
	)
	if err != nil {
		return GeneralSceneProviderResult{}, err
	}
	return GeneralSceneProviderResult{
		Payload:   json.RawMessage(generated.Content),
		Provider:  generated.Provider,
		Model:     generated.Model,
		RequestID: generated.RequestID,
	}, nil
}

var _ GeneralSceneProvider = (*generalSceneTextProvider)(nil)
