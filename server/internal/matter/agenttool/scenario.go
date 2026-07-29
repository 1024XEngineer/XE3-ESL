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
	Title string `json:"title"`
}

type ScenarioSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type MatterResult struct {
	MatterID   string      `json:"matter_id"`
	Title      string      `json:"title"`
	Status     string      `json:"status"`
	Version    int64       `json:"version"`
	UpdatedAt  string      `json:"updated_at"`
	SourceRefs []SourceRef `json:"-"`
}

type ScenarioPort interface {
	CreateScenario(ctx context.Context, call CallContext, input ScenarioCreateInput) (MatterResult, error)
	SearchScenarios(ctx context.Context, call CallContext, input ScenarioSearchInput) ([]MatterResult, error)
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
		Description: "Create and persist one long-lived RealityMatter for the current user. Use when the user wants to establish a new real-world context such as a specific interview, meeting, client relationship, or presentation project. This is a write operation and does not create or start a practice session. Do not use to find an existing Matter, start practice, inspect reviews, search user materials, or answer standalone translation and wording questions.",
		InputSchema: ObjectSchema(map[string]any{
			"title": TextSchema(
				"Required short user-facing title for the new RealityMatter.",
				200,
			),
		}, []string{"title"}),
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
		return Result{}, ErrExecutionRejected
	}
	var parsed ScenarioCreateInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Title == "" {
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
		Description: "Search the current user's existing RealityMatters and return matching Matter records. Use when the user refers to a previous real-world interview, meeting, client relationship, or presentation project and the existing Matter must be identified first. Do not use to create a new Matter, start practice, read a review, or search resume and job-description materials.",
		InputSchema: ObjectSchema(map[string]any{
			"query": TextSchema(
				"Words from the title of the existing RealityMatter to find.",
				500,
			),
			"limit": IntegerRangeSchema(
				"Maximum number of Matter records to return.",
				1,
				20,
			),
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
		return Result{}, ErrExecutionRejected
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
		items = append(items, matterMap(result))
		sourceRefs = append(sourceRefs, result.SourceRefs...)
	}
	return Result{
		Content:    map[string]any{"matters": items},
		SourceRefs: sourceRefs,
	}, nil
}

// scenarioToolResult 把单个 Matter 领域结果转换成工具结果。
func scenarioToolResult(result MatterResult) Result {
	return Result{
		Content:    map[string]any{"matter": matterMap(result)},
		SourceRefs: result.SourceRefs,
	}
}

// matterMap 返回暴露给模型的精简 Matter JSON 对象。
func matterMap(result MatterResult) map[string]any {
	return map[string]any{
		"matter_id":  result.MatterID,
		"title":      result.Title,
		"status":     result.Status,
		"version":    result.Version,
		"updated_at": result.UpdatedAt,
	}
}
