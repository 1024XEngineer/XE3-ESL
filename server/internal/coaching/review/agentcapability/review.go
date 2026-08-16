package agentcapability

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

const (
	ReviewSearchToolName = "review.search.v2"
	ReviewGetToolName    = "review.get.v2"
)

type ReviewSearchInput struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type ReviewGetInput struct {
	ReportID string `json:"report_id"`
}

type ReviewSummary struct {
	ID                 string                 `json:"report_id"`
	PracticeSessionID  string                 `json:"practice_session_id"`
	SceneType          string                 `json:"scene_type"`
	PracticeExperience string                 `json:"practice_experience"`
	SceneCategory      string                 `json:"scene_category"`
	PracticeMode       string                 `json:"practice_mode"`
	Scoreability       string                 `json:"scoreability_status"`
	Summary            string                 `json:"summary"`
	CompletedAt        string                 `json:"completed_at"`
	SourceRefs         []capability.SourceRef `json:"-"`
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
	ID                 string                 `json:"report_id"`
	PracticeSessionID  string                 `json:"practice_session_id"`
	SceneType          string                 `json:"scene_type"`
	PracticeExperience string                 `json:"practice_experience"`
	SceneCategory      string                 `json:"scene_category"`
	PracticeMode       string                 `json:"practice_mode"`
	Scoreability       string                 `json:"scoreability_status"`
	Summary            string                 `json:"summary"`
	Dimensions         []ReviewDimension      `json:"dimensions"`
	PriorityActions    []ReviewPriorityAction `json:"priority_actions"`
	CompletedAt        string                 `json:"completed_at"`
	SourceRefs         []capability.SourceRef `json:"-"`
}

type ReviewPort interface {
	SearchReviews(
		context.Context,
		capability.CallContext,
		ReviewSearchInput,
	) ([]ReviewSummary, error)
	GetReview(
		context.Context,
		capability.CallContext,
		ReviewGetInput,
	) (ReviewDetail, error)
}

type ReviewSearchTool struct{ port ReviewPort }

func NewReviewSearchTool(port ReviewPort) ReviewSearchTool {
	return ReviewSearchTool{port: port}
}

func (tool ReviewSearchTool) Definition() capability.Definition {
	return capability.Definition{
		Name: ReviewSearchToolName,
		Description: "Use this tool to search the current user's completed " +
			"Evaluation reports. " +
			"Omit query for the newest reports; use natural-language query for " +
			"older practice history. Internal report identifiers must never be " +
			"asked from or exposed to the user. Do not use it for current-turn " +
			"speech feedback or scenario discovery.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"query": capability.TextSchema(
				"Optional words describing an older practice report.",
				500,
			),
			"limit": capability.IntegerRangeSchema(
				"Maximum number of report summaries to return.",
				1,
				20,
			),
		}, nil),
		ReadOnly: true,
		Risk:     capability.RiskReadOnly,
	}
}

func (tool ReviewSearchTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.port == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	var parsed ReviewSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return capability.Result{}, capability.ErrInvalidInput
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
	reports, err := tool.port.SearchReviews(ctx, call, parsed)
	if err != nil {
		return capability.Result{}, err
	}
	items := make([]ReviewSummary, len(reports))
	sourceRefs := make([]capability.SourceRef, 0, len(reports))
	for index, report := range reports {
		items[index] = report
		sourceRefs = append(sourceRefs, report.SourceRefs...)
	}
	return capability.Result{
		Content:    map[string]any{"reports": items},
		SourceRefs: sourceRefs,
	}, nil
}

type ReviewGetTool struct{ port ReviewPort }

func NewReviewGetTool(port ReviewPort) ReviewGetTool {
	return ReviewGetTool{port: port}
}

func (tool ReviewGetTool) Definition() capability.Definition {
	return capability.Definition{
		Name: ReviewGetToolName,
		Description: "Use this tool to read one completed Evaluation report " +
			"selected by " +
			"review.search.v2. Never ask the user to provide or repeat its " +
			"internal report identifier. Do not use it without an identifier " +
			"returned by review.search.v2.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"report_id": capability.IdentifierSchema(
				"Exact report id returned by review.search.v2.",
			),
		}, []string{"report_id"}),
		ReadOnly: true,
		Risk:     capability.RiskReadOnly,
	}
}

func (tool ReviewGetTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.port == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	var parsed ReviewGetInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.ReportID == "" {
		return capability.Result{}, capability.ErrInvalidInput
	}
	report, err := tool.port.GetReview(ctx, call, parsed)
	if err != nil {
		return capability.Result{}, err
	}
	return capability.Result{
		Content:    map[string]any{"report": report},
		SourceRefs: report.SourceRefs,
	}, nil
}
