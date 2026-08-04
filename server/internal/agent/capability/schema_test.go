package capability

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeInputFiltersUnknownFields(t *testing.T) {
	schema := ObjectSchema(map[string]any{
		"query": TextSchema("Query text.", 100),
		"options": ObjectSchema(map[string]any{
			"kind": StringEnumSchema("Result kind.", "review", "scenario"),
		}, nil),
	}, []string{"query"})
	input := json.RawMessage(
		`{"query":"last interview","unknown":true,` +
			`"options":{"kind":"review","nested_unknown":"drop"}}`,
	)

	normalized, err := NormalizeInput(schema, input)
	if err != nil {
		t.Fatalf("NormalizeInput() error = %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(normalized, &value); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, exists := value["unknown"]; exists {
		t.Fatalf("normalized input retained unknown field: %s", normalized)
	}
	options, ok := value["options"].(map[string]any)
	if !ok || options["kind"] != "review" {
		t.Fatalf("normalized options = %#v", value["options"])
	}
	if _, exists := options["nested_unknown"]; exists {
		t.Fatalf("normalized nested input retained unknown field: %s", normalized)
	}
}

func TestNormalizeInputValidatesSupportedConstraints(t *testing.T) {
	schema := ObjectSchema(map[string]any{
		"query": TextSchema("Query text.", 10),
		"kind":  StringEnumSchema("Result kind.", "review", "scenario"),
		"id":    IdentifierSchema("Stable result id."),
		"limit": IntegerRangeSchema("Maximum results.", 1, 5),
	}, []string{"query"})

	tests := map[string]json.RawMessage{
		"missing required":  json.RawMessage(`{}`),
		"blank text":        json.RawMessage(`{"query":"   "}`),
		"text too long":     json.RawMessage(`{"query":"12345678901"}`),
		"invalid enum":      json.RawMessage(`{"query":"ok","kind":"other"}`),
		"invalid id format": json.RawMessage(`{"query":"ok","id":"bad id"}`),
		"below minimum":     json.RawMessage(`{"query":"ok","limit":0}`),
		"above maximum":     json.RawMessage(`{"query":"ok","limit":6}`),
		"fractional integer": json.RawMessage(
			`{"query":"ok","limit":1.5}`,
		),
		"wrong type":    json.RawMessage(`{"query":123}`),
		"null value":    json.RawMessage(`{"query":null}`),
		"trailing json": json.RawMessage(`{"query":"ok"} {}`),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeInput(schema, input); !errors.Is(
				err,
				ErrInvalidInput,
			) {
				t.Fatalf("NormalizeInput() error = %v, want %v", err, ErrInvalidInput)
			}
		})
	}

	valid := json.RawMessage(
		`{"query":"review","kind":"review","id":"review-1","limit":5}`,
	)
	if _, err := NormalizeInput(schema, valid); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
}

func TestNormalizeInputRejectsInvalidSchemaDefinitions(t *testing.T) {
	tests := map[string]map[string]any{
		"missing type": {},
		"object missing properties": {
			"type": "object",
		},
		"required field not declared": ObjectSchema(
			map[string]any{"query": StringSchema("Query.")},
			[]string{"missing"},
		),
		"unsupported property type": ObjectSchema(
			map[string]any{
				"query": map[string]any{
					"type": "unknown",
				},
			},
			nil,
		),
		"unsupported format": ObjectSchema(
			map[string]any{
				"query": map[string]any{
					"type":   "string",
					"format": "unsupported",
				},
			},
			nil,
		),
		"empty enum": ObjectSchema(
			map[string]any{
				"kind": StringEnumSchema("Kind."),
			},
			nil,
		),
		"inverted range": ObjectSchema(
			map[string]any{
				"limit": IntegerRangeSchema("Limit.", 5, 1),
			},
			nil,
		),
		"malformed required": {
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             "query",
			"additionalProperties": false,
		},
	}
	for name, schema := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeInput(schema, json.RawMessage(`{}`))
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf(
					"NormalizeInput() error = %v, want %v",
					err,
					ErrInvalidDefinition,
				)
			}
		})
	}
}

func TestTextSchemaCountsUnicodeCharacters(t *testing.T) {
	schema := ObjectSchema(map[string]any{
		"query": TextSchema("Query.", 4),
	}, []string{"query"})
	if _, err := NormalizeInput(
		schema,
		json.RawMessage(`{"query":"中文测试"}`),
	); err != nil {
		t.Fatalf("four-rune text rejected: %v", err)
	}
	_, err := NormalizeInput(
		schema,
		json.RawMessage(`{"query":"中文测试长"}`),
	)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long text error = %v, want %v", err, ErrInvalidInput)
	}
}
