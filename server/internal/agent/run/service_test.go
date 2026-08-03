package run

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSubmitWithImagesRequiresImageInputCapability(t *testing.T) {
	service := &Service{}
	_, err := service.SubmitWithImages(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		"30000000-0000-4000-8000-000000000001",
		"image-message-1",
		"Please review this image.",
		[]string{"40000000-0000-4000-8000-000000000001"},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("SubmitWithImages error = %v, want %v", err, ErrInvalidRequest)
	}
}

func TestRetryTextStreamReadsInputFromConversation(t *testing.T) {
	t.Parallel()

	actor := requestcontext.Actor{
		UserID:    "20000000-0000-4000-8000-000000000001",
		SessionID: "60000000-0000-4000-8000-000000000001",
	}
	retry := Retry{
		Run: Run{
			ID:             "10000000-0000-4000-8000-000000000002",
			OwnerID:        actor.UserID,
			ThreadID:       "30000000-0000-4000-8000-000000000001",
			InputMessageID: "40000000-0000-4000-8000-000000000001",
			Status:         StatusCompleted,
		},
		Created: false,
	}
	messages := &recordingMessageReader{
		message: conversation.Message{
			ID:      retry.Run.InputMessageID,
			Content: "I am a Java backend engineer.",
		},
	}
	observer := &recordingStreamObserver{}
	service := &Service{
		repository: completedRetryRepository{
			loopRepository: loopRepository{},
			retry:          retry,
		},
		messages: messages,
	}

	result, err := service.RetryTextStream(
		context.Background(),
		actor,
		"10000000-0000-4000-8000-000000000001",
		"retry-client-message-1",
		observer,
	)
	if err != nil {
		t.Fatalf("RetryTextStream: %v", err)
	}
	if result != retry {
		t.Fatalf("RetryTextStream result = %#v, want %#v", result, retry)
	}
	if messages.ownerID != actor.UserID ||
		messages.threadID != retry.Run.ThreadID ||
		messages.messageID != retry.Run.InputMessageID {
		t.Fatalf(
			"message read = owner %q thread %q message %q",
			messages.ownerID,
			messages.threadID,
			messages.messageID,
		)
	}
	if observer.submission.Run != retry.Run ||
		observer.submission.UserMessage != messages.message ||
		observer.submission.Created != retry.Created {
		t.Fatalf("committed submission = %#v", observer.submission)
	}
}

func TestRunConfigurationMatchesPersistedProviderModelAndBudget(t *testing.T) {
	configuration := Configuration{
		Provider:           "qianwen",
		Model:              "qwen-test",
		MaxOutputTokens:    512,
		MaxInputCharacters: 12000,
	}
	run := Run{
		RequestedProvider:  configuration.Provider,
		RequestedModel:     configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
		MaxInputCharacters: configuration.MaxInputCharacters,
	}
	tests := map[string]struct {
		configuration Configuration
		want          bool
	}{
		"matches": {
			configuration: configuration,
			want:          true,
		},
		"provider drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.Provider = "qianwen_next"
				return changed
			}(),
			want: false,
		},
		"model drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.Model = "qwen-next"
				return changed
			}(),
			want: false,
		},
		"output budget drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.MaxOutputTokens++
				return changed
			}(),
			want: false,
		},
		"context budget drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.MaxInputCharacters++
				return changed
			}(),
			want: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := runConfigurationMatches(
				run,
				test.configuration,
			); got != test.want {
				t.Fatalf(
					"runConfigurationMatches() = %v, want %v",
					got,
					test.want,
				)
			}
		})
	}
}

type completedRetryRepository struct {
	loopRepository
	retry Retry
}

func (repository completedRetryRepository) CreateRetry(
	context.Context,
	string,
	string,
	string,
	Configuration,
) (Retry, error) {
	return repository.retry, nil
}

type recordingMessageReader struct {
	message   conversation.Message
	ownerID   string
	threadID  string
	messageID string
}

func (reader *recordingMessageReader) FindMessage(
	_ context.Context,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	reader.ownerID = ownerID
	reader.threadID = threadID
	reader.messageID = messageID
	return reader.message, nil
}

type recordingStreamObserver struct {
	submission Submission
}

func (observer *recordingStreamObserver) OnInputCommitted(
	_ context.Context,
	submission Submission,
) error {
	observer.submission = submission
	return nil
}

func (*recordingStreamObserver) OnAssistantStarted(context.Context, Run) error {
	return nil
}

func (*recordingStreamObserver) OnAssistantDelta(context.Context, string) error {
	return nil
}
