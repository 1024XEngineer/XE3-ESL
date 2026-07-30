package matter

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxMatterTitleRunes       = 200
	maxMatterTitleBytes       = 512
	maxMatterSearchQueryRunes = 500
	maxMatterSearchQueryBytes = 2000
	defaultMatterSearchLimit  = 10
	MaxMatterSearchLimit      = 20
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("matter: repository is required")
	}
	return &Service{repository: repository}, nil
}

func (s *Service) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	title string,
) (Matter, error) {
	if !actor.Valid() {
		return Matter{}, ErrInvalidRequest
	}
	normalizedTitle, ok := normalizeTitle(title)
	if !ok {
		return Matter{}, ErrInvalidRequest
	}
	return s.repository.Create(ctx, actor.UserID, normalizedTitle)
}

func (s *Service) CreateIdempotent(
	ctx context.Context,
	actor requestcontext.Actor,
	requestID string,
	title string,
) (Matter, error) {
	if !actor.Valid() || !requestIDPattern.MatchString(requestID) {
		return Matter{}, ErrInvalidRequest
	}
	normalizedTitle, ok := normalizeTitle(title)
	if !ok {
		return Matter{}, ErrInvalidRequest
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
) ([]Matter, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	return s.repository.ListOwned(ctx, actor.UserID)
}

func (s *Service) Search(
	ctx context.Context,
	actor requestcontext.Actor,
	query SearchQuery,
) ([]Matter, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	normalizedQuery, ok := normalizeSearchQuery(query.Query)
	if !ok {
		return nil, ErrInvalidRequest
	}
	limit := query.Limit
	if limit == 0 {
		limit = defaultMatterSearchLimit
	}
	if limit < 1 || limit > MaxMatterSearchLimit {
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
	matterID string,
) (Matter, error) {
	if !actor.Valid() || !validUUID(matterID) {
		return Matter{}, ErrNotFound
	}
	return s.repository.FindOwned(ctx, actor.UserID, matterID)
}

func (s *Service) ChangeStatus(
	ctx context.Context,
	actor requestcontext.Actor,
	matterID string,
	expectedVersion int64,
	status Status,
) (Matter, error) {
	if !actor.Valid() || !validUUID(matterID) || expectedVersion < 1 ||
		!validStatus(status) {
		return Matter{}, ErrInvalidRequest
	}
	current, err := s.repository.FindOwned(ctx, actor.UserID, matterID)
	if err != nil {
		return Matter{}, err
	}
	if current.Version != expectedVersion {
		return Matter{}, ErrConflict
	}
	if current.Status == status {
		return current, nil
	}
	if !canTransition(current.Status, status) {
		return Matter{}, ErrConflict
	}
	return s.repository.UpdateStatus(
		ctx,
		actor.UserID,
		matterID,
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
		utf8.RuneCountInString(title) > maxMatterTitleRunes ||
		len(title) > maxMatterTitleBytes {
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
		utf8.RuneCountInString(query) > maxMatterSearchQueryRunes ||
		len(query) > maxMatterSearchQueryBytes {
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
