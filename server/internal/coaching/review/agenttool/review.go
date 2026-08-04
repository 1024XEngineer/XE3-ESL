package agenttool

import (
	"context"
	"encoding/json"
	"strings"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

const (
	ReviewSearchToolName = "review.search.v2"
	ReviewGetToolName    = "review.get.v2"
)

type ReviewSearchInput struct {
	Query             string `json:"query,omitempty"`
	PracticeSessionID string `json:"practice_session_id,omitempty"`
	Limit             int    `json:"limit,omitempty"`
}

type ReviewGetInput struct {
	ReportID string `json:"report_id"`
}

type ReviewSummary struct {
	ID                string      `json:"report_id"`
	PracticeSessionID string      `json:"practice_session_id"`
	SceneType         string      `json:"scene_type"`
	SceneModel        string      `json:"scene_model"`
	Scoreability      string      `json:"scoreability_status"`
	Summary           string      `json:"summary"`
	CompletedAt       string      `json:"completed_at"`
	SourceRefs        []SourceRef `json:"-"`
}

type ReviewFinding struct {
	ID               string   `json:"finding_id"`
	Message          string   `json:"message"`
	Suggestion       string   `json:"suggestion,omitempty"`
	OriginalExcerpts []string `json:"original_excerpts,omitempty"`
}

type ReviewDimension struct {
	Key          string          `json:"key"`
	Score        *float64        `json:"score,omitempty"`
	Scale        string          `json:"scale"`
	Coverage     float64         `json:"coverage"`
	Confidence   float64         `json:"confidence"`
	Strengths    []ReviewFinding `json:"strengths"`
	Improvements []ReviewFinding `json:"improvements"`
	Examples     []ReviewFinding `json:"recommended_examples"`
}

type ReviewPriorityAction struct {
	DimensionKey string `json:"dimension_key"`
	FindingID    string `json:"finding_id"`
}

type ReviewDetail struct {
	ID                string                 `json:"report_id"`
	PracticeSessionID string                 `json:"practice_session_id"`
	SceneType         string                 `json:"scene_type"`
	SceneModel        string                 `json:"scene_model"`
	Scoreability      string                 `json:"scoreability_status"`
	Summary           string                 `json:"summary"`
	Dimensions        []ReviewDimension      `json:"dimensions"`
	PriorityActions   []ReviewPriorityAction `json:"priority_actions"`
	CompletedAt       string                 `json:"completed_at"`
	SourceRefs        []SourceRef            `json:"-"`
}

type ReviewPort interface {
	SearchReviews(
		context.Context,
		CallContext,
		ReviewSearchInput,
	) ([]ReviewSummary, error)
	GetReview(
		context.Context,
		CallContext,
		ReviewGetInput,
	) (ReviewDetail, error)
}

type ReviewSearchTool struct{ port ReviewPort }

func NewReviewSearchTool(port ReviewPort) ReviewSearchTool {
	return ReviewSearchTool{port: port}
}

func (tool ReviewSearchTool) Definition() Definition {
	return Definition{
		Name: ReviewSearchToolName,
		Description: "Use this tool to search the current user's completed " +
			"Evaluation reports. " +
			"Omit query for the newest reports; use natural-language query for " +
			"older practice history. Internal report identifiers must never be " +
			"asked from or exposed to the user. Do not use it for current-turn " +
			"speech feedback or scenario discovery.",
		InputSchema: ObjectSchema(map[string]any{
			"query": TextSchema(
				"Optional words describing an older practice report.",
				500,
			),
			"limit": IntegerRangeSchema(
				"Maximum number of report summaries to return.",
				1,
				20,
			),
		}, nil),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

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
	reports, err := tool.port.SearchReviews(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	items := make([]ReviewSummary, len(reports))
	sourceRefs := make([]SourceRef, 0, len(reports))
	for index, report := range reports {
		items[index] = report
		sourceRefs = append(sourceRefs, report.SourceRefs...)
	}
	return Result{
		Content:    map[string]any{"reports": items},
		SourceRefs: sourceRefs,
	}, nil
}

type ReviewGetTool struct{ port ReviewPort }

func NewReviewGetTool(port ReviewPort) ReviewGetTool {
	return ReviewGetTool{port: port}
}

func (tool ReviewGetTool) Definition() Definition {
	return Definition{
		Name: ReviewGetToolName,
		Description: "Use this tool to read one completed Evaluation report " +
			"selected by " +
			"review.search.v2. Never ask the user to provide or repeat its " +
			"internal report identifier. Do not use it without an identifier " +
			"returned by review.search.v2.",
		InputSchema: ObjectSchema(map[string]any{
			"report_id": IdentifierSchema(
				"Exact report id returned by review.search.v2.",
			),
		}, []string{"report_id"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

func (tool ReviewGetTool) Execute(
	ctx context.Context,
	call CallContext,
	input json.RawMessage,
) (Result, error) {
	if tool.port == nil {
		return Result{}, ErrExecutionRejected
	}
	var parsed ReviewGetInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.ReportID == "" {
		return Result{}, ErrInvalidInput
	}
	report, err := tool.port.GetReview(ctx, call, parsed)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content:    map[string]any{"report": report},
		SourceRefs: report.SourceRefs,
	}, nil
}
