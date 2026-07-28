package tools

import (
	"context"
	"encoding/json"
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

// NewReviewSearchTool creates the adapter for the review search tool.
func NewReviewSearchTool(port ReviewPort) ReviewSearchTool {
	return ReviewSearchTool{port: port}
}

// Definition describes review.search.v1 for model and command exposure.
func (tool ReviewSearchTool) Definition() Definition {
	return Definition{
		Name:        ReviewSearchToolName,
		Description: "Search the user's historical practice reviews or interview evaluations.",
		InputSchema: objectSchema(map[string]any{
			"query":       stringSchema("What review or evaluation the user wants to find."),
			"scenario_id": stringSchema("Optional scenario id to restrict the search."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of reviews to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute validates search input and delegates review lookup to the ReviewPort.
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

// NewReviewGetTool creates the adapter for reading one review.
func NewReviewGetTool(port ReviewPort) ReviewGetTool {
	return ReviewGetTool{port: port}
}

// Definition describes review.get.v1 for model exposure.
func (tool ReviewGetTool) Definition() Definition {
	return Definition{
		Name:        ReviewGetToolName,
		Description: "Read one structured review or interview evaluation by id.",
		InputSchema: objectSchema(map[string]any{
			"review_id": stringSchema("Review id to read."),
		}, []string{"review_id"}),
		ReadOnly: true,
		Risk:     RiskReadOnly,
	}
}

// Execute validates get input and delegates review detail loading to the ReviewPort.
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

// reviewMap returns the compact JSON object exposed back to the model for a review.
func reviewMap(review ReviewSummary) map[string]any {
	return map[string]any{
		"id":          review.ID,
		"title":       review.Title,
		"summary":     review.Summary,
		"scenario_id": review.ScenarioID,
	}
}
