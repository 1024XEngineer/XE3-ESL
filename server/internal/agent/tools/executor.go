package tools

import (
	"context"
	"encoding/json"
)

type Executor struct {
	registry *Registry
}

func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

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

func MarshalInput(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
