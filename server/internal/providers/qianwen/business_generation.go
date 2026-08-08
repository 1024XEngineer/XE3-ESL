package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	conversationtitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
)

type MemoryGenerator struct {
	generator *textClient
}

func NewMemoryGenerator(
	configuration TextConfig,
	apiKey string,
) (*MemoryGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &MemoryGenerator{generator: generator}, nil
}

func (generator *MemoryGenerator) GenerateJSON(
	ctx context.Context,
	request memory.GenerationRequest,
) (memory.GenerationResult, error) {
	if generator == nil {
		return memory.GenerationResult{}, missingBusinessGenerator()
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		request.SystemPrompt,
		request.UserPrompt,
		protocol.TextResponseFormatJSON,
	)
	if err != nil {
		return memory.GenerationResult{}, err
	}
	return memory.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, nil
}

type SummaryGenerator struct {
	generator *textClient
}

type TitleGenerator struct {
	generator *textClient
}

func NewTitleGenerator(
	configuration TextConfig,
	apiKey string,
) (*TitleGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &TitleGenerator{generator: generator}, nil
}

func (generator *TitleGenerator) GenerateJSON(
	ctx context.Context,
	request conversationtitle.GenerationRequest,
) (conversationtitle.GenerationResult, error) {
	if generator == nil {
		return conversationtitle.GenerationResult{}, missingBusinessGenerator()
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		request.SystemPrompt,
		request.UserPrompt,
		protocol.TextResponseFormatJSON,
	)
	if err != nil {
		return conversationtitle.GenerationResult{}, err
	}
	return conversationtitle.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, nil
}

func NewSummaryGenerator(
	configuration TextConfig,
	apiKey string,
) (*SummaryGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &SummaryGenerator{generator: generator}, nil
}

func (generator *SummaryGenerator) GenerateJSON(
	ctx context.Context,
	request summary.GenerationRequest,
) (summary.GenerationResult, error) {
	if generator == nil {
		return summary.GenerationResult{}, missingBusinessGenerator()
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		request.SystemPrompt,
		request.UserPrompt,
		protocol.TextResponseFormatJSON,
	)
	if err != nil {
		return summary.GenerationResult{}, err
	}
	return summary.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, nil
}

type PreparationJobTargetGenerator struct {
	generator *textClient
}

func NewPreparationJobTargetGenerator(
	configuration TextConfig,
	apiKey string,
) (*PreparationJobTargetGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &PreparationJobTargetGenerator{generator: generator}, nil
}

func (generator *PreparationJobTargetGenerator) GenerateJobTarget(
	ctx context.Context,
	request preparation.JobTargetGenerationRequest,
) (preparation.JobTargetGenerationResult, error) {
	if generator == nil {
		return preparation.JobTargetGenerationResult{}, missingBusinessGenerator()
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		request.SystemInstruction,
		request.UserMaterial,
		protocol.TextResponseFormatDefault,
	)
	if err != nil {
		return preparation.JobTargetGenerationResult{}, err
	}
	return preparation.JobTargetGenerationResult{Content: result.Content}, nil
}

type ResumeFieldGenerator struct {
	generator *textClient
}

func NewResumeFieldGenerator(
	configuration TextConfig,
	apiKey string,
) (*ResumeFieldGenerator, error) {
	if configuration.MaxOutputTokens <
		fieldextractor.MinimumGenerationOutputTokens {
		return nil, errors.New(
			"qianwen: Resume output budget is below the required minimum",
		)
	}
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &ResumeFieldGenerator{generator: generator}, nil
}

func (generator *ResumeFieldGenerator) GenerateJSON(
	ctx context.Context,
	request fieldextractor.GenerationRequest,
) (fieldextractor.GenerationResult, error) {
	if generator == nil {
		return fieldextractor.GenerationResult{}, missingBusinessGenerator()
	}
	if request.MinimumOutputTokens !=
		fieldextractor.MinimumGenerationOutputTokens {
		return fieldextractor.GenerationResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("qianwen: invalid Resume output budget"),
		)
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		request.SystemPrompt,
		request.DocumentPayload,
		protocol.TextResponseFormatJSON,
	)
	if err != nil {
		return fieldextractor.GenerationResult{}, err
	}
	return fieldextractor.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, nil
}

func generateBusinessText(
	ctx context.Context,
	generator *textClient,
	systemPrompt string,
	userPrompt string,
	format protocol.TextResponseFormat,
) (protocol.TextResult, error) {
	if generator == nil {
		return protocol.TextResult{}, protocol.NewGenerationError(
			protocol.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("qianwen: business generator is required"),
		)
	}
	return generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: systemPrompt},
			{Role: protocol.TextRoleUser, Content: userPrompt},
		},
		ResponseFormat: format,
	})
}

func missingBusinessGenerator() error {
	return protocol.NewGenerationError(
		protocol.ErrorConfiguration,
		0,
		"",
		"",
		errors.New("qianwen: business generator is required"),
	)
}

var (
	_ memory.Generator               = (*MemoryGenerator)(nil)
	_ summary.Generator              = (*SummaryGenerator)(nil)
	_ preparation.JobTargetGenerator = (*PreparationJobTargetGenerator)(nil)
	_ fieldextractor.Generator       = (*ResumeFieldGenerator)(nil)
)
