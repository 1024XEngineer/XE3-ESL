package interaction

import (
	"context"
	"testing"

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
