package translation

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

const maxTranslationRunes = 8000

var (
	ErrInvalidRequest = errors.New("agent message translation: invalid request")
	ErrNotFound       = errors.New("agent message translation: not found")
	ErrInvalidContext = errors.New("agent message translation: invalid context")
)

type MessageReader interface {
	FindOwnedMessage(context.Context, string, string) (conversation.Message, error)
}

type Result struct {
	MessageID      string
	TargetLanguage string
	Content        string
}

type Service struct {
	messages   MessageReader
	translator sharedtranslation.Translator
}

func NewService(
	messages MessageReader,
	translator sharedtranslation.Translator,
) (*Service, error) {
	if messages == nil || translator == nil {
		return nil, errors.New("agent message translation: dependencies are required")
	}
	return &Service{messages: messages, translator: translator}, nil
}

func (service *Service) Translate(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
) (Result, error) {
	if ctx == nil || !actor.Valid() || !conversation.ValidUUID(messageID) {
		return Result{}, ErrInvalidRequest
	}
	message, err := service.messages.FindOwnedMessage(ctx, actor.UserID, messageID)
	if err != nil {
		if errors.Is(err, conversation.ErrNotFound) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	text := strings.TrimSpace(message.Content)
	if message.ID != messageID || message.OwnerID != actor.UserID ||
		message.Role != conversation.MessageRoleAssistant ||
		!conversation.ValidUUID(message.ProducedByRunID) ||
		!conversation.ValidMessageContent(text) {
		return Result{}, ErrInvalidContext
	}
	content, err := service.translator.Translate(
		ctx,
		sharedtranslation.Request{Text: text},
	)
	if err != nil {
		return Result{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" || !utf8.ValidString(content) ||
		utf8.RuneCountInString(content) > maxTranslationRunes {
		return Result{}, ErrInvalidContext
	}
	return Result{
		MessageID:      message.ID,
		TargetLanguage: sharedtranslation.TargetLanguage,
		Content:        content,
	}, nil
}
