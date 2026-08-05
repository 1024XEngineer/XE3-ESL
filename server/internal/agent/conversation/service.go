package conversation

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Service struct {
	repository Repository
	goals      GoalReader
}

func NewService(
	repository Repository,
	goals GoalReader,
) (*Service, error) {
	if repository == nil || goals == nil {
		return nil, errors.New("agent: service dependency is required")
	}
	return &Service{repository: repository, goals: goals}, nil
}

func (s *Service) CreateThread(
	ctx context.Context,
	actor requestcontext.Actor,
	activeGoalID string,
) (Thread, error) {
	if !actor.Valid() {
		return Thread{}, ErrInvalidRequest
	}
	if activeGoalID != "" {
		if err := s.requireActiveGoal(ctx, actor, activeGoalID); err != nil {
			return Thread{}, err
		}
	}
	return s.repository.CreateThread(ctx, actor.UserID, activeGoalID)
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
	if !actor.Valid() || pageSize < 1 || pageSize > MaxPageSize {
		return ThreadPage{}, ErrInvalidRequest
	}
	var before *ThreadPageCursor
	if rawCursor != "" {
		decoded, err := DecodeThreadPageCursor(rawCursor)
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
		result.NextCursor, err = EncodeThreadPageCursor(ThreadPageCursor{
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
	if !actor.Valid() || !ValidUUID(threadID) {
		return Thread{}, ErrNotFound
	}
	return s.repository.FindThread(ctx, actor.UserID, threadID)
}

func (s *Service) DeleteThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) error {
	if !actor.Valid() || !ValidUUID(threadID) {
		return ErrNotFound
	}
	return s.repository.DeleteThread(ctx, actor.UserID, threadID)
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
	if !actor.Valid() || !ValidUUID(threadID) {
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

func (s *Service) SetActiveGoal(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	goalID string,
) (ThreadGoalLink, error) {
	if !actor.Valid() || !ValidUUID(threadID) {
		return ThreadGoalLink{}, ErrNotFound
	}
	if err := s.requireActiveGoal(ctx, actor, goalID); err != nil {
		return ThreadGoalLink{}, err
	}
	return s.repository.SetActiveGoal(
		ctx,
		actor.UserID,
		threadID,
		goalID,
	)
}

func (s *Service) AppendUserMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
) (Message, error) {
	if !actor.Valid() || !ValidUUID(threadID) {
		return Message{}, ErrNotFound
	}
	if !ValidClientMessageID(clientMessageID) ||
		!ValidMessageContent(content) {
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
	if !actor.Valid() || !ValidUUID(threadID) {
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
	if !actor.Valid() || !ValidUUID(threadID) {
		return MessagePage{}, ErrNotFound
	}
	if pageSize < 1 || pageSize > MaxPageSize {
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
		decoded, err := DecodeMessagePageCursor(rawCursor, threadID)
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
		result.NextCursor, err = EncodeMessagePageCursor(MessagePageCursor{
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

func (s *Service) requireActiveGoal(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
) error {
	if !ValidUUID(goalID) {
		return ErrNotFound
	}
	item, found, err := s.goals.ReadGoalState(ctx, actor, goalID)
	if err != nil {
		return ErrRepository
	}
	if !found || item.ID != goalID {
		return ErrNotFound
	}
	if !item.Active {
		return ErrConflict
	}
	return nil
}

func reverseMessages(messages []Message) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}
