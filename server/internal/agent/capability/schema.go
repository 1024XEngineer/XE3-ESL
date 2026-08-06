// Package capability defines the provider-neutral contract for business
// capabilities exposed to the model as tools.
package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Risk string

const (
	RiskReadOnly     Risk = "read_only"
	RiskLowRiskWrite Risk = "low_risk_write"
)

var (
	ErrInvalidDefinition = errors.New("agent tool: invalid definition")
	ErrInvalidInput      = errors.New("agent tool: invalid input")
	ErrUnknownTool       = errors.New("agent tool: unknown tool")
	ErrExecutionRejected = errors.New("agent tool: execution rejected")
)

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	ReadOnly    bool           `json:"read_only"`
	Risk        Risk           `json:"risk"`
}

type SourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type CallContext struct {
	Actor      requestcontext.Actor
	ThreadID   string
	RunID      string
	ToolCallID string
	RequestID  string
}

type Invocation struct {
	Name  string
	Input json.RawMessage
}

type InvocationEffect uint8

const (
	InvocationEffectReadOnly InvocationEffect = iota + 1
	InvocationEffectMayWrite
)

// MayWrite reports whether an invocation must consume the write budget.
// Unknown values fail closed and therefore also report a possible write.
func (effect InvocationEffect) MayWrite() bool {
	return effect != InvocationEffectReadOnly
}

// InvocationEffectClassifier lets a conditionally writing tool classify one
// invocation after Registry has normalized its input against the tool schema.
type InvocationEffectClassifier interface {
	ClassifyInvocationEffect(
		input json.RawMessage,
	) (InvocationEffect, error)
}

type Result struct {
	Content    map[string]any      `json:"content"`
	SourceRefs []SourceRef         `json:"source_refs,omitempty"`
	Handoffs   []agenthandoff.Item `json:"handoffs,omitempty"`
}

type Tool interface {
	Definition() Definition
	Execute(ctx context.Context, call CallContext, input json.RawMessage) (Result, error)
}

// ValidateDefinition 检查工具定义是否完整，能否安全暴露给模型。
func ValidateDefinition(definition Definition) error {
	if strings.TrimSpace(definition.Name) == "" ||
		strings.TrimSpace(definition.Description) == "" ||
		definition.InputSchema == nil ||
		!validRisk(definition.Risk) {
		return ErrInvalidDefinition
	}
	if readOnlyRisk(definition.Risk) != definition.ReadOnly {
		return ErrInvalidDefinition
	}
	if err := validateSchemaDefinition(definition.InputSchema); err != nil {
		return ErrInvalidDefinition
	}
	return nil
}

// ValidateJSONObject 检查工具入参是否为非空 JSON 对象。
func ValidateJSONObject(input json.RawMessage) error {
	value, err := decodeJSONValue(input)
	if err != nil {
		return ErrInvalidInput
	}
	if object, ok := value.(map[string]any); !ok || object == nil {
		return ErrInvalidInput
	}
	return nil
}

// ValidateInput 按工具声明的 InputSchema 校验一次工具调用入参。
func ValidateInput(schema map[string]any, input json.RawMessage) error {
	_, err := NormalizeInput(schema, input)
	return err
}

// NormalizeInput 按 Schema 校验并重建工具参数。
// 模型附加的未知字段会在这里被移除，工具实现永远只收到声明过的字段。
func NormalizeInput(
	schema map[string]any,
	input json.RawMessage,
) (json.RawMessage, error) {
	if err := validateSchemaDefinition(schema); err != nil {
		return nil, ErrInvalidDefinition
	}
	value, err := decodeJSONValue(input)
	if err != nil {
		return nil, ErrInvalidInput
	}
	normalized, err := normalizeSchemaValue(schema, value)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return raw, nil
}

// readOnlyRisk 判断风险等级是否代表只读工具。
func readOnlyRisk(risk Risk) bool {
	return risk == RiskReadOnly
}

// validRisk 判断风险等级是否为当前支持的取值。
func validRisk(risk Risk) bool {
	return risk == RiskReadOnly ||
		risk == RiskLowRiskWrite
}

// schemaType 读取 JSON Schema 的 type 字段。
func schemaType(schema map[string]any) string {
	value, _ := schema["type"].(string)
	return value
}

// requiredFields 读取 JSON Schema 的 required 字段。
func requiredFields(schema map[string]any) []string {
	switch raw := schema["required"].(type) {
	case []string:
		return append([]string{}, raw...)
	case []any:
		fields := make([]string, 0, len(raw))
		for _, value := range raw {
			field, ok := value.(string)
			if ok {
				fields = append(fields, field)
			}
		}
		return fields
	default:
		return nil
	}
}

// additionalPropertiesDisabled 判断 Schema 是否禁止额外字段。
func additionalPropertiesDisabled(schema map[string]any) bool {
	value, ok := schema["additionalProperties"].(bool)
	return ok && !value
}

func decodeJSONValue(input json.RawMessage) (any, error) {
	if len(input) == 0 {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidInput
	}
	return value, nil
}

func normalizeSchemaValue(schema map[string]any, value any) (any, error) {
	if value == nil || !matchesJSONType(value, schemaType(schema)) {
		return nil, ErrInvalidInput
	}
	if !matchesEnum(value, schema["enum"]) {
		return nil, ErrInvalidInput
	}
	switch schemaType(schema) {
	case "object":
		return normalizeObject(schema, value.(map[string]any))
	case "array":
		return normalizeArray(schema, value.([]any))
	case "string":
		text := value.(string)
		if !matchesStringConstraints(schema, text) {
			return nil, ErrInvalidInput
		}
		return text, nil
	case "integer", "number":
		if !matchesNumberConstraints(schema, value.(json.Number)) {
			return nil, ErrInvalidInput
		}
		return value, nil
	default:
		return value, nil
	}
}

func normalizeObject(
	schema map[string]any,
	value map[string]any,
) (map[string]any, error) {
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
	}
	for _, field := range requiredFields(schema) {
		raw, exists := value[field]
		if !exists || raw == nil {
			return nil, ErrInvalidInput
		}
	}
	normalized := make(map[string]any, len(value))
	for field, raw := range value {
		property, declared := properties[field].(map[string]any)
		if !declared {
			if additionalPropertiesDisabled(schema) {
				continue
			}
			normalized[field] = raw
			continue
		}
		clean, err := normalizeSchemaValue(property, raw)
		if err != nil {
			return nil, err
		}
		normalized[field] = clean
	}
	return normalized, nil
}

func normalizeArray(schema map[string]any, value []any) ([]any, error) {
	if !matchesLength(len(value), schema["minItems"], schema["maxItems"]) {
		return nil, ErrInvalidInput
	}
	itemSchema, ok := schema["items"].(map[string]any)
	if !ok {
		return value, nil
	}
	normalized := make([]any, 0, len(value))
	for _, item := range value {
		clean, err := normalizeSchemaValue(itemSchema, item)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}

func matchesStringConstraints(schema map[string]any, value string) bool {
	if !matchesLength(
		utf8.RuneCountInString(value),
		schema["minLength"],
		schema["maxLength"],
	) {
		return false
	}
	switch format, _ := schema["format"].(string); format {
	case "":
		return true
	case "non-empty-text":
		return strings.TrimSpace(value) != ""
	case "agent-id":
		return agentIDPattern.MatchString(value)
	default:
		return false
	}
}

func matchesNumberConstraints(schema map[string]any, value json.Number) bool {
	number, err := value.Float64()
	if err != nil {
		return false
	}
	if minimum, ok := schemaNumber(schema["minimum"]); ok && number < minimum {
		return false
	}
	if maximum, ok := schemaNumber(schema["maximum"]); ok && number > maximum {
		return false
	}
	return true
}

func matchesLength(length int, minimum any, maximum any) bool {
	if value, ok := schemaInteger(minimum); ok && length < value {
		return false
	}
	if value, ok := schemaInteger(maximum); ok && length > value {
		return false
	}
	return true
}

func matchesEnum(value any, raw any) bool {
	if raw == nil {
		return true
	}
	values, ok := raw.([]string)
	if !ok {
		generic, genericOK := raw.([]any)
		if !genericOK {
			return false
		}
		for _, candidate := range generic {
			if scalarEqual(value, candidate) {
				return true
			}
		}
		return false
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range values {
		if text == candidate {
			return true
		}
	}
	return false
}

func scalarEqual(left any, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil &&
		bytes.Equal(leftJSON, rightJSON)
}

// matchesJSONType 判断反序列化后的 JSON 值是否符合声明类型。
func matchesJSONType(value any, declared string) bool {
	switch declared {
	case "":
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		parsed, err := number.Float64()
		return err == nil && math.Trunc(parsed) == parsed
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}

func schemaInteger(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if math.Trunc(typed) == typed {
			return int(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	}
	return 0, false
}

func schemaNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	}
	return 0, false
}

func validateSchemaDefinition(schema map[string]any) error {
	if schema == nil || !supportedSchemaType(schemaType(schema)) {
		return ErrInvalidDefinition
	}
	if !validFormatDefinition(schema) ||
		!validEnumDefinition(schemaType(schema), schema["enum"]) {
		return ErrInvalidDefinition
	}
	switch schemaType(schema) {
	case "object":
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return ErrInvalidDefinition
		}
		if raw, exists := schema["additionalProperties"]; exists {
			if _, ok := raw.(bool); !ok {
				return ErrInvalidDefinition
			}
		}
		required, ok := validatedRequiredFields(schema)
		if !ok {
			return ErrInvalidDefinition
		}
		for _, field := range required {
			if _, exists := properties[field]; !exists {
				return ErrInvalidDefinition
			}
		}
		for _, raw := range properties {
			property, ok := raw.(map[string]any)
			if !ok || validateSchemaDefinition(property) != nil {
				return ErrInvalidDefinition
			}
		}
	case "array":
		if raw, exists := schema["items"]; exists {
			items, ok := raw.(map[string]any)
			if !ok || validateSchemaDefinition(items) != nil {
				return ErrInvalidDefinition
			}
		}
	}
	if !validIntegerConstraint(schema["minLength"]) ||
		!validIntegerConstraint(schema["maxLength"]) ||
		!validIntegerConstraint(schema["minItems"]) ||
		!validIntegerConstraint(schema["maxItems"]) ||
		!validNumberConstraint(schema["minimum"]) ||
		!validNumberConstraint(schema["maximum"]) ||
		!validIntegerRange(schema["minLength"], schema["maxLength"]) ||
		!validIntegerRange(schema["minItems"], schema["maxItems"]) ||
		!validNumberRange(schema["minimum"], schema["maximum"]) {
		return ErrInvalidDefinition
	}
	return nil
}

func validFormatDefinition(schema map[string]any) bool {
	raw, exists := schema["format"]
	if !exists {
		return true
	}
	format, ok := raw.(string)
	if !ok || schemaType(schema) != "string" {
		return false
	}
	return format == "non-empty-text" || format == "agent-id"
}

func validEnumDefinition(declared string, raw any) bool {
	if raw == nil {
		return true
	}
	switch values := raw.(type) {
	case []string:
		if declared != "string" || len(values) == 0 {
			return false
		}
		for _, value := range values {
			if value == "" {
				return false
			}
		}
		return true
	case []any:
		if len(values) == 0 {
			return false
		}
		for _, value := range values {
			if value == nil {
				return false
			}
			if declared == "string" {
				if _, ok := value.(string); !ok {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func validatedRequiredFields(schema map[string]any) ([]string, bool) {
	raw, exists := schema["required"]
	if !exists {
		return nil, true
	}
	var fields []string
	switch values := raw.(type) {
	case []string:
		fields = append([]string(nil), values...)
	case []any:
		fields = make([]string, 0, len(values))
		for _, value := range values {
			field, ok := value.(string)
			if !ok {
				return nil, false
			}
			fields = append(fields, field)
		}
	default:
		return nil, false
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field == "" {
			return nil, false
		}
		if _, exists := seen[field]; exists {
			return nil, false
		}
		seen[field] = struct{}{}
	}
	return fields, true
}

func supportedSchemaType(value string) bool {
	return value == "object" ||
		value == "array" ||
		value == "string" ||
		value == "integer" ||
		value == "number" ||
		value == "boolean"
}

func validIntegerConstraint(value any) bool {
	if value == nil {
		return true
	}
	parsed, ok := schemaInteger(value)
	return ok && parsed >= 0
}

func validNumberConstraint(value any) bool {
	if value == nil {
		return true
	}
	_, ok := schemaNumber(value)
	return ok
}

func validIntegerRange(minimum any, maximum any) bool {
	minimumValue, hasMinimum := schemaInteger(minimum)
	maximumValue, hasMaximum := schemaInteger(maximum)
	return !hasMinimum || !hasMaximum || minimumValue <= maximumValue
}

func validNumberRange(minimum any, maximum any) bool {
	minimumValue, hasMinimum := schemaNumber(minimum)
	maximumValue, hasMaximum := schemaNumber(maximum)
	return !hasMinimum || !hasMaximum || minimumValue <= maximumValue
}
