package agenttool

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	ScenarioCreateToolName = "scenario.create.v1"
	ScenarioSearchToolName = "scenario.search.v1"
)

type ScenarioCreateInput struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	Goal  string `json:"goal,omitempty"`
}

type ScenarioSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type ScenarioResult struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Type      string      `json:"type"`
	Status    string      `json:"status"`
	Summary   string      `json:"summary,omitempty"`
	SourceRef []SourceRef `json:"source_refs,omitempty"`
}

type ScenarioPort interface {
	CreateScenario(ctx context.Context, call CallContext, input ScenarioCreateInput) (ScenarioResult, error)
	SearchScenarios(ctx context.Context, call CallContext, input ScenarioSearchInput) ([]ScenarioResult, error)
}

type ScenarioCreateTool struct {
	port ScenarioPort
}

// NewScenarioCreateTool 创建场景创建工具的适配器。
func NewScenarioCreateTool(port ScenarioPort) ScenarioCreateTool {
	return ScenarioCreateTool{port: port}
}

// Definition 描述 scenario.create.v1，供模型和命令入口识别。
func (tool ScenarioCreateTool) Definition() Definition {
	return Definition{
		Name:        ScenarioCreateToolName,
		Description: "Create a user's real-world interview, meeting, client, or speaking scenario.",
		InputSchema: ObjectSchema(map[string]any{
			"type":  StringSchema("Scenario type such as interview, meeting, client, presentation, or speaking."),
			"title": StringSchema("Short user-facing scenario title."),
			"goal":  StringSchema("What the user wants to prepare or improve."),
		}, []string{"type"}),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

// Execute 校验创建场景入参，并委托 ScenarioPort 创建场景。
func (tool ScenarioCreateTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrToolRejected
	}
	var parsed ScenarioCreateInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Type == "" {
		return Result{}, ErrInvalidInput
	}
	result, err := tool.port.CreateScenario(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	return scenarioToolResult(result), nil
}

type ScenarioSearchTool struct {
	port ScenarioPort
}

// NewScenarioSearchTool 创建场景搜索工具的适配器。
func NewScenarioSearchTool(port ScenarioPort) ScenarioSearchTool {
	return ScenarioSearchTool{port: port}
}

// Definition 描述 scenario.search.v1，供模型和命令入口识别。
func (tool ScenarioSearchTool) Definition() Definition {
	return Definition{
		Name:        ScenarioSearchToolName,
		Description: "Search the user's existing scenarios when the current scenario is ambiguous.",
		InputSchema: ObjectSchema(map[string]any{
			"query": StringSchema("User phrase describing the scenario to find."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of scenarios to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute 校验搜索场景入参，并委托 ScenarioPort 查询场景。
func (tool ScenarioSearchTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrToolRejected
	}
	var parsed ScenarioSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Query == "" {
		return Result{}, ErrInvalidInput
	}
	results, err := tool.port.SearchScenarios(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	items := make([]map[string]any, 0, len(results))
	sourceRefs := make([]SourceRef, 0)
	for _, result := range results {
		items = append(items, scenarioMap(result))
		sourceRefs = append(sourceRefs, result.SourceRef...)
	}
	return Result{
		Content:    map[string]any{"scenarios": items},
		SourceRefs: sourceRefs,
	}, nil
}

// scenarioToolResult 把单个场景领域结果转换成工具结果。
func scenarioToolResult(result ScenarioResult) Result {
	return Result{
		Content:    map[string]any{"scenario": scenarioMap(result)},
		SourceRefs: result.SourceRef,
	}
}

// scenarioMap 返回暴露给模型的精简场景 JSON 对象。
func scenarioMap(result ScenarioResult) map[string]any {
	return map[string]any{
		"id":      result.ID,
		"title":   result.Title,
		"type":    result.Type,
		"status":  result.Status,
		"summary": result.Summary,
	}
}
