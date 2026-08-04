package agentcapability

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	GoalCreateCapabilityName = "goal.create.v1"
	GoalSearchCapabilityName = "goal.search.v1"
)

type GoalCreateInput struct {
	Title string `json:"title"`
}

type GoalSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type GoalResult struct {
	GoalID     string      `json:"goal_id"`
	Title      string      `json:"title"`
	Status     string      `json:"status"`
	Version    int64       `json:"version"`
	UpdatedAt  string      `json:"updated_at"`
	SourceRefs []SourceRef `json:"-"`
}

type GoalPort interface {
	CreateGoal(ctx context.Context, call CallContext, input GoalCreateInput) (GoalResult, error)
	SearchGoals(ctx context.Context, call CallContext, input GoalSearchInput) ([]GoalResult, error)
}

type GoalCreateCapability struct {
	port GoalPort
}

// NewGoalCreateCapability creates the Agent adapter for a user-owned Goal.
func NewGoalCreateCapability(port GoalPort) GoalCreateCapability {
	return GoalCreateCapability{port: port}
}

// Definition 描述 goal.create.v1，供模型和命令入口识别。
func (capability GoalCreateCapability) Definition() Definition {
	return Definition{
		Name:        GoalCreateCapabilityName,
		Description: "Create and persist one long-lived Goal for the current user. Use when the user wants to establish a real-world objective such as preparing for a specific interview, meeting, client conversation, or presentation. This is a write operation and does not create or start a practice session. Do not use to find an existing Goal, start practice, inspect reviews, search user materials, or answer standalone translation and wording questions.",
		InputSchema: ObjectSchema(map[string]any{
			"title": TextSchema(
				"Required short user-facing title for the new Goal.",
				200,
			),
		}, []string{"title"}),
		ReadOnly: false,
		Risk:     RiskLowRiskWrite,
	}
}

// Execute validates input and delegates Goal creation to the owning module.
func (capability GoalCreateCapability) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if capability.port == nil {
		return Result{}, ErrExecutionRejected
	}
	var parsed GoalCreateInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Title == "" {
		return Result{}, ErrInvalidInput
	}
	result, err := capability.port.CreateGoal(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	return goalCapabilityResult(result), nil
}

type GoalSearchCapability struct {
	port GoalPort
}

// NewGoalSearchCapability creates the Agent adapter for Goal search.
func NewGoalSearchCapability(port GoalPort) GoalSearchCapability {
	return GoalSearchCapability{port: port}
}

// Definition 描述 goal.search.v1，供模型和命令入口识别。
func (capability GoalSearchCapability) Definition() Definition {
	return Definition{
		Name:        GoalSearchCapabilityName,
		Description: "Search the current user's existing Goals. Use when the user refers to a previous real-world interview, meeting, client conversation, or presentation and the corresponding Goal must be identified first. Do not use to create a new Goal, start practice, read a review, or search resume and job-description materials.",
		InputSchema: ObjectSchema(map[string]any{
			"query": TextSchema(
				"Words from the title of the existing Goal to find.",
				500,
			),
			"limit": IntegerRangeSchema(
				"Maximum number of Goal records to return.",
				1,
				20,
			),
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute validates input and delegates Goal search to the owning module.
func (capability GoalSearchCapability) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if capability.port == nil {
		return Result{}, ErrExecutionRejected
	}
	var parsed GoalSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Query == "" {
		return Result{}, ErrInvalidInput
	}
	results, err := capability.port.SearchGoals(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	items := make([]map[string]any, 0, len(results))
	sourceRefs := make([]SourceRef, 0)
	for _, result := range results {
		items = append(items, goalMap(result))
		sourceRefs = append(sourceRefs, result.SourceRefs...)
	}
	return Result{
		Content:    map[string]any{"goals": items},
		SourceRefs: sourceRefs,
	}, nil
}

// goalCapabilityResult projects one Goal without exposing repository details.
func goalCapabilityResult(result GoalResult) Result {
	return Result{
		Content:    map[string]any{"goal": goalMap(result)},
		SourceRefs: result.SourceRefs,
	}
}

// goalMap returns the minimal Goal projection exposed to the model.
func goalMap(result GoalResult) map[string]any {
	return map[string]any{
		"goal_id":    result.GoalID,
		"title":      result.Title,
		"status":     result.Status,
		"version":    result.Version,
		"updated_at": result.UpdatedAt,
	}
}
