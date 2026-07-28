// Package tools 定义 Agent 工具契约，并把领域服务适配成 Agent 可调用工具。
package tools

import (
	"context"
	"encoding/json"
	"errors"
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
	if len(input) == 0 || json.Unmarshal(input, &value) != nil {
		return ErrInvalidInput
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
