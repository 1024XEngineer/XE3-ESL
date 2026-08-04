package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresExtractionBarrierOwnerAndCutoffIsolation(t *testing.T) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	ctx := context.Background()
	base := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	actorA := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	actorB := requestcontext.Actor{
		UserID:    integrationUserB,
		SessionID: integrationSessionB,
	}
	jobs := []barrierIntegrationJob{
		{
			runID:           "aa000000-0000-4000-8000-000000000001",
			ownerID:         actorA.UserID,
			status:          "pending",
			sourceCompleted: base.Add(time.Second),
		},
		{
			runID:           "aa000000-0000-4000-8000-000000000002",
			ownerID:         actorA.UserID,
			status:          "running",
			sourceCompleted: base.Add(2 * time.Second),
		},
		{
			runID:           "aa000000-0000-4000-8000-000000000003",
			ownerID:         actorA.UserID,
			status:          "completed",
			sourceCompleted: base.Add(3 * time.Second),
		},
		{
			runID:           "aa000000-0000-4000-8000-000000000004",
			ownerID:         actorA.UserID,
			status:          "failed",
			sourceCompleted: base.Add(4 * time.Second),
		},
		{
			runID:           "aa000000-0000-4000-8000-000000000005",
			ownerID:         actorA.UserID,
			status:          "discarded",
			sourceCompleted: base.Add(5 * time.Second),
		},
		{
			runID:           "aa000000-0000-4000-8000-000000000006",
			ownerID:         actorA.UserID,
			status:          "pending",
			sourceCompleted: base.Add(7 * time.Second),
		},
		{
			runID:           "bb000000-0000-4000-8000-000000000001",
			ownerID:         actorB.UserID,
			status:          "pending",
			sourceCompleted: base.Add(time.Second),
		},
	}
	for _, job := range jobs {
		insertBarrierIntegrationJob(t, database, job)
	}

	cutoff := base.Add(6 * time.Second)
	snapshot, err := repository.ReadExtractionBarrier(
		ctx,
		ExtractionBarrierRequest{Actor: actorA, Cutoff: cutoff},
	)
	if err != nil {
		t.Fatalf("ReadExtractionBarrier actor A: %v", err)
	}
	expected := ExtractionBarrierSnapshot{
		Cutoff:                               cutoff,
		JobCount:                             5,
		PendingCount:                         1,
		RunningCount:                         1,
		CompletedCount:                       1,
		FailedCount:                          1,
		DiscardedCount:                       1,
		LatestSourceCompletedAt:              base.Add(5 * time.Second),
		EarliestNonTerminalSourceCompletedAt: base.Add(time.Second),
	}
	if snapshot != expected {
		t.Fatalf("actor A snapshot = %#v, want %#v", snapshot, expected)
	}

	snapshot, err = repository.ReadExtractionBarrier(
		ctx,
		ExtractionBarrierRequest{Actor: actorB, Cutoff: cutoff},
	)
	if err != nil {
		t.Fatalf("ReadExtractionBarrier actor B: %v", err)
	}
	if snapshot.JobCount != 1 ||
		snapshot.PendingCount != 1 ||
		snapshot.LatestSourceCompletedAt != base.Add(time.Second) {
		t.Fatalf("actor B snapshot = %#v", snapshot)
	}

	emptyCutoff := base
	snapshot, err = repository.ReadExtractionBarrier(
		ctx,
		ExtractionBarrierRequest{Actor: actorA, Cutoff: emptyCutoff},
	)
	if err != nil {
		t.Fatalf("ReadExtractionBarrier empty cutoff: %v", err)
	}
	if snapshot != (ExtractionBarrierSnapshot{Cutoff: emptyCutoff}) ||
		!snapshot.Ready() {
		t.Fatalf("empty snapshot = %#v", snapshot)
	}
}

func TestPostgresExtractionBarrierRejectsInvalidRequest(t *testing.T) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	valid := ExtractionBarrierRequest{
		Actor: requestcontext.Actor{
			UserID:    integrationUserA,
			SessionID: integrationSessionA,
		},
		Cutoff: time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC),
	}
	cases := map[string]ExtractionBarrierRequest{
		"invalid actor": {
			Actor:  requestcontext.Actor{},
			Cutoff: valid.Cutoff,
		},
		"zero cutoff": {
			Actor: valid.Actor,
		},
		"non-UTC cutoff": {
			Actor: valid.Actor,
			Cutoff: valid.Cutoff.In(
				time.FixedZone("UTC+8", 8*60*60),
			),
		},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := repository.ReadExtractionBarrier(
				context.Background(),
				request,
			)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("ReadExtractionBarrier error = %v", err)
			}
		})
	}
}

func TestPostgresExtractionBarrierObservesConcurrentCompletionAndRestart(
	t *testing.T,
) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	base := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	actor := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	job := barrierIntegrationJob{
		runID:           "aa000000-0000-4000-8000-000000000010",
		ownerID:         actor.UserID,
		status:          "pending",
		sourceCompleted: base,
	}
	insertBarrierIntegrationJob(t, database, job)
	cutoff := base.Add(2 * time.Second)
	scheduler := &fakeExtractionBarrierScheduler{now: cutoff.Add(time.Second)}
	scheduler.onWait = func() {
		_, updateErr := database.Exec(context.Background(), `
UPDATE agent_memory_extraction_jobs
SET
    status = 'completed',
    attempt_count = 1,
    candidate_count = 0,
    applied_count = 0,
    rejected_count = 0,
    updated_at = transaction_timestamp(),
    completed_at = transaction_timestamp()
WHERE source_run_id = $1`, job.runID)
		if updateErr != nil {
			t.Fatalf("complete Extraction job: %v", updateErr)
		}
		scheduler.onWait = nil
	}
	coordinator := newTestExtractionBarrierCoordinator(
		t,
		repository,
		scheduler,
	)

	result, err := coordinator.Await(
		context.Background(),
		ExtractionBarrierRequest{Actor: actor, Cutoff: cutoff},
	)
	if err != nil {
		t.Fatalf("Await concurrent completion: %v", err)
	}
	if result.Status != ExtractionBarrierWaited ||
		!result.CoveredThrough.Equal(job.sourceCompleted) {
		t.Fatalf("waited result = %#v", result)
	}

	restarted := newTestExtractionBarrierCoordinator(
		t,
		repository,
		&fakeExtractionBarrierScheduler{now: scheduler.now},
	)
	restartedResult, err := restarted.Await(
		context.Background(),
		ExtractionBarrierRequest{Actor: actor, Cutoff: cutoff},
	)
	if err != nil {
		t.Fatalf("Await after restart: %v", err)
	}
	if restartedResult.Status != ExtractionBarrierReady ||
		restartedResult.Waited != 0 {
		t.Fatalf("restart result = %#v", restartedResult)
	}
}

type barrierIntegrationJob struct {
	runID           string
	ownerID         string
	status          string
	sourceCompleted time.Time
}

func insertBarrierIntegrationJob(
	t *testing.T,
	database *pgxpool.Pool,
	job barrierIntegrationJob,
) {
	t.Helper()
	createdAt := job.sourceCompleted.Add(time.Second)
	attemptCount := 0
	var leaseToken any
	var leaseExpiresAt any
	var candidateCount any
	var appliedCount any
	var rejectedCount any
	var failureKind any
	var completedAt any
	switch job.status {
	case "running":
		attemptCount = 1
		leaseToken = "cc000000-0000-4000-8000-000000000001"
		leaseExpiresAt = createdAt.Add(time.Minute)
	case "completed":
		attemptCount = 1
		candidateCount = 1
		appliedCount = 1
		rejectedCount = 0
		completedAt = createdAt
	case "failed", "discarded":
		attemptCount = 1
		failureKind = "test_failure"
		completedAt = createdAt
	}
	_, err := database.Exec(context.Background(), `
INSERT INTO agent_memory_extraction_jobs (
    source_run_id,
    owner_user_id,
    source_thread_id,
    source_input_message_id,
    source_assistant_message_id,
    source_attempt,
    source_completed_at,
    status,
    attempt_count,
    lease_token,
    lease_expires_at,
    next_attempt_at,
    candidate_count,
    applied_count,
    rejected_count,
    failure_kind,
    created_at,
    updated_at,
    completed_at
) VALUES (
    $1, $2,
    'dd000000-0000-4000-8000-000000000001',
    'ee000000-0000-4000-8000-000000000001',
    'ff000000-0000-4000-8000-000000000001',
    1, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $13, $14
)`,
		job.runID,
		job.ownerID,
		job.sourceCompleted,
		job.status,
		attemptCount,
		leaseToken,
		leaseExpiresAt,
		createdAt,
		candidateCount,
		appliedCount,
		rejectedCount,
		failureKind,
		createdAt,
		completedAt,
	)
	if err != nil {
		t.Fatalf("insert %s Extraction Barrier job: %v", job.status, err)
	}
}
