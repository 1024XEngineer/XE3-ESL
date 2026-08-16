package agentcapability

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
)

const defaultReviewSearchLimit = 10

type reportReader interface {
	GetFormalReport(
		context.Context,
		string,
		string,
	) (report.StoredFormalReport, error)
	ListFormalReports(
		context.Context,
		string,
		report.HistoryQuery,
	) (report.HistoryPage, error)
}

type ServicePort struct {
	reports reportReader
}

func NewServicePort(reports reportReader) (*ServicePort, error) {
	if reports == nil {
		return nil, errors.New("review capability: report reader is required")
	}
	return &ServicePort{reports: reports}, nil
}

func (port *ServicePort) SearchReviews(
	ctx context.Context,
	call capability.CallContext,
	input ReviewSearchInput,
) ([]ReviewSummary, error) {
	if port == nil || port.reports == nil || !call.Actor.Valid() {
		return nil, capability.ErrExecutionRejected
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultReviewSearchLimit
	}
	page, err := port.reports.ListFormalReports(
		ctx,
		call.Actor.UserID,
		report.HistoryQuery{
			Limit:  limit,
			Search: strings.TrimSpace(input.Query),
		},
	)
	if err != nil {
		return nil, mapReviewToolError(err)
	}
	result := make([]ReviewSummary, len(page.Items))
	for index, item := range page.Items {
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
	if port == nil || port.reports == nil || !call.Actor.Valid() {
		return ReviewDetail{}, capability.ErrExecutionRejected
	}
	item, err := port.reports.GetFormalReport(
		ctx,
		call.Actor.UserID,
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

func mapReviewSummary(item report.StoredFormalReport) ReviewSummary {
	return ReviewSummary{
		ID:                 item.ReportID,
		PracticeSessionID:  item.PracticeSessionID,
		SceneType:          string(item.Report.SceneType),
		PracticeExperience: item.Report.PracticeExperience,
		SceneCategory:      item.Report.SceneCategory,
		PracticeMode:       item.Report.PracticeMode,
		Scoreability:       string(item.Report.ScoreabilityStatus),
		Summary:            item.Report.Summary,
		CompletedAt:        item.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []capability.SourceRef{{
			Type: "evaluation_report",
			ID:   item.ReportID,
		}},
	}
}

func mapReviewDetail(item report.StoredFormalReport) ReviewDetail {
	dimensions := make([]ReviewDimension, len(item.Report.Dimensions))
	for index, dimension := range item.Report.Dimensions {
		dimensions[index] = ReviewDimension{
			Key:          dimension.Key,
			Score:        cloneScore(dimension.Score),
			Scale:        string(dimension.Scale),
			Coverage:     dimension.Coverage,
			Confidence:   dimension.Confidence,
			Strengths:    mapFindings(dimension.Strengths),
			Improvements: mapFindings(dimension.Improvements),
			Examples:     mapFindings(dimension.Examples),
		}
	}
	actions := make([]ReviewPriorityAction, len(item.Report.PriorityActions))
	for index, action := range item.Report.PriorityActions {
		actions[index] = ReviewPriorityAction{
			DimensionKey: action.DimensionKey,
			FindingID:    action.FindingID,
		}
	}
	return ReviewDetail{
		ID:                 item.ReportID,
		PracticeSessionID:  item.PracticeSessionID,
		SceneType:          string(item.Report.SceneType),
		PracticeExperience: item.Report.PracticeExperience,
		SceneCategory:      item.Report.SceneCategory,
		PracticeMode:       item.Report.PracticeMode,
		Scoreability:       string(item.Report.ScoreabilityStatus),
		Summary:            item.Report.Summary,
		Dimensions:         dimensions,
		PriorityActions:    actions,
		CompletedAt:        item.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []capability.SourceRef{{
			Type: "evaluation_report",
			ID:   item.ReportID,
		}},
	}
}

func mapFindings(items []report.ReportFinding) []ReviewFinding {
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
	case errors.Is(err, evaluation.ErrInvalidRequest):
		return capability.ErrInvalidInput
	case errors.Is(err, evaluation.ErrNotFound),
		errors.Is(err, evaluation.ErrAccountUnavailable):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
