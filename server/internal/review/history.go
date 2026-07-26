package review

import (
	"context"
	"strings"
	"time"
)

const MaxHistoryPageSize = 50

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

func validHistoryCursor(cursor HistoryCursor) bool {
	return !cursor.CreatedAt.IsZero() &&
		validUUID(cursor.ReviewID)
}
