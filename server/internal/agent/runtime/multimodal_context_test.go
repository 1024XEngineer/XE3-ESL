package runtime

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/image"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestContextAssemblerAddsSignedImagesToMultimodalUserMessage(
	t *testing.T,
) {
	t.Parallel()

	const (
		ownerID   = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
		runID     = "40000000-0000-4000-8000-000000000001"
	)
	message := Message{
		ID:       messageID,
		OwnerID:  ownerID,
		ThreadID: threadID,
		Sequence: 1,
		Role:     MessageRoleUser,
		Modality: core.MessageModalityMultimodal,
		Content:  "What should I improve?",
	}
	repository := multimodalContextRepository{
		thread:  Thread{ID: threadID, OwnerID: ownerID},
		message: message,
	}
	assembler, err := NewContextAssembler(
		repository,
		multimodalContextMatters{},
		multimodalContextStableProfile{},
		multimodalContextMemories{},
		WithImageContextReader(multimodalContextImages{}),
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	configuration := RunConfiguration{
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
		Run{
			ID:                 runID,
			OwnerID:            ownerID,
			ThreadID:           threadID,
			InputMessageID:     messageID,
			Status:             RunStatusPending,
			RequestedProvider:  configuration.Provider,
			RequestedModel:     configuration.Model,
			MaxOutputTokens:    configuration.MaxOutputTokens,
			MaxInputCharacters: configuration.MaxInputCharacters,
		},
		configuration,
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

func TestContextAssemblerImageBudgetKeepsNewestImages(t *testing.T) {
	t.Parallel()

	const (
		ownerID  = "10000000-0000-4000-8000-000000000001"
		threadID = "20000000-0000-4000-8000-000000000001"
		runID    = "40000000-0000-4000-8000-000000000001"
	)
	messages := []Message{
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
		thread:   Thread{ID: threadID, OwnerID: ownerID},
		messages: messages,
	}
	assembler, err := NewContextAssembler(
		repository,
		multimodalContextMatters{},
		multimodalContextStableProfile{},
		multimodalContextMemories{},
		WithImageContextReader(multimodalBudgetImages{}),
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	configuration := RunConfiguration{
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
		Run{
			ID:                 runID,
			OwnerID:            ownerID,
			ThreadID:           threadID,
			InputMessageID:     messages[2].ID,
			Status:             RunStatusPending,
			RequestedProvider:  configuration.Provider,
			RequestedModel:     configuration.Model,
			MaxOutputTokens:    configuration.MaxOutputTokens,
			MaxInputCharacters: configuration.MaxInputCharacters,
		},
		configuration,
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
) Message {
	return Message{
		ID:       id,
		OwnerID:  ownerID,
		ThreadID: threadID,
		Sequence: sequence,
		Role:     MessageRoleUser,
		Modality: core.MessageModalityMultimodal,
		Content:  "Message with images.",
	}
}

type multimodalContextRepository struct {
	thread  Thread
	message Message
}

func (repository multimodalContextRepository) FindThread(
	context.Context,
	string,
	string,
) (Thread, error) {
	return repository.thread, nil
}

func (multimodalContextRepository) FindLatestSummaryCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (core.ThreadSummaryCheckpoint, error) {
	return core.ThreadSummaryCheckpoint{}, ErrNotFound
}

func (repository multimodalContextRepository) ListMessagesForContext(
	context.Context,
	string,
	string,
	int64,
	int64,
	int,
) ([]Message, int, error) {
	return []Message{repository.message}, 0, nil
}

func (repository multimodalContextRepository) FindMessage(
	context.Context,
	string,
	string,
	string,
) (Message, error) {
	return repository.message, nil
}

type multimodalContextMatters struct{}

func (multimodalContextMatters) ReadOwned(
	context.Context,
	requestcontext.Actor,
	string,
) (matter.Matter, error) {
	return matter.Matter{}, matter.ErrNotFound
}

type multimodalContextStableProfile struct{}

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
	thread   Thread
	messages []Message
}

func (repository multimodalBudgetRepository) FindThread(
	context.Context,
	string,
	string,
) (Thread, error) {
	return repository.thread, nil
}

func (multimodalBudgetRepository) FindLatestSummaryCheckpoint(
	context.Context,
	string,
	string,
	int64,
) (core.ThreadSummaryCheckpoint, error) {
	return core.ThreadSummaryCheckpoint{}, ErrNotFound
}

func (repository multimodalBudgetRepository) ListMessagesForContext(
	context.Context,
	string,
	string,
	int64,
	int64,
	int,
) ([]Message, int, error) {
	return append([]Message(nil), repository.messages...), 0, nil
}

func (repository multimodalBudgetRepository) FindMessage(
	context.Context,
	string,
	string,
	string,
) (Message, error) {
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
