package agenttool

import (
	"context"
	"encoding/json"
	"strings"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	ReviewSearchToolName = "review.search.v1"
	ReviewGetToolName    = "review.get.v1"
)

type ReviewSearchInput struct {
	Query             string `json:"query,omitempty"`
	PracticeSessionID string `json:"practice_session_id,omitempty"`
	Limit             int    `json:"limit,omitempty"`
}

type ReviewGetInput struct {
	ReviewID string `json:"review_id"`
}

type ReviewSummary struct {
	ID                   string      `json:"review_id"`
	PracticeSessionID    string      `json:"practice_session_id"`
	ScenarioDefinitionID string      `json:"scenario_definition_id,omitempty"`
	Summary              string      `json:"summary"`
	CompletedAt          string      `json:"completed_at,omitempty"`
	SourceRefs           []SourceRef `json:"-"`
}

type ReviewConclusion struct {
	Key        string `json:"key"`
	Category   string `json:"category"`
	Score      int    `json:"score,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type ReviewFeedbackItem struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type ReviewDetail struct {
	ID                          string               `json:"review_id"`
	PracticeSessionID           string               `json:"practice_session_id"`
	ScenarioDefinitionID        string               `json:"scenario_definition_id,omitempty"`
	Status                      string               `json:"status"`
	SummaryEligibility          string               `json:"summary_eligibility"`
	OverallScore                *int                 `json:"overall_score,omitempty"`
	Summary                     string               `json:"summary"`
	Conclusions                 []ReviewConclusion   `json:"conclusions"`
	FeedbackItems               []ReviewFeedbackItem `json:"feedback_items,omitempty"`
	RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs,omitempty"`
	InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons,omitempty"`
	CompletedAt                 string               `json:"completed_at,omitempty"`
	SourceRefs                  []SourceRef          `json:"-"`
}

type ReviewPort interface {
	SearchReviews(ctx context.Context, call CallContext, input ReviewSearchInput) ([]ReviewSummary, error)
	GetReview(ctx context.Context, call CallContext, input ReviewGetInput) (ReviewDetail, error)
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
		Description: "Search the current user's completed practice reviews and interview evaluations. When the user refers to the practice they just completed, omit query to retrieve the latest completed review. Use a natural-language query only for older or specific practice history. Review identifiers are internal: use returned identifiers only for review.get.v1 and never ask the user to provide or repeat them. Do not use for correcting only the current sentence or searching scenarios.",
		InputSchema: ObjectSchema(map[string]any{
			"query": TextSchema(
				"Optional words describing an older or specific review. Omit for the latest completed practice.",
				500,
			),
			"limit": IntegerRangeSchema(
				"Maximum number of review summaries to return.",
				1,
				20,
			),
		}, nil),
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
		return Result{}, ErrExecutionRejected
	}
	var parsed ReviewSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{}, ErrInvalidInput
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
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
		Description: "Use this tool to read the full structured details of exactly one review by the internal review_id returned from review.search.v1. Never ask the user to provide, repeat, or understand this identifier, and never expose it in the reply. Do not use for broad historical review searches or guess an id from natural language.",
		InputSchema: ObjectSchema(map[string]any{
			"review_id": IdentifierSchema(
				"Exact review id returned by a previous review search.",
			),
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
		return Result{}, ErrExecutionRejected
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
		Content:    map[string]any{"review": reviewDetailMap(review)},
		SourceRefs: review.SourceRefs,
	}, nil
}

// reviewMap 返回暴露给模型的 Review 搜索摘要。
func reviewMap(review ReviewSummary) map[string]any {
	return map[string]any{
		"review_id":              review.ID,
		"practice_session_id":    review.PracticeSessionID,
		"scenario_definition_id": review.ScenarioDefinitionID,
		"summary":                review.Summary,
		"completed_at":           review.CompletedAt,
	}
}

// reviewDetailMap 返回正式 FormalReview 的结构化结果，不暴露所有者和生成租约。
func reviewDetailMap(review ReviewDetail) map[string]any {
	return map[string]any{
		"review_id":                     review.ID,
		"practice_session_id":           review.PracticeSessionID,
		"scenario_definition_id":        review.ScenarioDefinitionID,
		"status":                        review.Status,
		"summary_eligibility":           review.SummaryEligibility,
		"overall_score":                 review.OverallScore,
		"summary":                       review.Summary,
		"conclusions":                   review.Conclusions,
		"feedback_items":                review.FeedbackItems,
		"repractice_suggestion_refs":    review.RepracticeSuggestionRefs,
		"insufficient_evidence_reasons": review.InsufficientEvidenceReasons,
		"completed_at":                  review.CompletedAt,
	}
}
