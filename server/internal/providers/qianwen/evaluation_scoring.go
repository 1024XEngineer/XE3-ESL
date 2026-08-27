package qianwen

import (
	"context"
	"errors"
	"fmt"
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
		strings.TrimSpace(request.UserPrompt) == "" || !request.Report.Valid() {
		return textgeneration.Result{}, errors.New(
			"qianwen: invalid Evaluation scoring request",
		)
	}
	schema, err := evaluationReportSchema(request.Report)
	if err != nil {
		return textgeneration.Result{}, err
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
			Schema: schema,
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

func evaluationReportSchema(
	contract textgeneration.ReportContract,
) (map[string]any, error) {
	if !contract.Valid() {
		return nil, errors.New("qianwen: invalid Evaluation report contract")
	}
	dimensionKeys := make([]any, len(contract.DimensionKeys))
	for index, key := range contract.DimensionKeys {
		dimensionKeys[index] = key
	}
	requiredDimensionOrder := strings.Join(contract.DimensionKeys, ", ")
	evidence := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"turn_id", "quote", "occurrence"},
		"properties": map[string]any{
			"turn_id": map[string]any{
				"type": "string", "minLength": 36, "maxLength": 36,
			},
			"quote": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 16 * 1024,
			},
			"occurrence": map[string]any{"type": "integer", "minimum": 1},
		},
	}
	finding := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"message", "suggestion", "evidence"},
		"properties": map[string]any{
			"message": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 2048,
			},
			"suggestion": map[string]any{
				"type": "string", "maxLength": 2048,
			},
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
			"key": map[string]any{
				"type": "string", "enum": dimensionKeys,
				"description": "Use each requested dimension key exactly once in the requested order.",
			},
			"score": map[string]any{
				"anyOf": []any{
					map[string]any{
						"type": "number", "minimum": 0,
						"maximum": contract.ScoreMaximum,
					},
					map[string]any{"type": "null"},
				},
			},
			"coverage": map[string]any{
				"type": "number", "minimum": 0, "maximum": 1,
			},
			"confidence": map[string]any{
				"type": "number", "minimum": 0, "maximum": 1,
			},
			"reason_codes": map[string]any{
				"type": "array", "maxItems": 8,
				"items": map[string]any{
					"type": "string", "minLength": 1, "maxLength": 128,
					"pattern": `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
				},
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
			"dimension_key": map[string]any{
				"type": "string", "enum": dimensionKeys,
			},
			"improvement_index": map[string]any{
				"type": "integer", "minimum": 1, "maximum": 5,
			},
		},
	}
	// discipline: the provider's strict-schema subset has no positional-array
	// keyword. Exact cardinality + the dynamic enum + this ordered instruction
	// guide generation; normalizeProviderReport remains the fail-closed authority
	// for cross-item uniqueness and order.
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
			"summary": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 2048,
			},
			"dimensions": map[string]any{
				"type": "array", "minItems": len(contract.DimensionKeys),
				"maxItems": len(contract.DimensionKeys), "items": dimension,
				"description": fmt.Sprintf(
					"Return each key exactly once and in this order: %s.",
					requiredDimensionOrder,
				),
			},
			"priority_actions": map[string]any{
				"type": "array", "maxItems": 5, "items": priorityAction,
			},
		},
	}, nil
}

var _ textgeneration.Generator = (*EvaluationScoringGenerator)(nil)
