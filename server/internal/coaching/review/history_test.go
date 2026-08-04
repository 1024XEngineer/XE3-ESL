package review

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type historySearchRepositoryStub struct {
	query HistorySearchQuery
}

func (stub *historySearchRepositoryStub) Get(
	context.Context,
	Actor,
	string,
) (FormalReview, error) {
	return FormalReview{}, nil
}

func (stub *historySearchRepositoryStub) ListCompletedHistory(
	context.Context,
	Actor,
	HistoryQuery,
) (HistoryPage, error) {
	return HistoryPage{}, nil
}

func (stub *historySearchRepositoryStub) SearchCompletedHistory(
	ctx context.Context,
	actor Actor,
	query HistorySearchQuery,
) ([]FormalReview, error) {
	stub.query = query
	return []FormalReview{}, nil
}

func TestHistoryServiceValidatesAndNormalizesSearch(t *testing.T) {
	repository := &historySearchRepositoryStub{}
	service := NewHistoryService(repository)
	actor := Actor{UserID: "10000000-0000-4000-8000-000000000001"}

	if _, err := service.SearchCompleted(
		context.Background(),
		actor,
		HistorySearchQuery{
			Query:             "  metrics  ",
			PracticeSessionID: "  practice-session-1  ",
			Limit:             10,
		},
	); err != nil {
		t.Fatalf("SearchCompleted() error = %v", err)
	}
	if repository.query.Query != "metrics" ||
		repository.query.PracticeSessionID != "practice-session-1" {
		t.Fatalf("normalized search query = %+v", repository.query)
	}

	invalid := []HistorySearchQuery{
		{Query: "", Limit: 1},
		{Query: "metrics", Limit: 0},
		{Query: "metrics", Limit: MaxHistorySearchPageSize + 1},
		{Query: strings.Repeat("x", maxHistorySearchRunes+1), Limit: 1},
		{Query: "metrics", PracticeSessionID: "bad\nid", Limit: 1},
	}
	for _, query := range invalid {
		if _, err := service.SearchCompleted(
			context.Background(),
			actor,
			query,
		); !errors.Is(err, ErrInvalidReview) {
			t.Fatalf("SearchCompleted(%+v) error = %v", query, err)
		}
	}
}
