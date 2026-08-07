package translation

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

const (
	userID    = "20000000-0000-4000-8000-000000000001"
	messageID = "40000000-0000-4000-8000-000000000001"
	runID     = "10000000-0000-4000-8000-000000000001"
)

func TestServiceTranslatesOwnedCompletedAssistantMessage(t *testing.T) {
	t.Parallel()
	reader := &messageReaderStub{message: conversation.Message{
		ID: messageID, OwnerID: userID, Role: conversation.MessageRoleAssistant,
		ProducedByRunID: runID, Content: " Keep the response concise. ",
	}}
	translator := &translatorStub{content: " 保持回答简洁。 "}
	service, err := NewService(reader, translator)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := service.Translate(context.Background(), actor(), messageID)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if reader.ownerID != userID || reader.messageID != messageID ||
		translator.text != "Keep the response concise." {
		t.Fatalf("reader=%#v translator=%#v", reader, translator)
	}
	if result.MessageID != messageID || result.TargetLanguage != "zh-CN" ||
		result.Content != "保持回答简洁。" {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceRejectsMessagesThatAreNotCompletedAssistantOutput(t *testing.T) {
	t.Parallel()
	tests := map[string]conversation.Message{
		"user": {
			ID: messageID, OwnerID: userID, Role: conversation.MessageRoleUser,
			Content: "Hello", ProducedByRunID: runID,
		},
		"assistant without completed run": {
			ID: messageID, OwnerID: userID,
			Role: conversation.MessageRoleAssistant, Content: "Hello",
		},
	}
	for name, message := range tests {
		message := message
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			translator := &translatorStub{}
			service, _ := NewService(&messageReaderStub{message: message}, translator)
			_, err := service.Translate(context.Background(), actor(), messageID)
			if !errors.Is(err, ErrInvalidContext) || translator.calls != 0 {
				t.Fatalf("Translate error=%v calls=%d", err, translator.calls)
			}
		})
	}
}

func TestServiceDoesNotRevealAnotherUsersMessage(t *testing.T) {
	t.Parallel()
	service, _ := NewService(
		&messageReaderStub{err: conversation.ErrNotFound},
		&translatorStub{},
	)
	_, err := service.Translate(context.Background(), actor(), messageID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Translate error = %v", err)
	}
}

func actor() requestcontext.Actor {
	return requestcontext.Actor{UserID: userID, SessionID: "session"}
}

type messageReaderStub struct {
	message            conversation.Message
	err                error
	ownerID, messageID string
}

func (reader *messageReaderStub) FindOwnedMessage(
	_ context.Context,
	ownerID string,
	messageID string,
) (conversation.Message, error) {
	reader.ownerID, reader.messageID = ownerID, messageID
	return reader.message, reader.err
}

type translatorStub struct {
	content string
	err     error
	text    string
	calls   int
}

func (translator *translatorStub) Translate(
	_ context.Context,
	request sharedtranslation.Request,
) (string, error) {
	translator.calls++
	translator.text = request.Text
	return translator.content, translator.err
}
