package tools

import (
	"context"
	"encoding/json"
)

type Executor struct {
	registry *Registry
}

// NewExecutor 基于工具注册表创建统一工具执行器。
func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

// Execute 校验工具调用，查找对应工具，并带着可信上下文执行它。
func (executor *Executor) Execute(
	ctx context.Context,
	call CallContext,
	invocation Invocation,
) (Result, error) {
	if executor == nil || executor.registry == nil {
		return Result{}, ErrUnknownTool
	}
	tool, ok := executor.registry.Get(invocation.Name)
	if !ok {
		return Result{}, ErrUnknownTool
	}
	if err := ValidateJSONObject(invocation.Input); err != nil {
		return Result{}, err
	}
	if !call.Actor.Valid() || call.ThreadID == "" ||
		call.RunID == "" || call.ToolCallID == "" ||
		call.RequestID == "" {
		return Result{}, ErrToolRejected
	}
	result, err := tool.Execute(ctx, call, invocation.Input)
	if err != nil {
		return Result{}, err
	}
	if result.Content == nil {
		result.Content = map[string]any{}
	}
	return result, nil
}

// MarshalInput 把结构化值编码成工具调用使用的原始 JSON 入参。
func MarshalInput(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
