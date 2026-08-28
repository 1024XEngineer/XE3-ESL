package qianwen

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type EvaluationScoringGenerator struct {
	generator *textClient
}

func NewEvaluationScoringGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationScoringGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &EvaluationScoringGenerator{generator: generator}, nil
}

func (generator *EvaluationScoringGenerator) Generate(
	ctx context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	if generator == nil || generator.generator == nil || ctx == nil ||
		strings.TrimSpace(request.SystemPrompt) == "" ||
		strings.TrimSpace(request.UserPrompt) == "" {
		return textgeneration.Result{}, errors.New(
			"qianwen: invalid Evaluation scoring request",
		)
	}
	generated, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: protocol.TextResponseFormatJSONSchema,
		ResponseSchema: &protocol.JSONSchemaDefinition{
			Name:   "evaluation_report",
			Strict: true,
			Schema: evaluationReportSchema(),
		},
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: generated.ID,
		Content:   generated.Content,
		Provider:  generated.Provider,
		Model:     generated.Model,
	}, nil
}

func evaluationReportSchema() map[string]any {
	evidence := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"turn_id", "quote", "occurrence"},
		"properties": map[string]any{
			"turn_id":    map[string]any{"type": "string"},
			"quote":      map[string]any{"type": "string"},
			"occurrence": map[string]any{"type": "integer", "minimum": 1},
		},
	}
	finding := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"message", "suggestion", "evidence"},
		"properties": map[string]any{
			"message":    map[string]any{"type": "string"},
			"suggestion": map[string]any{"type": "string"},
			"evidence": map[string]any{
				"type": "array", "maxItems": 8, "items": evidence,
			},
		},
	}
	findings := func() map[string]any {
		return map[string]any{
			"type": "array", "maxItems": 5, "items": finding,
		}
	}
	dimension := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []any{
			"key", "score", "coverage", "confidence", "reason_codes",
			"strengths", "improvements", "recommended_examples",
		},
		"properties": map[string]any{
			"key": map[string]any{"type": "string"},
			"score": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "number"},
					map[string]any{"type": "null"},
				},
			},
			"coverage":   map[string]any{"type": "number"},
			"confidence": map[string]any{"type": "number"},
			"reason_codes": map[string]any{
				"type": "array", "maxItems": 8,
				"items": map[string]any{"type": "string"},
			},
			"strengths":            findings(),
			"improvements":         findings(),
			"recommended_examples": findings(),
		},
	}
	priorityAction := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"dimension_key", "improvement_index"},
		"properties": map[string]any{
			"dimension_key": map[string]any{"type": "string"},
			"improvement_index": map[string]any{
				"type": "integer", "minimum": 1,
			},
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []any{
			"scoreability_status", "summary", "dimensions", "priority_actions",
		},
		"properties": map[string]any{
			"scoreability_status": map[string]any{
				"type": "string", "enum": []any{"PROVISIONAL", "INSUFFICIENT"},
			},
			"summary": map[string]any{"type": "string"},
			"dimensions": map[string]any{
				"type": "array", "minItems": 1, "maxItems": 8, "items": dimension,
			},
			"priority_actions": map[string]any{
				"type": "array", "maxItems": 5, "items": priorityAction,
			},
		},
	}
}

var _ textgeneration.Generator = (*EvaluationScoringGenerator)(nil)
