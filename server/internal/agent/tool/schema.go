// Package tool defines the Agent tool contract and adapts domain services into
// tools the Agent runtime can call.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Risk string

const (
	RiskReadOnly        Risk = "read_only"
	RiskLowRiskWrite    Risk = "low_risk_write"
	RiskRequiresConfirm Risk = "requires_confirm"
)

var (
	ErrInvalidDefinition = errors.New("agent tool: invalid definition")
	ErrInvalidInput      = errors.New("agent tool: invalid input")
	ErrUnknownTool       = errors.New("agent tool: unknown tool")
	ErrToolRejected      = errors.New("agent tool: rejected")
)

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

type Result struct {
	Content    map[string]any `json:"content"`
	SourceRefs []SourceRef    `json:"source_refs,omitempty"`
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
	return nil
}

// ValidateJSONObject 检查工具入参是否为非空 JSON 对象。
func ValidateJSONObject(input json.RawMessage) error {
	var value map[string]any
	if len(input) == 0 || json.Unmarshal(input, &value) != nil || value == nil {
		return ErrInvalidInput
	}
	return nil
}

// ValidateInput 按工具声明的 InputSchema 校验一次工具调用入参。
func ValidateInput(schema map[string]any, input json.RawMessage) error {
	if err := ValidateJSONObject(input); err != nil {
		return err
	}
	var value map[string]any
	if err := json.Unmarshal(input, &value); err != nil {
		return ErrInvalidInput
	}
	if schemaType(schema) != "object" {
		return ErrInvalidDefinition
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		properties = map[string]any{}
	}
	for _, field := range requiredFields(schema) {
		raw, exists := value[field]
		if !exists || raw == nil {
			return ErrInvalidInput
		}
	}
	if additionalPropertiesDisabled(schema) {
		for field := range value {
			if _, exists := properties[field]; !exists {
				return ErrInvalidInput
			}
		}
	}
	for field, raw := range value {
		if raw == nil {
			return ErrInvalidInput
		}
		property, ok := properties[field].(map[string]any)
		if !ok {
			continue
		}
		if !matchesJSONType(raw, schemaType(property)) {
			return ErrInvalidInput
		}
	}
	return nil
}

// readOnlyRisk 判断风险等级是否代表只读工具。
func readOnlyRisk(risk Risk) bool {
	return risk == RiskReadOnly
}

// validRisk 判断风险等级是否为当前支持的取值。
func validRisk(risk Risk) bool {
	return risk == RiskReadOnly ||
		risk == RiskLowRiskWrite ||
		risk == RiskRequiresConfirm
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

// matchesJSONType 判断反序列化后的 JSON 值是否符合声明类型。
func matchesJSONType(value any, declared string) bool {
	switch declared {
	case "":
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	case "number":
		_, ok := value.(float64)
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
