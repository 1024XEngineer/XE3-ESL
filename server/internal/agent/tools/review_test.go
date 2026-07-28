package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeReviewPort struct {
	searchInput ReviewSearchInput
	getInput    ReviewGetInput
}

func (port *fakeReviewPort) SearchReviews(
	ctx context.Context,
	call CallContext,
	input ReviewSearchInput,
) ([]ReviewSummary, error) {
	port.searchInput = input
	return []ReviewSummary{{
		ID:      "review-1",
		Title:   "Mock interview review",
		Summary: "Answer was too long.",
	}}, nil
}

func (port *fakeReviewPort) GetReview(
	ctx context.Context,
	call CallContext,
	input ReviewGetInput,
) (ReviewSummary, error) {
	port.getInput = input
	return ReviewSummary{
		ID:      input.ReviewID,
		Title:   "Mock interview review",
		Summary: "Answer was too long.",
	}, nil
}

func TestReviewSearchToolMapsInput(t *testing.T) {
	port := &fakeReviewPort{}
	result, err := NewReviewSearchTool(port).Execute(
		context.Background(),
		validCallContext(),
		json.RawMessage(`{"query":"上次面试评价"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := port.searchInput.Query, "上次面试评价"; got != want {
		t.Fatalf("searchInput.Query = %q, want %q", got, want)
	}
	if result.Content["reviews"] == nil {
		t.Fatalf("result.Content = %#v, want reviews", result.Content)
	}
}

func TestReviewGetToolMapsInput(t *testing.T) {
	port := &fakeReviewPort{}
	result, err := NewReviewGetTool(port).Execute(
		context.Background(),
		validCallContext(),
		json.RawMessage(`{"review_id":"review-1"}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := port.getInput.ReviewID, "review-1"; got != want {
		t.Fatalf("getInput.ReviewID = %q, want %q", got, want)
	}
	if result.Content["review"] == nil {
		t.Fatalf("result.Content = %#v, want review", result.Content)
	}
}
