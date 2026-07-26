package agent

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxMessageContentRunes = 4096
	maxMessageContentBytes = 16384
)

type Service struct {
	repository Repository
	matters    matter.Reader
}

func NewService(
	repository Repository,
	matters matter.Reader,
) (*Service, error) {
	if repository == nil || matters == nil {
		return nil, errors.New("agent: service dependency is required")
	}
	return &Service{repository: repository, matters: matters}, nil
}

func (s *Service) CreateThread(
	ctx context.Context,
	actor requestcontext.Actor,
	activeMatterID string,
) (Thread, error) {
	if !actor.Valid() {
		return Thread{}, ErrInvalidRequest
	}
	if activeMatterID != "" {
		if err := s.requireActiveMatter(ctx, actor, activeMatterID); err != nil {
			return Thread{}, err
		}
	}
	return s.repository.CreateThread(ctx, actor.UserID, activeMatterID)
}

func (s *Service) ListThreads(
	ctx context.Context,
	actor requestcontext.Actor,
) ([]Thread, error) {
	if !actor.Valid() {
		return nil, ErrInvalidRequest
	}
	return s.repository.ListThreads(ctx, actor.UserID)
}

func (s *Service) GetThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (Thread, error) {
	if !actor.Valid() || !validUUID(threadID) {
		return Thread{}, ErrNotFound
	}
	return s.repository.FindThread(ctx, actor.UserID, threadID)
}

func (s *Service) SetActiveMatter(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
) (ThreadMatterLink, error) {
	if !actor.Valid() || !validUUID(threadID) {
		return ThreadMatterLink{}, ErrNotFound
	}
	if err := s.requireActiveMatter(ctx, actor, matterID); err != nil {
		return ThreadMatterLink{}, err
	}
	return s.repository.SetActiveMatter(
		ctx,
		actor.UserID,
		threadID,
		matterID,
	)
}

func (s *Service) AppendUserMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
) (Message, error) {
	if !actor.Valid() || !validUUID(threadID) {
		return Message{}, ErrNotFound
	}
	if !clientMessageIDPattern.MatchString(clientMessageID) ||
		!validMessageContent(content) {
		return Message{}, ErrInvalidRequest
	}
	return s.repository.AppendUserMessage(
		ctx,
		actor.UserID,
		threadID,
		clientMessageID,
		content,
	)
}

func (s *Service) ListMessages(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) ([]Message, error) {
	if !actor.Valid() || !validUUID(threadID) {
		return nil, ErrNotFound
	}
	if _, err := s.repository.FindThread(
		ctx,
		actor.UserID,
		threadID,
	); err != nil {
		return nil, err
	}
	return s.repository.ListMessages(ctx, actor.UserID, threadID)
}

func (s *Service) requireActiveMatter(
	ctx context.Context,
	actor requestcontext.Actor,
	matterID string,
) error {
	if !validUUID(matterID) {
		return ErrNotFound
	}
	item, err := s.matters.ReadOwned(ctx, actor, matterID)
	if errors.Is(err, matter.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return ErrRepository
	}
	if item.Status != matter.StatusActive {
		return ErrConflict
	}
	return nil
}

func validMessageContent(value string) bool {
	return utf8.ValidString(value) &&
		len(value) >= 1 &&
		utf8.RuneCountInString(value) <= maxMessageContentRunes &&
		len(value) <= maxMessageContentBytes &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) != ""
}
