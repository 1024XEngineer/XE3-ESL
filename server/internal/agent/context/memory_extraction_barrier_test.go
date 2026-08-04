package context

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAssemblerAuditsFirstMessageMemoryExtractionBarrier(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	coveredThrough := cutoff.Add(-time.Second)
	barrier := &recordingContextMemoryBarrier{result: MemoryExtractionBarrierResult{
		PolicyVersion:  MemoryExtractionBarrierPolicyV1,
		Cutoff:         cutoff,
		Status:         MemoryExtractionBarrierWaited,
		Waited:         150 * time.Millisecond,
		CoveredThrough: coveredThrough,
	}}
	assembler := newMemoryBarrierTestAssembler(t, 1, barrier)

	manifest, _, err := assembler.Assemble(
		context.Background(),
		memoryBarrierTestActor(),
		memoryBarrierTestCommand(cutoff),
	)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if barrier.calls != 1 ||
		barrier.request.Actor != memoryBarrierTestActor() ||
		!barrier.request.Cutoff.Equal(cutoff) {
		t.Fatalf("barrier request = %#v calls=%d", barrier.request, barrier.calls)
	}
	if manifest.MemoryExtractionBarrierPolicyVersion !=
		MemoryExtractionBarrierPolicyV1 ||
		manifest.MemoryExtractionBarrierStatus != "waited" ||
		manifest.MemoryExtractionBarrierWaitedMilliseconds != 150 ||
		!manifest.MemoryExtractionBarrierCutoff.Equal(cutoff) ||
		!manifest.MemoryExtractionBarrierCoveredThrough.Equal(coveredThrough) ||
		!manifest.Valid() {
		t.Fatalf("barrier audit = %#v", manifest)
	}
}

func TestAssemblerSkipsMemoryExtractionBarrierAfterFirstMessage(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	barrier := &recordingContextMemoryBarrier{
		err: ErrMemoryConsistencyUnavailable,
	}
	assembler := newMemoryBarrierTestAssembler(t, 2, barrier)

	manifest, _, err := assembler.Assemble(
		context.Background(),
		memoryBarrierTestActor(),
		memoryBarrierTestCommand(cutoff),
	)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if barrier.calls != 0 ||
		manifest.MemoryExtractionBarrierStatus != "not_required" ||
		manifest.MemoryExtractionBarrierWaitedMilliseconds != 0 ||
		!manifest.MemoryExtractionBarrierCoveredThrough.IsZero() {
		t.Fatalf("continuous-turn audit = %#v calls=%d", manifest, barrier.calls)
	}
}

func TestAssemblerStopsBeforeContextReadsWhenBarrierFails(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	for _, expected := range []error{
		ErrMemoryConsistencyUnavailable,
		ErrMemoryConsistencyRejected,
	} {
		barrier := &recordingContextMemoryBarrier{err: expected}
		assembler := newMemoryBarrierTestAssembler(t, 1, barrier)
		_, _, err := assembler.Assemble(
			context.Background(),
			memoryBarrierTestActor(),
			memoryBarrierTestCommand(cutoff),
		)
		if !errors.Is(err, expected) {
			t.Fatalf("Assemble error = %v, want %v", err, expected)
		}
	}
}

func newMemoryBarrierTestAssembler(
	t *testing.T,
	sequence int64,
	barrier MemoryExtractionBarrier,
) *Assembler {
	t.Helper()
	message := conversation.Message{
		ID:       "30000000-0000-4000-8000-000000000001",
		OwnerID:  "10000000-0000-4000-8000-000000000001",
		ThreadID: "20000000-0000-4000-8000-000000000001",
		Sequence: sequence,
		Role:     conversation.MessageRoleUser,
		Modality: conversation.MessageModalityText,
		Content:  "Help me practice an interview answer.",
	}
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
		barrier,
	)
	if err != nil {
		t.Fatalf("NewAssembler: %v", err)
	}
	return assembler
}

func memoryBarrierTestActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "memory-barrier-session",
	}
}

func memoryBarrierTestCommand(cutoff time.Time) AssembleCommand {
	return AssembleCommand{
		RunID:              "40000000-0000-4000-8000-000000000001",
		OwnerID:            "10000000-0000-4000-8000-000000000001",
		ThreadID:           "20000000-0000-4000-8000-000000000001",
		InputMessageID:     "30000000-0000-4000-8000-000000000001",
		RunCreatedAt:       cutoff,
		Provider:           "fake",
		Model:              "fake-model",
		MaxOutputTokens:    256,
		MaxInputCharacters: 12000,
	}
}

type recordingContextMemoryBarrier struct {
	result  MemoryExtractionBarrierResult
	err     error
	request MemoryExtractionBarrierRequest
	calls   int
}

func (barrier *recordingContextMemoryBarrier) Await(
	_ context.Context,
	request MemoryExtractionBarrierRequest,
) (MemoryExtractionBarrierResult, error) {
	barrier.calls++
	barrier.request = request
	return barrier.result, barrier.err
}
