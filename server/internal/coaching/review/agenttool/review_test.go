package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

func TestReviewSearchToolReturnsReportsAndEvaluationSources(t *testing.T) {
	t.Parallel()
	port := &reviewPortStub{summaries: []ReviewSummary{{
		ID:                "10000000-0000-4000-8000-000000000001",
		PracticeSessionID: "practice-session-1",
		SceneType:         "OVERSEAS_DAILY_LIFE",
		SceneModel:        "DAILY_BASIC_DIALOGUE",
		Scoreability:      "PROVISIONAL",
		Summary:           "Completed report",
		CompletedAt:       "2026-08-04T08:00:00Z",
		SourceRefs: []tool.SourceRef{{
			Type: "evaluation_report",
			ID:   "10000000-0000-4000-8000-000000000001",
		}},
	}}}
	reviewTool := NewReviewSearchTool(port)
	if err := tool.ValidateDefinition(reviewTool.Definition()); err != nil {
		t.Fatal(err)
	}
	result, err := reviewTool.Execute(
		context.Background(),
		validReviewCallContext(),
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	reports, ok := result.Content["reports"].([]ReviewSummary)
	if !ok || len(reports) != 1 || len(result.SourceRefs) != 1 ||
		result.SourceRefs[0].Type != "evaluation_report" {
		t.Fatalf("result = %#v", result)
	}
}

func TestReviewGetToolRejectsMissingReportID(t *testing.T) {
	t.Parallel()
	_, err := NewReviewGetTool(&reviewPortStub{}).Execute(
		context.Background(),
		validReviewCallContext(),
		json.RawMessage(`{}`),
	)
	if !errors.Is(err, tool.ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
}

type reviewPortStub struct {
	summaries []ReviewSummary
}

func (port *reviewPortStub) SearchReviews(
	context.Context,
	tool.CallContext,
	ReviewSearchInput,
) ([]ReviewSummary, error) {
	return port.summaries, nil
}

func (port *reviewPortStub) GetReview(
	context.Context,
	tool.CallContext,
	ReviewGetInput,
) (ReviewDetail, error) {
	return ReviewDetail{}, nil
}
