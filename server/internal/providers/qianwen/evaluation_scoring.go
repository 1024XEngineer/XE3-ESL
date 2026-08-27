package qianwen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

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
	evidence := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"turn_id", "quote", "occurrence"},
		"properties": map[string]any{
			"turn_id": map[string]any{
				"type":    "string",
				"pattern": `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
			},
			"quote":      utf8ByteBoundedStringSchema(16*1024, true),
			"occurrence": map[string]any{"type": "integer", "minimum": 1},
		},
	}
	finding := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"message", "suggestion", "evidence"},
		"properties": map[string]any{
			"message":    utf8ByteBoundedStringSchema(2048, true),
			"suggestion": utf8ByteBoundedStringSchema(2048, false),
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
	dimensionSlots := make(map[string]any, len(contract.DimensionKeys))
	requiredDimensionSlots := make([]any, len(contract.DimensionKeys))
	dimensionSlotOrder := make([]string, len(contract.DimensionKeys))
	for index, key := range contract.DimensionKeys {
		slot := fmt.Sprintf("dimension_%d", index+1)
		requiredDimensionSlots[index] = slot
		dimensionSlotOrder[index] = slot + "=" + key
		dimensionSlots[slot] = map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required": []any{
				"key", "score", "coverage", "confidence", "reason_codes",
				"strengths", "improvements", "recommended_examples",
			},
			"properties": map[string]any{
				"key": map[string]any{
					"type": "string", "enum": []any{key},
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
			"summary": utf8ByteBoundedStringSchema(2048, true),
			"dimensions": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": requiredDimensionSlots, "properties": dimensionSlots,
				"description": fmt.Sprintf(
					"Return the ordered dimension slots exactly as follows: %s.",
					strings.Join(dimensionSlotOrder, ", "),
				),
			},
			"priority_actions": map[string]any{
				"type": "array", "maxItems": 5, "items": priorityAction,
			},
		},
	}, nil
}

func utf8ByteBoundedStringSchema(maximumBytes int, requireNonEmpty bool) map[string]any {
	schema := map[string]any{
		"type": "string", "maxLength": maximumBytes / utf8.UTFMax,
	}
	if requireNonEmpty {
		schema["minLength"] = 1
	}
	return schema
}

var _ textgeneration.Generator = (*EvaluationScoringGenerator)(nil)
