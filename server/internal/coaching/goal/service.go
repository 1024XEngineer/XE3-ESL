package goal

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxGoalTitleRunes       = 200
	maxGoalTitleBytes       = 512
	maxGoalSearchQueryRunes = 500
	maxGoalSearchQueryBytes = 2000
	defaultGoalSearchLimit  = 10
	MaxGoalSearchLimit      = 20
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("goal: repository is required")
	}
	return &Service{repository: repository}, nil
}

func (s *Service) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	title string,
) (Goal, error) {
	if !actor.Valid() {
		return Goal{}, ErrInvalidRequest
	}
	normalizedTitle, ok := normalizeTitle(title)
	if !ok {
		return Goal{}, ErrInvalidRequest
	}
	return s.repository.Create(ctx, actor.UserID, normalizedTitle)
}

func (s *Service) CreateIdempotent(
	ctx context.Context,
	actor requestcontext.Actor,
	requestID string,
	title string,
) (Goal, error) {
	if !actor.Valid() || !requestIDPattern.MatchString(requestID) {
		return Goal{}, ErrInvalidRequest
	}
	normalizedTitle, ok := normalizeTitle(title)
	if !ok {
		return Goal{}, ErrInvalidRequest
	}
	return s.repository.CreateIdempotent(
		ctx,
		actor.UserID,
		requestID,
		normalizedTitle,
	)
}

func (s *Service) List(
	ctx context.Context,
	actor requestcontext.Actor,
) ([]Goal, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	return s.repository.ListOwned(ctx, actor.UserID)
}

func (s *Service) Search(
	ctx context.Context,
	actor requestcontext.Actor,
	query SearchQuery,
) ([]Goal, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	normalizedQuery, ok := normalizeSearchQuery(query.Query)
	if !ok {
		return nil, ErrInvalidRequest
	}
	limit := query.Limit
	if limit == 0 {
		limit = defaultGoalSearchLimit
	}
	if limit < 1 || limit > MaxGoalSearchLimit {
		return nil, ErrInvalidRequest
	}
	return s.repository.SearchOwned(ctx, actor.UserID, SearchQuery{
		Query: normalizedQuery,
		Limit: limit,
	})
}

func (s *Service) ReadOwned(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
) (Goal, error) {
	if !actor.Valid() || !validUUID(goalID) {
		return Goal{}, ErrNotFound
	}
	return s.repository.FindOwned(ctx, actor.UserID, goalID)
}

func (s *Service) ChangeStatus(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
	expectedVersion int64,
	status Status,
) (Goal, error) {
	if !actor.Valid() || !validUUID(goalID) || expectedVersion < 1 ||
		!validStatus(status) {
		return Goal{}, ErrInvalidRequest
	}
	current, err := s.repository.FindOwned(ctx, actor.UserID, goalID)
	if err != nil {
		return Goal{}, err
	}
	if current.Version != expectedVersion {
		return Goal{}, ErrConflict
	}
	if current.Status == status {
		return current, nil
	}
	if !canTransition(current.Status, status) {
		return Goal{}, ErrConflict
	}
	return s.repository.UpdateStatus(
		ctx,
		actor.UserID,
		goalID,
		expectedVersion,
		status,
	)
}

func normalizeTitle(title string) (string, bool) {
	if !utf8.ValidString(title) {
		return "", false
	}
	title = strings.TrimSpace(title)
	if title == "" ||
		strings.ContainsRune(title, '\x00') ||
		utf8.RuneCountInString(title) > maxGoalTitleRunes ||
		len(title) > maxGoalTitleBytes {
		return "", false
	}
	return title, true
}

func normalizeSearchQuery(query string) (string, bool) {
	if !utf8.ValidString(query) {
		return "", false
	}
	query = strings.TrimSpace(query)
	if query == "" ||
		strings.ContainsRune(query, '\x00') ||
		utf8.RuneCountInString(query) > maxGoalSearchQueryRunes ||
		len(query) > maxGoalSearchQueryBytes {
		return "", false
	}
	return query, true
}

func validStatus(status Status) bool {
	return status == StatusActive ||
		status == StatusCompleted ||
		status == StatusArchived
}

func canTransition(from, to Status) bool {
	switch from {
	case StatusActive:
		return to == StatusCompleted || to == StatusArchived
	case StatusCompleted:
		return to == StatusActive || to == StatusArchived
	case StatusArchived:
		return to == StatusActive
	default:
		return false
	}
}
