package tool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

type Executor struct {
	registry    *Registry
	logger      *slog.Logger
	logPayloads bool
}

// NewExecutor 基于工具注册表创建统一工具执行器。
func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

func NewExecutorWithLogger(
	registry *Registry,
	logger *slog.Logger,
	logPayloads ...bool,
) *Executor {
	enabled := true
	if len(logPayloads) > 0 {
		enabled = logPayloads[0]
	}
	return &Executor{
		registry:    registry,
		logger:      logger,
		logPayloads: enabled,
	}
}

// Execute 校验工具调用，查找对应工具，并带着可信上下文执行它。
func (executor *Executor) Execute(
	ctx context.Context,
	call CallContext,
	invocation Invocation,
	policy Policy,
) (Result, error) {
	if executor == nil || executor.registry == nil {
		return Result{}, ErrUnknownTool
	}
	tool, ok := executor.registry.Get(invocation.Name)
	if !ok {
		return Result{}, ErrUnknownTool
	}
	definition := tool.Definition()
	if !policy.Allows(definition) {
		return Result{}, ErrToolRejected
	}
	if !call.Actor.Valid() || call.ThreadID == "" ||
		call.RunID == "" || call.ToolCallID == "" ||
		call.RequestID == "" {
		return Result{}, ErrToolRejected
	}
	if err := ValidateInput(definition.InputSchema, invocation.Input); err != nil {
		executor.logFailure(call, definition, 0, err)
		return Result{}, err
	}
	startedAt := time.Now()
	executor.logStarted(call, definition, invocation.Input)
	result, err := tool.Execute(ctx, call, invocation.Input)
	if err != nil {
		executor.logFailure(call, definition, time.Since(startedAt), err)
		return Result{}, err
	}
	if result.Content == nil {
		result.Content = map[string]any{}
	}
	executor.logSucceeded(call, definition, result, time.Since(startedAt))
	return result, nil
}

func (executor *Executor) logStarted(
	call CallContext,
	definition Definition,
	input json.RawMessage,
) {
	if executor == nil || executor.logger == nil {
		return
	}
	attrs := []any{
		"run_id", call.RunID,
		"thread_id", call.ThreadID,
		"tool_call_id", call.ToolCallID,
		"tool_name", definition.Name,
		"risk", string(definition.Risk),
	}
	if executor.logPayloads {
		attrs = append(attrs, "input_summary", SummarizeJSON(input))
	}
	executor.logger.Info("agent.tool.call.started", attrs...)
}

func (executor *Executor) logSucceeded(
	call CallContext,
	definition Definition,
	result Result,
	duration time.Duration,
) {
	if executor == nil || executor.logger == nil {
		return
	}
	attrs := []any{
		"run_id", call.RunID,
		"thread_id", call.ThreadID,
		"tool_call_id", call.ToolCallID,
		"tool_name", definition.Name,
		"duration_ms", duration.Milliseconds(),
		"source_ref_count", len(result.SourceRefs),
	}
	if executor.logPayloads {
		attrs = append(attrs, "result_summary", SummarizeResult(result))
	}
	executor.logger.Info("agent.tool.call.succeeded", attrs...)
}

func (executor *Executor) logFailure(
	call CallContext,
	definition Definition,
	duration time.Duration,
	err error,
) {
	if executor == nil || executor.logger == nil {
		return
	}
	executor.logger.Warn(
		"agent.tool.call.failed",
		"run_id", call.RunID,
		"thread_id", call.ThreadID,
		"tool_call_id", call.ToolCallID,
		"tool_name", definition.Name,
		"duration_ms", duration.Milliseconds(),
		"error_category", ErrorCategory(err),
		"retryable", RetryableError(err),
	)
}

func ErrorCategory(err error) string {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, ErrUnknownTool):
		return "unknown_tool"
	case errors.Is(err, ErrToolRejected):
		return "permission_denied"
	default:
		return "internal"
	}
}

func RetryableError(err error) bool {
	return !errors.Is(err, ErrInvalidInput) &&
		!errors.Is(err, ErrToolRejected) &&
		!errors.Is(err, ErrUnknownTool)
}

// MarshalInput 把结构化值编码成工具调用使用的原始 JSON 入参。
func MarshalInput(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
