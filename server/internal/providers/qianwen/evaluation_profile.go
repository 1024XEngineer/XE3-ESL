package qianwen

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type EvaluationProfileGenerator struct{ generator *textClient }

func NewEvaluationProfileGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationProfileGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &EvaluationProfileGenerator{generator: generator}, nil
}

func (generator *EvaluationProfileGenerator) Generate(
	ctx context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	if generator == nil || generator.generator == nil || ctx == nil ||
		strings.TrimSpace(request.SystemPrompt) == "" ||
		strings.TrimSpace(request.UserPrompt) == "" {
		return textgeneration.Result{}, errors.New("qianwen: invalid Evaluation profile request")
	}
	generated, err := generator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: protocol.TextResponseFormatJSONSchema,
		ResponseSchema: &protocol.JSONSchemaDefinition{
			Name: "ielts_cumulative_profile", Strict: true,
			Schema: ieltsCumulativeProfileSchema(),
		},
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: generated.ID, Content: generated.Content,
		Provider: generated.Provider, Model: generated.Model,
	}, nil
}

func ieltsCumulativeProfileSchema() map[string]any {
	evidence := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"turn_id", "quote", "occurrence", "part"},
		"properties": map[string]any{
			"turn_id":    map[string]any{"type": "string"},
			"quote":      map[string]any{"type": "string", "maxLength": 4096},
			"occurrence": map[string]any{"type": "integer", "minimum": 1},
			"part":       map[string]any{"type": "integer", "enum": []any{1, 2}},
		},
	}
	observation := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"kind", "reason_code", "evidence"},
		"properties": map[string]any{
			"kind":        map[string]any{"type": "string", "enum": []any{"STRENGTH", "IMPROVEMENT"}},
			"reason_code": map[string]any{"type": "string"},
			"evidence":    map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": evidence},
		},
	}
	dimension := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"key", "provisional_band_low", "provisional_band_high", "coverage", "confidence", "observations"},
		"properties": map[string]any{
			"key":                   map[string]any{"type": "string"},
			"provisional_band_low":  map[string]any{"type": "number", "minimum": 0, "maximum": 9},
			"provisional_band_high": map[string]any{"type": "number", "minimum": 0, "maximum": 9},
			"coverage":              map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"confidence":            map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"observations":          map[string]any{"type": "array", "maxItems": 3, "items": observation},
		},
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"completed_parts", "dimensions"},
		"properties": map[string]any{
			"completed_parts": map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]any{"type": "integer", "enum": []any{1, 2}}},
			"dimensions":      map[string]any{"type": "array", "minItems": 4, "maxItems": 4, "items": dimension},
		},
	}
}

var _ textgeneration.Generator = (*EvaluationProfileGenerator)(nil)
