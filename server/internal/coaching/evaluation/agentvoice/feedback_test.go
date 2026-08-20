package agentvoice

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestFeedbackSchedulesOnlyVoiceMessages(t *testing.T) {
	t.Parallel()
	const (
		userID    = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
	)
	tests := []struct {
		name          string
		modality      conversation.MessageModality
		wantScheduled int
		wantStatusURL string
	}{
		{
			name:          "voice ASR message",
			modality:      conversation.MessageModalityVoice,
			wantScheduled: 1,
			wantStatusURL: "/v1/agent-messages/" + messageID + "/evaluation",
		},
		{
			name:     "keyboard text message",
			modality: conversation.MessageModalityText,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queue := &feedbackQueueStub{}
			scheduler, err := evaluation.NewAgentMessageFeedbackScheduler(
				queue,
				evaluation.ConfigLineage{
					SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
					StrategyRef:     "speech-feedback/v1",
					PipelineVersion: "speech-evaluation/v1",
					PromptVersion:   "speech-feedback/v1",
					ResultSchema:    "speech-feedback/v1",
					Provider:        "qianwen",
					Model:           "qwen-plus",
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			feedback, err := NewFeedback(scheduler, feedbackMessageReaderStub{
				message: conversation.Message{
					ID: messageID, OwnerID: userID, ThreadID: threadID,
					Role: conversation.MessageRoleUser, Modality: test.modality,
					Content:   "I called the landlord because it is leaking.",
					CreatedAt: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			reference, err := feedback.EnsureMessage(
				context.Background(),
				requestcontext.Actor{UserID: userID, SessionID: "session-1"},
				threadID,
				messageID,
			)
			if err != nil {
				t.Fatal(err)
			}
			if queue.calls != test.wantScheduled || reference.StatusURL != test.wantStatusURL {
				t.Fatalf("queue calls = %d, status URL = %q", queue.calls, reference.StatusURL)
			}
		})
	}
}

type feedbackQueueStub struct{ calls int }

func (queue *feedbackQueueStub) Queue(
	context.Context,
	evaluation.QueueCommand,
) (evaluation.Record, bool, error) {
	queue.calls++
	return evaluation.Record{}, true, nil
}

type feedbackMessageReaderStub struct{ message conversation.Message }

func (reader feedbackMessageReaderStub) FindMessageByID(
	context.Context,
	string,
	string,
) (conversation.Message, error) {
	return reader.message, nil
}
