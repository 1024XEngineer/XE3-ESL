package agenttool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	domainreview "github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

const (
	servicePortUserID  = "10000000-0000-4000-8000-000000000001"
	servicePortOtherID = "10000000-0000-4000-8000-000000000002"
	servicePortReview  = "20000000-0000-4000-8000-000000000001"
)

type historyRepositoryStub struct {
	item        domainreview.FormalReview
	searchItems []domainreview.FormalReview
	searchQuery domainreview.HistorySearchQuery
	searchActor domainreview.Actor
	getActor    domainreview.Actor
	getID       string
	err         error
}

func (stub *historyRepositoryStub) Get(
	ctx context.Context,
	actor domainreview.Actor,
	reviewID string,
) (domainreview.FormalReview, error) {
	stub.getActor = actor
	stub.getID = reviewID
	if stub.err != nil {
		return domainreview.FormalReview{}, stub.err
	}
	if actor.UserID != stub.item.OwnerUserID || reviewID != stub.item.ID {
		return domainreview.FormalReview{}, domainreview.ErrReviewNotFound
	}
	return stub.item, nil
}

func (stub *historyRepositoryStub) ListCompletedHistory(
	context.Context,
	domainreview.Actor,
	domainreview.HistoryQuery,
) (domainreview.HistoryPage, error) {
	return domainreview.HistoryPage{}, nil
}

func (stub *historyRepositoryStub) SearchCompletedHistory(
	ctx context.Context,
	actor domainreview.Actor,
	query domainreview.HistorySearchQuery,
) ([]domainreview.FormalReview, error) {
	stub.searchActor = actor
	stub.searchQuery = query
	if stub.err != nil {
		return nil, stub.err
	}
	return append([]domainreview.FormalReview(nil), stub.searchItems...), nil
}

func TestServicePortSearchesCompletedFormalReviews(t *testing.T) {
	item := completedFormalReview()
	repository := &historyRepositoryStub{
		item:        item,
		searchItems: []domainreview.FormalReview{item},
	}
	port, err := NewServicePort(domainreview.NewHistoryService(repository))
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	result, err := port.SearchReviews(
		context.Background(),
		servicePortCallContext(servicePortUserID),
		ReviewSearchInput{
			Query:             "metrics",
			PracticeSessionID: item.PracticeSessionID,
		},
	)
	if err != nil {
		t.Fatalf("SearchReviews() error = %v", err)
	}
	if repository.searchActor.UserID != servicePortUserID ||
		repository.searchQuery.Limit != defaultReviewSearchLimit ||
		repository.searchQuery.PracticeSessionID != item.PracticeSessionID {
		t.Fatalf(
			"search actor/query = %+v / %+v",
			repository.searchActor,
			repository.searchQuery,
		)
	}
	if len(result) != 1 ||
		result[0].ID != item.ID ||
		result[0].Summary != item.Result.Summary ||
		result[0].ScenarioDefinitionID !=
			item.EvaluationContext.ScenarioDefinitionID ||
		len(result[0].SourceRefs) != 1 ||
		result[0].SourceRefs[0].Type != "formal_review" {
		t.Fatalf("SearchReviews() = %+v", result)
	}
}

func TestServicePortGetsStructuredFormalReview(t *testing.T) {
	item := completedFormalReview()
	repository := &historyRepositoryStub{item: item}
	port, err := NewServicePort(domainreview.NewHistoryService(repository))
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	result, err := port.GetReview(
		context.Background(),
		servicePortCallContext(servicePortUserID),
		ReviewGetInput{ReviewID: item.ID},
	)
	if err != nil {
		t.Fatalf("GetReview() error = %v", err)
	}
	if repository.getActor.UserID != servicePortUserID ||
		repository.getID != item.ID {
		t.Fatalf("get actor/id = %+v / %q", repository.getActor, repository.getID)
	}
	if result.SummaryEligibility != string(domainreview.SummaryEligible) ||
		result.OverallScore == nil ||
		*result.OverallScore != 88 ||
		len(result.Conclusions) != 1 ||
		len(result.FeedbackItems) != 1 ||
		result.CompletedAt == "" {
		t.Fatalf("GetReview() = %+v", result)
	}
}

func TestServicePortDoesNotExposeForeignOrIncompleteReview(t *testing.T) {
	item := completedFormalReview()
	repository := &historyRepositoryStub{item: item}
	port, err := NewServicePort(domainreview.NewHistoryService(repository))
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}

	_, err = port.GetReview(
		context.Background(),
		servicePortCallContext(servicePortOtherID),
		ReviewGetInput{ReviewID: item.ID},
	)
	if !errors.Is(err, tool.ErrExecutionRejected) {
		t.Fatalf("foreign GetReview() error = %v", err)
	}

	incomplete := item
	incomplete.Status = domainreview.FormalReviewPending
	incomplete.Result = nil
	repository.item = incomplete
	_, err = port.GetReview(
		context.Background(),
		servicePortCallContext(servicePortUserID),
		ReviewGetInput{ReviewID: item.ID},
	)
	if !errors.Is(err, tool.ErrExecutionRejected) {
		t.Fatalf("incomplete GetReview() error = %v", err)
	}
}

func completedFormalReview() domainreview.FormalReview {
	completedAt := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	return domainreview.FormalReview{
		ID:                servicePortReview,
		OwnerUserID:       servicePortUserID,
		PracticeSessionID: "practice-session-1",
		Status:            domainreview.FormalReviewCompleted,
		EvaluationContext: domainreview.EvaluationContext{
			ScenarioDefinitionID: "scn_programmer_interview",
		},
		Result: &domainreview.ReviewResult{
			SummaryEligibility:  domainreview.SummaryEligible,
			OverallScore:        88,
			OverallScorePresent: true,
			Summary:             "Clear structure with measurable impact.",
			Conclusions: []domainreview.ReviewConclusion{{
				Key:      "clarity",
				Category: "clarity",
				Score:    88,
				Message:  "The answer was clear.",
			}},
			FeedbackItems: []domainreview.ReviewFeedbackItem{{
				Key:     "metrics",
				Kind:    domainreview.FeedbackImprovement,
				Message: "Add one concrete metric.",
			}},
			RepracticeSuggestionRefs: []string{"practice-session-2"},
		},
		CompletedAt: &completedAt,
	}
}

func servicePortCallContext(userID string) tool.CallContext {
	return tool.CallContext{
		Actor: requestcontext.Actor{
			UserID:    userID,
			SessionID: "identity-session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}
