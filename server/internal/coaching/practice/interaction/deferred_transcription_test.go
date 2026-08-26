package interaction

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestDeferredSubmissionCompletesOnlyAfterTurnConfirmation(t *testing.T) {
	transcribed := deferredSubmission(TranscriptionReservation{
		ID: "reservation-1", SessionID: "session-1", QuestionID: "question-1",
		Status: TranscriptionCompleted,
	})
	confirmed := deferredSubmission(TranscriptionReservation{
		ID: "reservation-1", SessionID: "session-1", QuestionID: "question-1",
		Status: TranscriptionConfirmed,
	})
	if transcribed.Status != TranscriptionProcessing {
		t.Fatalf("transcribed status = %q", transcribed.Status)
	}
	if confirmed.Status != TranscriptionCompleted {
		t.Fatalf("confirmed status = %q", confirmed.Status)
	}
}

func TestDeferredProcessorDeduplicatesWithoutBlockingStatusReads(t *testing.T) {
	processor := &DeferredTranscriptionProcessor{
		ctx:     context.Background(),
		queue:   make(chan deferredTranscriptionRequest, 1),
		pending: make(map[string]struct{}),
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	reservation := TranscriptionReservation{ID: "reservation-1"}
	if err := processor.Enqueue(context.Background(), actor, reservation); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := processor.Enqueue(context.Background(), actor, reservation); err != nil {
		t.Fatalf("deduplicated enqueue: %v", err)
	}
	if got := len(processor.queue); got != 1 {
		t.Fatalf("queued requests = %d", got)
	}
	if err := processor.Enqueue(
		context.Background(), actor, TranscriptionReservation{ID: "reservation-2"},
	); err != nil {
		t.Fatalf("full queue enqueue: %v", err)
	}
}

func TestDeferredProcessorCompletesQueuedReservation(t *testing.T) {
	candidate := roundCandidate()
	reservation := TranscriptionReservation{
		ID: "reservation-1", SessionID: candidate.SessionID,
		QuestionID: candidate.QuestionID, Status: TranscriptionReserved,
	}
	confirmed := make(chan string, 1)
	rounds := &roundVoice{
		candidate:          candidate,
		turn:               roundTurn(candidate),
		deferred:           reservation,
		confirmationCalled: confirmed,
	}
	orchestrator, err := NewRoundOrchestrator(rounds, roundPractice{})
	if err != nil {
		t.Fatalf("new orchestrator: %v", err)
	}
	processorContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	processor, err := NewDeferredTranscriptionProcessor(
		processorContext,
		orchestrator,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("new processor: %v", err)
	}
	if err := processor.Enqueue(
		context.Background(), roundActor(), reservation,
	); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case key := <-confirmed:
		if key != "deferred-confirm-reservation-1" {
			t.Fatalf("confirmation key = %q", key)
		}
	case <-time.After(time.Second):
		t.Fatal("queued transcription was not processed")
	}
}

func TestDeferredProcessorRejectsMissingDependencies(t *testing.T) {
	if processor, err := NewDeferredTranscriptionProcessor(
		context.Background(), nil, slog.Default(),
	); err == nil || processor != nil {
		t.Fatalf("processor = %#v, error = %v", processor, err)
	}
}
