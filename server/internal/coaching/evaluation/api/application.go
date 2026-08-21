package evaluationapi

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
)

type store interface {
	GetRecordBySource(context.Context, string, evaluation.Kind, string) (evaluation.Record, error)
	RetryFailedSessionReport(context.Context, string, string) (evaluation.Record, bool, error)
	ListFeedbackItems(context.Context, string, string) ([]evaluation.FeedbackItem, error)
	GetFormalReport(context.Context, string, string) (report.StoredFormalReport, error)
	ListFormalReports(context.Context, string, report.HistoryQuery) (report.HistoryPage, error)
}

type Application struct {
	store store
}

func NewApplication(store store) (*Application, error) {
	if store == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Application{store: store}, nil
}

type Resource struct {
	Evaluation    evaluation.Record
	FeedbackItems []evaluation.FeedbackItem
}

func (application *Application) GetBySource(
	ctx context.Context,
	userID string,
	kind evaluation.Kind,
	sourceID string,
) (Resource, error) {
	if application == nil || application.store == nil || ctx == nil {
		return Resource{}, evaluation.ErrInvalidRequest
	}
	record, err := application.store.GetRecordBySource(ctx, userID, kind, sourceID)
	if err != nil {
		return Resource{}, err
	}
	return application.resource(ctx, userID, record)
}

func (application *Application) RetrySessionReport(
	ctx context.Context,
	userID string,
	sessionID string,
) (Resource, bool, error) {
	if application == nil || application.store == nil || ctx == nil {
		return Resource{}, false, evaluation.ErrInvalidRequest
	}
	record, replayed, err := application.store.RetryFailedSessionReport(
		ctx, userID, sessionID,
	)
	if err != nil {
		return Resource{}, false, err
	}
	resource, err := application.resource(ctx, userID, record)
	return resource, replayed, err
}

func (application *Application) resource(
	ctx context.Context,
	userID string,
	record evaluation.Record,
) (Resource, error) {
	items := []evaluation.FeedbackItem{}
	if record.Status == evaluation.JobReady && record.Kind != evaluation.KindSessionReport {
		var err error
		items, err = application.store.ListFeedbackItems(ctx, userID, record.ID)
		if err != nil {
			return Resource{}, err
		}
	}
	return Resource{Evaluation: record, FeedbackItems: items}, nil
}

func (application *Application) GetReport(
	ctx context.Context,
	userID string,
	reportID string,
) (report.StoredFormalReport, error) {
	if application == nil || application.store == nil || ctx == nil {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	return application.store.GetFormalReport(ctx, userID, reportID)
}

func (application *Application) ListReports(
	ctx context.Context,
	userID string,
	query report.HistoryQuery,
) (report.HistoryPage, error) {
	if application == nil || application.store == nil || ctx == nil {
		return report.HistoryPage{}, evaluation.ErrInvalidRequest
	}
	page, err := application.store.ListFormalReports(ctx, userID, query)
	if errors.Is(err, evaluation.ErrNotFound) {
		return report.HistoryPage{Items: []report.StoredFormalReport{}}, nil
	}
	return page, err
}
