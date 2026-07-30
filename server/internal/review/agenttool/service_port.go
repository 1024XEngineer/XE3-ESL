package agenttool

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	domainreview "github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

const defaultReviewSearchLimit = 10

// ServicePort adapts Review's ordinary application service to Agent Tool DTOs.
// It never reads Review tables directly and derives ownership from CallContext.
type ServicePort struct {
	history *domainreview.HistoryService
}

func NewServicePort(history *domainreview.HistoryService) (*ServicePort, error) {
	if history == nil {
		return nil, errors.New("review agenttool: history service is required")
	}
	return &ServicePort{history: history}, nil
}

func (port *ServicePort) SearchReviews(
	ctx context.Context,
	call tool.CallContext,
	input ReviewSearchInput,
) ([]ReviewSummary, error) {
	if port == nil || port.history == nil || !call.Actor.Valid() {
		return nil, tool.ErrExecutionRejected
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultReviewSearchLimit
	}
	items, err := port.history.SearchCompleted(
		ctx,
		domainreview.Actor{UserID: call.Actor.UserID},
		domainreview.HistorySearchQuery{
			Query:             input.Query,
			PracticeSessionID: input.PracticeSessionID,
			Limit:             limit,
		},
	)
	if err != nil {
		return nil, mapReviewToolError(err)
	}
	result := make([]ReviewSummary, 0, len(items))
	for _, item := range items {
		if item.Result == nil || item.Status != domainreview.FormalReviewCompleted {
			return nil, tool.ErrExecutionRejected
		}
		result = append(result, mapReviewSummary(item))
	}
	return result, nil
}

func (port *ServicePort) GetReview(
	ctx context.Context,
	call tool.CallContext,
	input ReviewGetInput,
) (ReviewDetail, error) {
	if port == nil || port.history == nil || !call.Actor.Valid() {
		return ReviewDetail{}, tool.ErrExecutionRejected
	}
	item, err := port.history.Get(
		ctx,
		domainreview.Actor{UserID: call.Actor.UserID},
		input.ReviewID,
	)
	if err != nil {
		return ReviewDetail{}, mapReviewToolError(err)
	}
	if item.Status != domainreview.FormalReviewCompleted || item.Result == nil {
		return ReviewDetail{}, tool.ErrExecutionRejected
	}
	return mapReviewDetail(item), nil
}

func mapReviewSummary(item domainreview.FormalReview) ReviewSummary {
	return ReviewSummary{
		ID:                   item.ID,
		PracticeSessionID:    item.PracticeSessionID,
		ScenarioDefinitionID: item.EvaluationContext.ScenarioDefinitionID,
		Summary:              item.Result.Summary,
		CompletedAt:          formatReviewTime(item.CompletedAt),
		SourceRefs: []tool.SourceRef{{
			Type: "formal_review",
			ID:   item.ID,
		}},
	}
}

func mapReviewDetail(item domainreview.FormalReview) ReviewDetail {
	result := item.Result
	detail := ReviewDetail{
		ID:                          item.ID,
		PracticeSessionID:           item.PracticeSessionID,
		ScenarioDefinitionID:        item.EvaluationContext.ScenarioDefinitionID,
		Status:                      string(item.Status),
		SummaryEligibility:          string(result.SummaryEligibility),
		Summary:                     result.Summary,
		Conclusions:                 make([]ReviewConclusion, len(result.Conclusions)),
		FeedbackItems:               make([]ReviewFeedbackItem, len(result.FeedbackItems)),
		RepracticeSuggestionRefs:    append([]string(nil), result.RepracticeSuggestionRefs...),
		InsufficientEvidenceReasons: append([]string(nil), result.InsufficientEvidenceReasons...),
		CompletedAt:                 formatReviewTime(item.CompletedAt),
		SourceRefs: []tool.SourceRef{{
			Type: "formal_review",
			ID:   item.ID,
		}},
	}
	if result.OverallScorePresent {
		score := result.OverallScore
		detail.OverallScore = &score
	}
	for index, conclusion := range result.Conclusions {
		detail.Conclusions[index] = ReviewConclusion{
			Key:        conclusion.Key,
			Category:   conclusion.Category,
			Score:      conclusion.Score,
			Message:    conclusion.Message,
			Suggestion: conclusion.Suggestion,
		}
	}
	for index, item := range result.FeedbackItems {
		detail.FeedbackItems[index] = ReviewFeedbackItem{
			Key:        item.Key,
			Kind:       string(item.Kind),
			Message:    item.Message,
			Suggestion: item.Suggestion,
		}
	}
	return detail
}

func formatReviewTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func mapReviewToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domainreview.ErrInvalidReview):
		return tool.ErrInvalidInput
	case errors.Is(err, domainreview.ErrReviewNotFound),
		errors.Is(err, domainreview.ErrAccountDeleted):
		return tool.ErrExecutionRejected
	default:
		return err
	}
}
