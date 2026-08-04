package review

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxHistoryPageSize       = 50
	MaxHistorySearchPageSize = 20
	maxHistorySearchRunes    = 500
	maxHistorySearchBytes    = 2000
)

type HistoryCursor struct {
	CreatedAt time.Time
	ReportID  string
}

type HistoryQuery struct {
	Limit  int
	Before *HistoryCursor
}

type HistoryPage struct {
	Items []Report
	Next  *HistoryCursor
}

type HistorySearchQuery struct {
	Query             string
	PracticeSessionID string
	Limit             int
}

type HistoryRepository interface {
	GetReport(context.Context, Actor, string) (Report, error)
	ListReports(context.Context, Actor, HistoryQuery) (HistoryPage, error)
	SearchReports(
		context.Context,
		Actor,
		HistorySearchQuery,
	) ([]Report, error)
}

type HistoryService struct {
	repository HistoryRepository
}

func NewHistoryService(repository HistoryRepository) *HistoryService {
	return &HistoryService{repository: repository}
}

func (service *HistoryService) Get(
	ctx context.Context,
	actor Actor,
	reportID string,
) (Report, error) {
	if service == nil || service.repository == nil || ctx == nil ||
		actor.validate() != nil || !validUUID(reportID) {
		return Report{}, ErrInvalidReview
	}
	return service.repository.GetReport(ctx, actor, reportID)
}

func (service *HistoryService) ListCompleted(
	ctx context.Context,
	actor Actor,
	query HistoryQuery,
) (HistoryPage, error) {
	if service == nil || service.repository == nil || ctx == nil ||
		actor.validate() != nil || query.Limit < 1 ||
		query.Limit > MaxHistoryPageSize ||
		(query.Before != nil && !validHistoryCursor(*query.Before)) {
		return HistoryPage{}, ErrInvalidReview
	}
	return service.repository.ListReports(ctx, actor, query)
}

func (service *HistoryService) SearchCompleted(
	ctx context.Context,
	actor Actor,
	query HistorySearchQuery,
) ([]Report, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.PracticeSessionID = strings.TrimSpace(query.PracticeSessionID)
	if service == nil || service.repository == nil || ctx == nil ||
		actor.validate() != nil || !validHistorySearchQuery(query) {
		return nil, ErrInvalidReview
	}
	return service.repository.SearchReports(ctx, actor, query)
}

func validHistoryCursor(cursor HistoryCursor) bool {
	return !cursor.CreatedAt.IsZero() && validUUID(cursor.ReportID)
}

func validHistorySearchQuery(query HistorySearchQuery) bool {
	return query.Query != "" && utf8.ValidString(query.Query) &&
		!strings.ContainsRune(query.Query, '\x00') &&
		utf8.RuneCountInString(query.Query) <= maxHistorySearchRunes &&
		len(query.Query) <= maxHistorySearchBytes &&
		(query.PracticeSessionID == "" ||
			validRetryRequestResourceID(query.PracticeSessionID)) &&
		query.Limit >= 1 && query.Limit <= MaxHistorySearchPageSize
}
