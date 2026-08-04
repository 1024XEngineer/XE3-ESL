package agentcapability

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	domainreview "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
)

const defaultReviewSearchLimit = 10

type ServicePort struct {
	history *domainreview.HistoryService
}

func NewServicePort(history *domainreview.HistoryService) (*ServicePort, error) {
	if history == nil {
		return nil, errors.New("review capability: history service is required")
	}
	return &ServicePort{history: history}, nil
}

func (port *ServicePort) SearchReviews(
	ctx context.Context,
	call capability.CallContext,
	input ReviewSearchInput,
) ([]ReviewSummary, error) {
	if port == nil || port.history == nil || !call.Actor.Valid() {
		return nil, capability.ErrExecutionRejected
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultReviewSearchLimit
	}
	actor := domainreview.Actor{UserID: call.Actor.UserID}
	query := strings.TrimSpace(input.Query)
	practiceSessionID := strings.TrimSpace(input.PracticeSessionID)
	var items []domainreview.Report
	var err error
	if query == "" && practiceSessionID == "" {
		var page domainreview.HistoryPage
		page, err = port.history.ListCompleted(
			ctx,
			actor,
			domainreview.HistoryQuery{Limit: limit},
		)
		items = page.Items
	} else {
		if query == "" {
			query = practiceSessionID
		}
		items, err = port.history.SearchCompleted(
			ctx,
			actor,
			domainreview.HistorySearchQuery{
				Query:             query,
				PracticeSessionID: practiceSessionID,
				Limit:             limit,
			},
		)
	}
	if err != nil {
		return nil, mapReviewToolError(err)
	}
	result := make([]ReviewSummary, len(items))
	for index, item := range items {
		if !item.Valid() {
			return nil, capability.ErrExecutionRejected
		}
		result[index] = mapReviewSummary(item)
	}
	return result, nil
}

func (port *ServicePort) GetReview(
	ctx context.Context,
	call capability.CallContext,
	input ReviewGetInput,
) (ReviewDetail, error) {
	if port == nil || port.history == nil || !call.Actor.Valid() {
		return ReviewDetail{}, capability.ErrExecutionRejected
	}
	item, err := port.history.Get(
		ctx,
		domainreview.Actor{UserID: call.Actor.UserID},
		input.ReportID,
	)
	if err != nil {
		return ReviewDetail{}, mapReviewToolError(err)
	}
	if !item.Valid() {
		return ReviewDetail{}, capability.ErrExecutionRejected
	}
	return mapReviewDetail(item), nil
}

func mapReviewSummary(item domainreview.Report) ReviewSummary {
	return ReviewSummary{
		ID:                item.ID,
		PracticeSessionID: item.PracticeSessionID,
		SceneType:         item.SceneType,
		SceneModel:        item.SceneModel,
		Scoreability:      item.ScoreabilityStatus,
		Summary:           item.Summary,
		CompletedAt:       item.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []capability.SourceRef{{
			Type: "evaluation_report",
			ID:   item.ID,
		}},
	}
}

func mapReviewDetail(item domainreview.Report) ReviewDetail {
	dimensions := make([]ReviewDimension, len(item.Dimensions))
	for index, dimension := range item.Dimensions {
		dimensions[index] = ReviewDimension{
			Key:          dimension.Key,
			Score:        cloneScore(dimension.Score),
			Scale:        dimension.Scale,
			Coverage:     dimension.Coverage,
			Confidence:   dimension.Confidence,
			Strengths:    mapFindings(dimension.Strengths),
			Improvements: mapFindings(dimension.Improvements),
			Examples:     mapFindings(dimension.Examples),
		}
	}
	actions := make([]ReviewPriorityAction, len(item.PriorityActions))
	for index, action := range item.PriorityActions {
		actions[index] = ReviewPriorityAction{
			DimensionKey: action.DimensionKey,
			FindingID:    action.FindingID,
		}
	}
	return ReviewDetail{
		ID:                item.ID,
		PracticeSessionID: item.PracticeSessionID,
		SceneType:         item.SceneType,
		SceneModel:        item.SceneModel,
		Scoreability:      item.ScoreabilityStatus,
		Summary:           item.Summary,
		Dimensions:        dimensions,
		PriorityActions:   actions,
		CompletedAt:       item.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []capability.SourceRef{{
			Type: "evaluation_report",
			ID:   item.ID,
		}},
	}
}

func mapFindings(items []domainreview.ReportFinding) []ReviewFinding {
	result := make([]ReviewFinding, len(items))
	for index, item := range items {
		excerpts := make([]string, 0, len(item.Evidence))
		for _, evidence := range item.Evidence {
			if strings.TrimSpace(evidence.OriginalExcerpt) != "" {
				excerpts = append(excerpts, evidence.OriginalExcerpt)
			}
		}
		result[index] = ReviewFinding{
			ID:               item.ID,
			Message:          item.Message,
			Suggestion:       item.Suggestion,
			OriginalExcerpts: excerpts,
		}
	}
	return result
}

func cloneScore(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func mapReviewToolError(err error) error {
	switch {
	case errors.Is(err, domainreview.ErrInvalidReview):
		return capability.ErrInvalidInput
	case errors.Is(err, domainreview.ErrReviewNotFound),
		errors.Is(err, domainreview.ErrAccountDeleted):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
