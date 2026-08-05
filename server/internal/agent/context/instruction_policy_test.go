package context

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentinstruction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/instruction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAssemblerBudgetsRenderedInstructionAndRecordsItsVersion(
	t *testing.T,
) {
	t.Parallel()

	const (
		ownerID   = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
		runID     = "40000000-0000-4000-8000-000000000001"
		budget    = 5000
	)
	rendered := agentinstruction.Render(agentinstruction.Projection{})
	messageCharacters := budget - utf8.RuneCountInString(rendered.Content)
	if messageCharacters < 1 {
		t.Fatalf("instruction exceeds test budget: %d", messageCharacters)
	}
	message := conversation.Message{
		ID:       messageID,
		OwnerID:  ownerID,
		ThreadID: threadID,
		Sequence: 1,
		Role:     conversation.MessageRoleUser,
		Modality: conversation.MessageModalityText,
		Content:  strings.Repeat("a", messageCharacters),
	}
	assembler := newInstructionBudgetAssembler(t, message)
	command := AssembleCommand{
		RunID:              runID,
		OwnerID:            ownerID,
		ThreadID:           threadID,
		InputMessageID:     messageID,
		RunCreatedAt:       time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC),
		Provider:           "fake",
		Model:              "fake-model",
		MaxOutputTokens:    256,
		MaxInputCharacters: budget,
	}
	actor := requestcontext.Actor{
		UserID:    ownerID,
		SessionID: "instruction-policy-session",
	}
	manifest, input, err := assembler.Assemble(
		context.Background(),
		actor,
		command,
	)
	if err != nil {
		t.Fatalf("assemble exact budget: %v", err)
	}
	if manifest.InstructionVersion != rendered.Version ||
		manifest.UsedInputCharacters != budget ||
		len(input.Messages) != 2 ||
		input.Messages[0].Content != rendered.Content {
		t.Fatalf("manifest = %#v, input = %#v", manifest, input)
	}

	message.Content += "a"
	assembler = newInstructionBudgetAssembler(t, message)
	_, _, err = assembler.Assemble(context.Background(), actor, command)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("assemble over budget error = %v", err)
	}
}

func newInstructionBudgetAssembler(
	t *testing.T,
	message conversation.Message,
) *Assembler {
	t.Helper()

	assembler, err := NewAssembler(
		multimodalRepository{
			thread: conversation.Thread{
				ID:      message.ThreadID,
				OwnerID: message.OwnerID,
			},
			message: message,
		},
		multimodalContextGoals{},
		multimodalContextLearningProfile{},
		multimodalContextStableProfile{},
		multimodalContextMemories{},
		multimodalContextMemoryBarrier{},
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	return assembler
}
