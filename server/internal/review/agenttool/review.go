package agenttool

import (
	"context"
	"encoding/json"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	ReviewSearchToolName = "review.search.v1"
	ReviewGetToolName    = "review.get.v1"
)

type ReviewSearchInput struct {
	Query      string `json:"query"`
	ScenarioID string `json:"scenario_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type ReviewGetInput struct {
	ReviewID string `json:"review_id"`
}

type ReviewSummary struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Summary    string      `json:"summary"`
	ScenarioID string      `json:"scenario_id,omitempty"`
	SourceRefs []SourceRef `json:"source_refs,omitempty"`
}

type ReviewPort interface {
	SearchReviews(ctx context.Context, call CallContext, input ReviewSearchInput) ([]ReviewSummary, error)
	GetReview(ctx context.Context, call CallContext, input ReviewGetInput) (ReviewSummary, error)
}

type ReviewSearchTool struct {
	port ReviewPort
}

// NewReviewSearchTool 创建 Review 搜索工具的适配器。
func NewReviewSearchTool(port ReviewPort) ReviewSearchTool {
	return ReviewSearchTool{port: port}
}

// Definition 描述 review.search.v1，供模型和命令入口识别。
func (tool ReviewSearchTool) Definition() Definition {
	return Definition{
		Name:        ReviewSearchToolName,
		Description: "Search the user's historical practice reviews or interview evaluations. Use when the user asks for 上次评价, 复盘, feedback, review, 面试表现, or previous practice results. Do not use for immediate grammar correction of the current sentence.",
		InputSchema: ObjectSchema(map[string]any{
			"query":       StringSchema("What review or evaluation the user wants to find."),
			"scenario_id": StringSchema("Optional scenario id to restrict the search."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of reviews to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute 校验 Review 搜索入参，并委托 ReviewPort 查询 Review。
func (tool ReviewSearchTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrToolRejected
	}
	var parsed ReviewSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Query == "" {
		return Result{}, ErrInvalidInput
	}
	reviews, err := tool.port.SearchReviews(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	items := make([]map[string]any, 0, len(reviews))
	sourceRefs := make([]SourceRef, 0)
	for _, review := range reviews {
		items = append(items, reviewMap(review))
		sourceRefs = append(sourceRefs, review.SourceRefs...)
	}
	return Result{
		Content:    map[string]any{"reviews": items},
		SourceRefs: sourceRefs,
	}, nil
}

type ReviewGetTool struct {
	port ReviewPort
}

// NewReviewGetTool 创建读取单个 Review 的工具适配器。
func NewReviewGetTool(port ReviewPort) ReviewGetTool {
	return ReviewGetTool{port: port}
}

// Definition 描述 review.get.v1，供模型识别。
func (tool ReviewGetTool) Definition() Definition {
	return Definition{
		Name:        ReviewGetToolName,
		Description: "Read one structured review or interview evaluation by id after a review search or when the user asks to expand a specific review, such as 第一条评价 or details of a previous feedback item.",
		InputSchema: ObjectSchema(map[string]any{
			"review_id": StringSchema("Review id to read."),
		}, []string{"review_id"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute 校验 Review 读取入参，并委托 ReviewPort 加载详情。
func (tool ReviewGetTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrToolRejected
	}
	var parsed ReviewGetInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.ReviewID == "" {
		return Result{}, ErrInvalidInput
	}
	review, err := tool.port.GetReview(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content:    map[string]any{"review": reviewMap(review)},
		SourceRefs: review.SourceRefs,
	}, nil
}

// reviewMap 返回暴露给模型的精简 Review JSON 对象。
func reviewMap(review ReviewSummary) map[string]any {
	return map[string]any{
		"id":          review.ID,
		"title":       review.Title,
		"summary":     review.Summary,
		"scenario_id": review.ScenarioID,
	}
}
