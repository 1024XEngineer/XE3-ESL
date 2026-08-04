package context

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAssemblerAddsSignedImagesToMultimodalUserMessage(
	t *testing.T,
) {
	t.Parallel()

	const (
		ownerID   = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
		runID     = "40000000-0000-4000-8000-000000000001"
	)
	message := conversation.Message{
		ID:       messageID,
		OwnerID:  ownerID,
		ThreadID: threadID,
		Sequence: 1,
		Role:     conversation.MessageRoleUser,
		Modality: conversation.MessageModalityMultimodal,
		Content:  "What should I improve?",
	}
	repository := multimodalRepository{
		thread:  conversation.Thread{ID: threadID, OwnerID: ownerID},
		message: message,
	}
	assembler, err := NewAssembler(
		repository,
		multimodalContextGoals{},
		multimodalContextLearningProfile{},
		multimodalContextStableProfile{},
		multimodalContextMemories{},
		multimodalContextMemoryBarrier{},
		WithImageReader(multimodalContextImages{}),
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	command := AssembleCommand{
		RunID:              runID,
		OwnerID:            ownerID,
		ThreadID:           threadID,
		InputMessageID:     messageID,
		RunCreatedAt:       time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC),
		Provider:           "fake",
		Model:              "fake-multimodal",
		MaxOutputTokens:    256,
		MaxInputCharacters: 12000,
	}
	_, request, err := assembler.Assemble(
		context.Background(),
		requestcontext.Actor{
			UserID:    ownerID,
			SessionID: "multimodal-session",
		},
		command,
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	user := request.Messages[1]
	if user.Content != "" || len(user.ContentParts) != 2 ||
		user.ContentParts[0].Kind != ai.ContentPartText ||
		user.ContentParts[0].Text != message.Content ||
		user.ContentParts[1].Kind != ai.ContentPartImageURL ||
		user.ContentParts[1].ImageURL !=
			"https://objects.invalid/image?signature=ephemeral" {
		t.Fatalf("user message = %#v", user)
	}
}

func TestAssemblerImageBudgetKeepsNewestImages(t *testing.T) {
	t.Parallel()

	const (
		ownerID  = "10000000-0000-4000-8000-000000000001"
		threadID = "20000000-0000-4000-8000-000000000001"
		runID    = "40000000-0000-4000-8000-000000000001"
	)
	messages := []conversation.Message{
		multimodalContextMessage(
			"30000000-0000-4000-8000-000000000001",
			ownerID,
			threadID,
			1,
		),
		multimodalContextMessage(
			"30000000-0000-4000-8000-000000000002",
			ownerID,
			threadID,
			2,
		),
		multimodalContextMessage(
			"30000000-0000-4000-8000-000000000003",
			ownerID,
			threadID,
			3,
		),
	}
	repository := multimodalBudgetRepository{
		thread:   conversation.Thread{ID: threadID, OwnerID: ownerID},
		messages: messages,
	}
	assembler, err := NewAssembler(
		repository,
		multimodalContextGoals{},
		multimodalContextLearningProfile{},
		multimodalContextStableProfile{},
		multimodalContextMemories{},
		multimodalContextMemoryBarrier{},
		WithImageReader(multimodalBudgetImages{}),
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	command := AssembleCommand{
		RunID:              runID,
		OwnerID:            ownerID,
		ThreadID:           threadID,
		InputMessageID:     messages[2].ID,
		RunCreatedAt:       time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC),
		Provider:           "fake",
		Model:              "fake-multimodal",
		MaxOutputTokens:    256,
		MaxInputCharacters: 12000,
	}
	_, request, err := assembler.Assemble(
		context.Background(),
		requestcontext.Actor{
			UserID:    ownerID,
			SessionID: "multimodal-session",
		},
		command,
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(request.Messages) != 4 ||
		len(request.Messages[1].ContentParts) != 0 ||
		len(request.Messages[2].ContentParts) != 5 ||
		len(request.Messages[3].ContentParts) != 5 {
		t.Fatalf("budgeted messages = %#v", request.Messages)
	}
}

func multimodalContextMessage(
	id string,
	ownerID string,
	threadID string,
	sequence int64,
) conversation.Message {
	return conversation.Message{
		ID:       id,
		OwnerID:  ownerID,
		ThreadID: threadID,
		Sequence: sequence,
		Role:     conversation.MessageRoleUser,
		Modality: conversation.MessageModalityMultimodal,
		Content:  "Message with images.",
	}
}

type multimodalRepository struct {
	thread  conversation.Thread
	message conversation.Message
}

func (repository multimodalRepository) FindThread(
	context.Context,
	string,
	string,
) (conversation.Thread, error) {
	return repository.thread, nil
}

func (multimodalRepository) FindLatestCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (summary.Checkpoint, error) {
	return summary.Checkpoint{}, conversation.ErrNotFound
}

func (repository multimodalRepository) ListMessagesForContext(
	context.Context,
	string,
	string,
	int64,
	int64,
	int,
) ([]conversation.Message, int, error) {
	return []conversation.Message{repository.message}, 0, nil
}

func (repository multimodalRepository) FindMessage(
	context.Context,
	string,
	string,
	string,
) (conversation.Message, error) {
	return repository.message, nil
}

type multimodalContextGoals struct{}

func (multimodalContextGoals) ReadOwned(
	context.Context,
	requestcontext.Actor,
	string,
) (goal.Goal, error) {
	return goal.Goal{}, goal.ErrNotFound
}

type multimodalContextStableProfile struct{}

type multimodalContextLearningProfile struct{}

func (multimodalContextLearningProfile) ReadLearningProfile(
	context.Context,
	LearningProfileReadRequest,
) ([]LearningProfileDimension, error) {
	return []LearningProfileDimension{}, nil
}

func (multimodalContextStableProfile) ReadStableProfile(
	context.Context,
	StableProfileReadRequest,
) ([]StableProfileMemory, error) {
	return nil, nil
}

type multimodalContextMemories struct{}

func (multimodalContextMemories) Search(
	context.Context,
	MemorySearchRequest,
) ([]MemorySearchHit, error) {
	return nil, nil
}

type multimodalContextMemoryBarrier struct{}

func (multimodalContextMemoryBarrier) Await(
	_ context.Context,
	request MemoryExtractionBarrierRequest,
) (MemoryExtractionBarrierResult, error) {
	return MemoryExtractionBarrierResult{
		PolicyVersion: MemoryExtractionBarrierPolicyV1,
		Cutoff:        request.Cutoff,
		Status:        MemoryExtractionBarrierReady,
	}, nil
}

type multimodalContextImages struct{}

func (multimodalContextImages) MessageImages(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) ([]agentimage.ContextImage, error) {
	return []agentimage.ContextImage{{
		AssetID: "50000000-0000-4000-8000-000000000001",
		URL:     "https://objects.invalid/image?signature=ephemeral",
	}}, nil
}

type multimodalBudgetRepository struct {
	thread   conversation.Thread
	messages []conversation.Message
}

func (repository multimodalBudgetRepository) FindThread(
	context.Context,
	string,
	string,
) (conversation.Thread, error) {
	return repository.thread, nil
}

func (multimodalBudgetRepository) FindLatestCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (summary.Checkpoint, error) {
	return summary.Checkpoint{}, conversation.ErrNotFound
}

func (repository multimodalBudgetRepository) ListMessagesForContext(
	context.Context,
	string,
	string,
	int64,
	int64,
	int,
) ([]conversation.Message, int, error) {
	return append([]conversation.Message(nil), repository.messages...), 0, nil
}

func (repository multimodalBudgetRepository) FindMessage(
	context.Context,
	string,
	string,
	string,
) (conversation.Message, error) {
	return repository.messages[len(repository.messages)-1], nil
}

type multimodalBudgetImages struct{}

func (multimodalBudgetImages) MessageImages(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	messageID string,
) ([]agentimage.ContextImage, error) {
	result := make([]agentimage.ContextImage, 0, 4)
	for index := 0; index < 4; index++ {
		result = append(result, agentimage.ContextImage{
			AssetID: messageID,
			URL: "https://objects.invalid/" +
				messageID +
				"?signature=ephemeral",
		})
	}
	return result, nil
}
