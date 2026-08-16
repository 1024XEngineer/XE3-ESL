package agentvoice

import (
	"context"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	voice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type messageReader interface {
	FindMessageByID(context.Context, string, string) (conversation.Message, error)
}

type Feedback struct {
	scheduler *evaluation.AgentMessageFeedbackScheduler
	messages  messageReader
}

func NewFeedback(
	scheduler *evaluation.AgentMessageFeedbackScheduler,
	messages messageReader,
) (*Feedback, error) {
	if scheduler == nil || messages == nil {
		return nil, voice.ErrInvalidContext
	}
	return &Feedback{scheduler: scheduler, messages: messages}, nil
}

func (feedback *Feedback) EnsureMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) (voice.FeedbackReference, error) {
	statusURL, found, err := feedback.ensure(ctx, actor, threadID, messageID)
	if err != nil || !found {
		return voice.FeedbackReference{}, err
	}
	return voice.FeedbackReference{StatusURL: statusURL}, nil
}

func (feedback *Feedback) StatusURLForAgentVoiceMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	messageID string,
) (string, bool, error) {
	return feedback.ensure(ctx, actor, "", messageID)
}

func (feedback *Feedback) ensure(
	ctx context.Context,
	actor requestcontext.Actor,
	expectedThreadID string,
	messageID string,
) (string, bool, error) {
	if feedback == nil || feedback.scheduler == nil || feedback.messages == nil ||
		ctx == nil || !actor.Valid() {
		return "", false, voice.ErrInvalidContext
	}
	message, err := feedback.messages.FindMessageByID(ctx, actor.UserID, messageID)
	if err != nil {
		return "", false, err
	}
	if message.OwnerID != actor.UserID ||
		(expectedThreadID != "" && message.ThreadID != expectedThreadID) ||
		message.Role != conversation.MessageRoleUser ||
		message.Modality != conversation.MessageModalityVoice ||
		strings.TrimSpace(message.Content) == "" {
		return "", false, nil
	}
	_, _, err = feedback.scheduler.Schedule(ctx, evaluation.AgentMessageEvidence{
		UserID:      actor.UserID,
		ThreadID:    message.ThreadID,
		MessageID:   message.ID,
		Transcript:  message.Content,
		ConfirmedAt: message.CreatedAt,
	})
	if err != nil {
		return "", false, err
	}
	return "/v1/agent-messages/" + message.ID + "/evaluation", true, nil
}

var _ voice.FeedbackPort = (*Feedback)(nil)
