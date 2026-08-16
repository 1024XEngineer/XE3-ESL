package qianwen

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/fieldextractor"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type SummaryGenerator struct {
	generator *textClient
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
		&protocol.JSONSchemaDefinition{
			Name:   "conversation_summary",
			Strict: true,
			Schema: conversationSummarySchema(),
		},
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
		nil,
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
		&protocol.JSONSchemaDefinition{
			Name:   "resume_fields",
			Strict: true,
			Schema: resumeFieldsSchema(),
		},
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
	schema *protocol.JSONSchemaDefinition,
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
	request := protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: systemPrompt},
			{Role: protocol.TextRoleUser, Content: userPrompt},
		},
	}
	if schema != nil {
		request.ResponseFormat = protocol.TextResponseFormatJSONSchema
		request.ResponseSchema = schema
	}
	return generator.Generate(ctx, request)
}

func conversationSummarySchema() map[string]any {
	properties := make(map[string]any, 6)
	for _, name := range []string{
		"current_intents", "background", "progress", "decisions",
		"open_questions", "next_steps",
	} {
		properties[name] = stringArraySchema(6)
	}
	return strictObjectSchema(
		[]any{
			"current_intents", "background", "progress", "decisions",
			"open_questions", "next_steps",
		},
		properties,
	)
}

func resumeFieldsSchema() map[string]any {
	workExperience := strictObjectSchema(
		[]any{
			"company", "position", "start_date", "end_date", "duties",
			"achievements",
		},
		map[string]any{
			"company":      stringSchema(),
			"position":     stringSchema(),
			"start_date":   stringSchema(),
			"end_date":     stringSchema(),
			"duties":       stringArraySchema(0),
			"achievements": stringArraySchema(0),
		},
	)
	projectExperience := strictObjectSchema(
		[]any{
			"project_name", "role", "description", "technologies", "duties",
			"achievements",
		},
		map[string]any{
			"project_name": stringSchema(),
			"role":         stringSchema(),
			"description":  stringSchema(),
			"technologies": stringArraySchema(0),
			"duties":       stringArraySchema(0),
			"achievements": stringArraySchema(0),
		},
	)
	educationExperience := strictObjectSchema(
		[]any{"school", "major", "degree", "gpa", "start_date", "end_date"},
		map[string]any{
			"school":     stringSchema(),
			"major":      stringSchema(),
			"degree":     stringSchema(),
			"gpa":        stringSchema(),
			"start_date": stringSchema(),
			"end_date":   stringSchema(),
		},
	)
	return strictObjectSchema(
		[]any{
			"target_position", "professional_summary", "work_experiences",
			"project_experiences", "education_experiences", "skills", "awards",
		},
		map[string]any{
			"target_position":      stringSchema(),
			"professional_summary": stringSchema(),
			"work_experiences":     objectArraySchema(workExperience, 0),
			"project_experiences":  objectArraySchema(projectExperience, 0),
			"education_experiences": objectArraySchema(
				educationExperience,
				0,
			),
			"skills": stringArraySchema(0),
			"awards": stringArraySchema(0),
		},
	)
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
	_ summary.Generator              = (*SummaryGenerator)(nil)
	_ preparation.JobTargetGenerator = (*PreparationJobTargetGenerator)(nil)
	_ fieldextractor.Generator       = (*ResumeFieldGenerator)(nil)
)
