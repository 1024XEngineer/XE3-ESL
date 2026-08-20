package context

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
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
	rendered := Instruction{
		Version: "test-product-v1",
		Content: "You are a test product assistant.",
	}
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
	assembler := newInstructionBudgetAssembler(t, rendered, message)
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
	assembler = newInstructionBudgetAssembler(t, rendered, message)
	_, _, err = assembler.Assemble(context.Background(), actor, command)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("assemble over budget error = %v", err)
	}
}

func newInstructionBudgetAssembler(
	t *testing.T,
	instruction Instruction,
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
		instruction,
		multimodalContextProfile{},
	)
	if err != nil {
		t.Fatalf("new assembler: %v", err)
	}
	return assembler
}

func TestAssemblerRecordsAndContinuesWhenCoachingProfileReadFails(t *testing.T) {
	const (
		ownerID   = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
		runID     = "40000000-0000-4000-8000-000000000001"
	)
	message := conversation.Message{
		ID: messageID, OwnerID: ownerID, ThreadID: threadID, Sequence: 1,
		Role: conversation.MessageRoleUser, Modality: conversation.MessageModalityText,
		Content: "Hello",
	}
	assembler, err := NewAssembler(
		multimodalRepository{
			thread:  conversation.Thread{ID: threadID, OwnerID: ownerID},
			message: message,
		},
		Instruction{Version: "test-v1", Content: "Test instruction."},
		failingCoachingProfileContributor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, input, err := assembler.Assemble(
		context.Background(),
		requestcontext.Actor{UserID: ownerID, SessionID: "session-1"},
		AssembleCommand{
			RunID: runID, OwnerID: ownerID, ThreadID: threadID,
			InputMessageID: messageID, RunCreatedAt: time.Now().UTC(),
			Provider: "test", Model: "test-model-v1",
			MaxOutputTokens: 256, MaxInputCharacters: 5000,
		},
	)
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if manifest.CoachingProfileContextStatus !=
		CoachingProfileContextUnavailableError ||
		manifest.CoachingProfileVersion != 0 ||
		len(input.Messages) != 2 || input.Messages[1].Content != "Hello" {
		t.Fatalf("manifest=%#v input=%#v", manifest, input)
	}
}

func TestAssemblerInjectsAuthoritativeCurrentTurnContext(t *testing.T) {
	const (
		ownerID   = "10000000-0000-4000-8000-000000000001"
		threadID  = "20000000-0000-4000-8000-000000000001"
		messageID = "30000000-0000-4000-8000-000000000001"
		runID     = "40000000-0000-4000-8000-000000000001"
	)
	message := conversation.Message{
		ID: messageID, OwnerID: ownerID, ThreadID: threadID, Sequence: 1,
		Role: conversation.MessageRoleUser, Modality: conversation.MessageModalityVoice,
		Content: "The air conditioner is leaking water.",
	}
	assembler, err := NewAssembler(
		multimodalRepository{
			thread:  conversation.Thread{ID: threadID, OwnerID: ownerID},
			message: message,
		},
		Instruction{Version: "test-v1", Content: "Test instruction."},
		multimodalContextProfile{},
		WithTurnContextContributor(staticTurnContextContributor{
			payload: `{"schema_version":"agent-turn-context/v1","speech_feedback":{"kinds":["STRENGTH"],"conclusion":"NO_CHANGE"},"practice":{"scene_name":"租房报修","user_role":"租客","ai_role":"物业工作人员"}}`,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, input, err := assembler.Assemble(
		context.Background(),
		requestcontext.Actor{UserID: ownerID, SessionID: "session"},
		AssembleCommand{
			RunID: runID, OwnerID: ownerID, ThreadID: threadID,
			InputMessageID: messageID, RunCreatedAt: time.Now().UTC(),
			Provider: "test", Model: "test-model-v1",
			MaxOutputTokens: 256, MaxInputCharacters: 5000,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Messages) != 2 ||
		!strings.Contains(input.Messages[0].Content, "authoritative current-turn") ||
		!strings.Contains(input.Messages[0].Content, `"conclusion":"NO_CHANGE"`) ||
		!strings.Contains(input.Messages[0].Content, `"ai_role":"物业工作人员"`) {
		t.Fatalf("system input = %q", input.Messages[0].Content)
	}
}

type failingCoachingProfileContributor struct{}

type staticTurnContextContributor struct{ payload string }

func (contributor staticTurnContextContributor) Contribute(
	context.Context,
	requestcontext.Actor,
	TurnContextRequest,
) (TurnContextContribution, error) {
	return TurnContextContribution{Payload: []byte(contributor.payload)}, nil
}

func (failingCoachingProfileContributor) Contribute(
	context.Context,
	requestcontext.Actor,
) (CoachingProfileContribution, error) {
	return CoachingProfileContribution{}, errors.New("profile repository unavailable")
}
