package app

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxAgentPageSize = core.MaxAgentPageSize

type Thread = core.Thread
type ThreadMatterLink = core.ThreadMatterLink
type Message = core.Message
type ThreadPageCursor = core.ThreadPageCursor
type MessagePageCursor = core.MessagePageCursor
type ThreadPage = core.ThreadPage
type MessagePage = core.MessagePage
type Repository = core.Repository

var (
	ErrInvalidRequest = core.ErrInvalidRequest
	ErrNotFound       = core.ErrNotFound
	ErrConflict       = core.ErrConflict
	ErrRepository     = core.ErrRepository
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

func (s *Service) PageThreads(
	ctx context.Context,
	actor requestcontext.Actor,
	pageSize int,
	rawCursor string,
) (ThreadPage, error) {
	if !actor.Valid() || pageSize < 1 || pageSize > maxAgentPageSize {
		return ThreadPage{}, ErrInvalidRequest
	}
	var before *ThreadPageCursor
	if rawCursor != "" {
		decoded, err := core.DecodeThreadPageCursor(rawCursor)
		if err != nil {
			return ThreadPage{}, err
		}
		before = &decoded
	}
	threads, err := s.repository.PageThreads(
		ctx,
		actor.UserID,
		pageSize+1,
		before,
	)
	if err != nil {
		return ThreadPage{}, err
	}
	focused, found, err := s.repository.FindFocusedThread(ctx, actor.UserID)
	if err != nil {
		return ThreadPage{}, err
	}
	result := ThreadPage{Threads: threads}
	if found {
		result.FocusedThreadID = focused.ID
	}
	if len(result.Threads) > pageSize {
		result.Threads = result.Threads[:pageSize]
		last := result.Threads[len(result.Threads)-1]
		result.NextCursor, err = core.EncodeThreadPageCursor(ThreadPageCursor{
			UpdatedAt: last.UpdatedAt,
			ThreadID:  last.ID,
		})
		if err != nil {
			return ThreadPage{}, err
		}
	}
	return result, nil
}

func (s *Service) GetThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (Thread, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return Thread{}, ErrNotFound
	}
	return s.repository.FindThread(ctx, actor.UserID, threadID)
}

func (s *Service) DeleteThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) error {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return ErrNotFound
	}
	repository, ok := s.repository.(core.ThreadDeletionRepository)
	if !ok {
		return ErrRepository
	}
	return repository.DeleteThread(ctx, actor.UserID, threadID)
}

func (s *Service) GetFocusedThread(
	ctx context.Context,
	actor requestcontext.Actor,
) (Thread, bool, error) {
	if !actor.Valid() {
		return Thread{}, false, ErrInvalidRequest
	}
	return s.repository.FindFocusedThread(ctx, actor.UserID)
}

func (s *Service) SetFocusedThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (Thread, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return Thread{}, ErrNotFound
	}
	return s.repository.SetFocusedThread(ctx, actor.UserID, threadID)
}

func (s *Service) ClearFocusedThread(
	ctx context.Context,
	actor requestcontext.Actor,
) error {
	if !actor.Valid() {
		return ErrInvalidRequest
	}
	return s.repository.ClearFocusedThread(ctx, actor.UserID)
}

func (s *Service) SetActiveMatter(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
) (ThreadMatterLink, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
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
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return Message{}, ErrNotFound
	}
	if !core.ValidClientMessageID(clientMessageID) ||
		!core.ValidMessageContent(content) {
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
	if !actor.Valid() || !core.ValidUUID(threadID) {
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

func (s *Service) PageMessages(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	pageSize int,
	rawCursor string,
) (MessagePage, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return MessagePage{}, ErrNotFound
	}
	if pageSize < 1 || pageSize > maxAgentPageSize {
		return MessagePage{}, ErrInvalidRequest
	}
	if _, err := s.repository.FindThread(
		ctx,
		actor.UserID,
		threadID,
	); err != nil {
		return MessagePage{}, err
	}
	var before *MessagePageCursor
	if rawCursor != "" {
		decoded, err := core.DecodeMessagePageCursor(rawCursor, threadID)
		if err != nil {
			return MessagePage{}, err
		}
		before = &decoded
	}
	messages, err := s.repository.PageMessages(
		ctx,
		actor.UserID,
		threadID,
		pageSize+1,
		before,
	)
	if err != nil {
		return MessagePage{}, err
	}
	result := MessagePage{Messages: messages}
	if len(result.Messages) > pageSize {
		result.Messages = result.Messages[:pageSize]
		oldest := result.Messages[len(result.Messages)-1]
		result.NextCursor, err = core.EncodeMessagePageCursor(MessagePageCursor{
			ThreadID:       threadID,
			BeforeSequence: oldest.Sequence,
		})
		if err != nil {
			return MessagePage{}, err
		}
	}
	reverseMessages(result.Messages)
	return result, nil
}

func (s *Service) requireActiveMatter(
	ctx context.Context,
	actor requestcontext.Actor,
	matterID string,
) error {
	if !core.ValidUUID(matterID) {
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

func reverseMessages(messages []Message) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}
