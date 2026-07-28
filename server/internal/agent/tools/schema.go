// Package tools contains the Agent tool contract and small adapters that expose
// domain services to the Agent runtime.
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

func ValidateJSONObject(input json.RawMessage) error {
	var value map[string]any
	if len(input) == 0 || json.Unmarshal(input, &value) != nil {
		return ErrInvalidInput
	}
	return nil
}

func readOnlyRisk(risk Risk) bool {
	return risk == RiskReadOnly
}

func validRisk(risk Risk) bool {
	return risk == RiskReadOnly ||
		risk == RiskLowRiskWrite ||
		risk == RiskRequiresConfirm
}
