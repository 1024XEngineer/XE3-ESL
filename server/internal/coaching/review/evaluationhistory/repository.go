package evaluationhistory

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
)

type Source interface {
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

type Repository struct {
	source Source
}

func New(source Source) (*Repository, error) {
	if source == nil {
		return nil, errors.New(
			"review: Evaluation report history source is required",
		)
	}
	return &Repository{source: source}, nil
}

func (history *Repository) GetReport(
	ctx context.Context,
	actor review.Actor,
	reportID string,
) (review.Report, error) {
	item, err := history.source.GetFormalReport(
		ctx,
		actor.UserID,
		reportID,
	)
	if err != nil {
		return review.Report{}, mapEvaluationReportHistoryError(err)
	}
	return mapEvaluationReport(item)
}

func (history *Repository) ListReports(
	ctx context.Context,
	actor review.Actor,
	query review.HistoryQuery,
) (review.HistoryPage, error) {
	var before *report.HistoryBoundary
	if query.Before != nil {
		before = &report.HistoryBoundary{
			CreatedAt: query.Before.CreatedAt,
			ReportID:  query.Before.ReportID,
		}
	}
	page, err := history.source.ListFormalReports(
		ctx,
		actor.UserID,
		report.HistoryQuery{
			Limit:  query.Limit,
			Before: before,
		},
	)
	if err != nil {
		return review.HistoryPage{}, mapEvaluationReportHistoryError(err)
	}
	items, err := mapEvaluationReports(page.Items)
	if err != nil {
		return review.HistoryPage{}, err
	}
	result := review.HistoryPage{Items: items}
	if page.HasMore && len(items) > 0 {
		last := items[len(items)-1]
		result.Next = &review.HistoryCursor{
			CreatedAt: last.CreatedAt,
			ReportID:  last.ID,
		}
	}
	return result, nil
}

func (history *Repository) SearchReports(
	ctx context.Context,
	actor review.Actor,
	query review.HistorySearchQuery,
) ([]review.Report, error) {
	page, err := history.source.ListFormalReports(
		ctx,
		actor.UserID,
		report.HistoryQuery{
			Limit:             query.Limit,
			Search:            query.Query,
			PracticeSessionID: query.PracticeSessionID,
		},
	)
	if err != nil {
		return nil, mapEvaluationReportHistoryError(err)
	}
	return mapEvaluationReports(page.Items)
}

func mapEvaluationReports(
	items []report.StoredFormalReport,
) ([]review.Report, error) {
	result := make([]review.Report, len(items))
	for index, item := range items {
		mapped, err := mapEvaluationReport(item)
		if err != nil {
			return nil, err
		}
		result[index] = mapped
	}
	return result, nil
}

func mapEvaluationReport(
	item report.StoredFormalReport,
) (review.Report, error) {
	dimensions := make([]review.ReportDimension, len(item.Report.Dimensions))
	for index, dimension := range item.Report.Dimensions {
		dimensions[index] = review.ReportDimension{
			Key:          dimension.Key,
			Score:        cloneReportScore(dimension.Score),
			Scale:        string(dimension.Scale),
			Coverage:     dimension.Coverage,
			Confidence:   dimension.Confidence,
			ReasonCodes:  cloneReportStrings(dimension.ReasonCodes),
			EvidenceRefs: cloneReportStrings(dimension.EvidenceRefs),
			Strengths:    mapEvaluationFindings(dimension.Strengths),
			Improvements: mapEvaluationFindings(dimension.Improvements),
			Examples:     mapEvaluationFindings(dimension.Examples),
		}
	}
	actions := make(
		[]review.ReportPriorityAction,
		len(item.Report.PriorityActions),
	)
	for index, action := range item.Report.PriorityActions {
		actions[index] = review.ReportPriorityAction{
			DimensionKey: action.DimensionKey,
			FindingID:    action.FindingID,
		}
	}
	mapped := review.Report{
		ID:                   item.ReportID,
		EvaluationID:         item.EvaluationID,
		EvaluationRevisionID: item.EvaluationRevisionID,
		OwnerUserID:          item.OwnerUserID,
		PracticeSessionID:    item.PracticeSessionID,
		Revision:             item.Revision,
		SchemaVersion:        item.Report.SchemaVersion,
		SceneType:            string(item.Report.SceneType),
		PracticeExperience:   item.Report.PracticeExperience,
		SceneCategory:        item.Report.SceneCategory,
		PracticeMode:         item.Report.PracticeMode,
		ScoreabilityStatus:   string(item.Report.ScoreabilityStatus),
		Summary:              item.Report.Summary,
		Dimensions:           dimensions,
		PriorityActions:      actions,
		DetailSchema:         item.Report.DetailSchema,
		Detail:               append([]byte(nil), item.Report.Detail...),
		CreatedAt:            item.CreatedAt,
	}
	if !mapped.Valid() {
		return review.Report{}, review.ErrInvalidReview
	}
	return mapped, nil
}

func mapEvaluationFindings(
	items []report.ReportFinding,
) []review.ReportFinding {
	result := make([]review.ReportFinding, len(items))
	for index, item := range items {
		evidence := make([]review.ReportEvidence, len(item.Evidence))
		for evidenceIndex, source := range item.Evidence {
			evidence[evidenceIndex] = review.ReportEvidence{
				EvidenceRefID:   source.EvidenceRefID,
				TurnID:          source.TurnID,
				StartUTF8Byte:   source.StartUTF8Byte,
				EndUTF8Byte:     source.EndUTF8Byte,
				OriginalExcerpt: source.OriginalExcerpt,
			}
		}
		result[index] = review.ReportFinding{
			ID:         item.ID,
			Message:    item.Message,
			Suggestion: item.Suggestion,
			Evidence:   evidence,
		}
	}
	return result
}

func cloneReportScore(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneReportStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func mapEvaluationReportHistoryError(err error) error {
	switch {
	case errors.Is(err, evaluation.ErrInvalidRequest):
		return review.ErrInvalidReview
	case errors.Is(err, evaluation.ErrNotFound):
		return review.ErrReviewNotFound
	case errors.Is(err, evaluation.ErrAccountUnavailable):
		return review.ErrAccountDeleted
	default:
		return err
	}
}

var _ review.HistoryRepository = (*Repository)(nil)
