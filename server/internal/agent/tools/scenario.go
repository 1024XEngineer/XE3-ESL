package tools

import (
	"context"
	"encoding/json"
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

// NewScenarioCreateTool creates the adapter for the scenario creation tool.
func NewScenarioCreateTool(port ScenarioPort) ScenarioCreateTool {
	return ScenarioCreateTool{port: port}
}

// Definition describes scenario.create.v1 for model and command exposure.
func (tool ScenarioCreateTool) Definition() Definition {
	return Definition{
		Name:        ScenarioCreateToolName,
		Description: "Create a user's real-world interview, meeting, client, or speaking scenario.",
		InputSchema: objectSchema(map[string]any{
			"type":  stringSchema("Scenario type such as interview, meeting, client, presentation, or speaking."),
			"title": stringSchema("Short user-facing scenario title."),
			"goal":  stringSchema("What the user wants to prepare or improve."),
		}, []string{"type"}),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

// Execute validates create input and delegates scenario creation to the ScenarioPort.
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

// NewScenarioSearchTool creates the adapter for the scenario search tool.
func NewScenarioSearchTool(port ScenarioPort) ScenarioSearchTool {
	return ScenarioSearchTool{port: port}
}

// Definition describes scenario.search.v1 for model and command exposure.
func (tool ScenarioSearchTool) Definition() Definition {
	return Definition{
		Name:        ScenarioSearchToolName,
		Description: "Search the user's existing scenarios when the current scenario is ambiguous.",
		InputSchema: objectSchema(map[string]any{
			"query": stringSchema("User phrase describing the scenario to find."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of scenarios to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute validates search input and delegates scenario lookup to the ScenarioPort.
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

// scenarioToolResult converts a single scenario domain result into a tool result.
func scenarioToolResult(result ScenarioResult) Result {
	return Result{
		Content:    map[string]any{"scenario": scenarioMap(result)},
		SourceRefs: result.SourceRef,
	}
}

// scenarioMap returns the compact JSON object exposed back to the model for a scenario.
func scenarioMap(result ScenarioResult) map[string]any {
	return map[string]any{
		"id":      result.ID,
		"title":   result.Title,
		"type":    result.Type,
		"status":  result.Status,
		"summary": result.Summary,
	}
}
