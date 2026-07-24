package matter

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxMatterTitleRunes = 200
	maxMatterTitleBytes = 512
)

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

func (s *Service) List(
	ctx context.Context,
	actor requestcontext.Actor,
) ([]Matter, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	return s.repository.ListOwned(ctx, actor.UserID)
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
		utf8.RuneCountInString(title) > maxMatterTitleRunes ||
		len(title) > maxMatterTitleBytes {
		return "", false
	}
	return title, true
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
