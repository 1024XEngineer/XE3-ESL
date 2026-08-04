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

// HistoryCursor is the stable keyset boundary for the Review-owned history
// read model. CreatedAt and ReviewID match the repository order exactly.
type HistoryCursor struct {
	CreatedAt time.Time
	ReviewID  string
}

type HistoryQuery struct {
	Limit  int
	Before *HistoryCursor
}

type HistoryPage struct {
	Items []FormalReview
	Next  *HistoryCursor
}

type HistorySearchQuery struct {
	Query             string
	PracticeSessionID string
	Limit             int
}

// HistoryRepository is deliberately narrower than the mutation repository.
// The Review module remains the only owner of its history query semantics.
type HistoryRepository interface {
	Get(
		ctx context.Context,
		actor Actor,
		reviewID string,
	) (FormalReview, error)
	ListCompletedHistory(
		ctx context.Context,
		actor Actor,
		query HistoryQuery,
	) (HistoryPage, error)
	SearchCompletedHistory(
		ctx context.Context,
		actor Actor,
		query HistorySearchQuery,
	) ([]FormalReview, error)
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
	reviewID string,
) (FormalReview, error) {
	if service == nil || service.repository == nil ||
		actor.validate() != nil || strings.TrimSpace(reviewID) == "" {
		return FormalReview{}, ErrInvalidReview
	}
	return service.repository.Get(ctx, actor, reviewID)
}

// ListCompleted returns only authoritative completed results. Offset
// pagination is intentionally excluded because concurrent Review creation
// would otherwise duplicate or skip rows.
func (service *HistoryService) ListCompleted(
	ctx context.Context,
	actor Actor,
	query HistoryQuery,
) (HistoryPage, error) {
	if service == nil || service.repository == nil ||
		actor.validate() != nil || query.Limit < 1 ||
		query.Limit > MaxHistoryPageSize ||
		(query.Before != nil && !validHistoryCursor(*query.Before)) {
		return HistoryPage{}, ErrInvalidReview
	}
	return service.repository.ListCompletedHistory(ctx, actor, query)
}

// SearchCompleted returns a bounded set of authoritative Review results. Search
// semantics remain Review-owned so callers never need to scan or filter history.
func (service *HistoryService) SearchCompleted(
	ctx context.Context,
	actor Actor,
	query HistorySearchQuery,
) ([]FormalReview, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.PracticeSessionID = strings.TrimSpace(query.PracticeSessionID)
	if service == nil || service.repository == nil ||
		actor.validate() != nil || !validHistorySearchQuery(query) {
		return nil, ErrInvalidReview
	}
	return service.repository.SearchCompletedHistory(ctx, actor, query)
}

func validHistoryCursor(cursor HistoryCursor) bool {
	return !cursor.CreatedAt.IsZero() &&
		validUUID(cursor.ReviewID)
}

func validHistorySearchQuery(query HistorySearchQuery) bool {
	return query.Query != "" &&
		utf8.ValidString(query.Query) &&
		!strings.ContainsRune(query.Query, '\x00') &&
		utf8.RuneCountInString(query.Query) <= maxHistorySearchRunes &&
		len(query.Query) <= maxHistorySearchBytes &&
		(query.PracticeSessionID == "" ||
			validContextIdentifier(query.PracticeSessionID, 128)) &&
		query.Limit >= 1 &&
		query.Limit <= MaxHistorySearchPageSize
}
