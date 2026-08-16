package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestThreadSummaryWorkerStoresOneCurrentCompression(t *testing.T) {
	database := newAgentTestDatabase(t)
	dataService, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA())
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	seedSummaryMessages(t, database.pool, thread.ID, 4, 1600)
	setSummaryTarget(t, database.pool, thread.ID, 4)

	generator := newFixedSummaryGenerator(agentsummary.GenerationResult{
		Provider: "fake",
		Model:    "configured-model",
		Content:  summaryValidJSON(),
	})
	configuration := summaryWorkerConfiguration()
	generation, err := agentsummary.NewGeneratorService(
		generator,
		configuration.Generation,
	)
	if err != nil {
		t.Fatalf("new Summary generator: %v", err)
	}
	worker, err := agentsummary.NewWorker(
		repositories.summary,
		generation,
		configuration,
	)
	if err != nil {
		t.Fatalf("new Summary worker: %v", err)
	}
	result, err := worker.ProcessPending(ctx, 1)
	if err != nil {
		t.Fatalf("process Summary: %v", err)
	}
	if result.Completed != 1 || generator.CallCount() != 1 {
		t.Fatalf("result = %#v generator calls = %d", result, generator.CallCount())
	}

	state, err := repositories.summary.FindSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		4,
	)
	if err != nil {
		t.Fatalf("find current Summary: %v", err)
	}
	if state.ThroughSequence != 3 ||
		len(state.Content.CurrentIntents) != 1 ||
		state.Content.CurrentIntents[0] != "Continue the conversation" {
		t.Fatalf("current Summary = %#v", state)
	}
	var target *int64
	if err := database.pool.QueryRow(ctx, `
SELECT summary_target_sequence
FROM agent_threads
WHERE id = $1`, thread.ID).Scan(&target); err != nil {
		t.Fatalf("read Summary target: %v", err)
	}
	if target == nil || *target != 4 {
		t.Fatalf("remaining Summary target = %v, want 4", target)
	}
}

func TestThreadSummaryClaimSerializesOneThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	dataService, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	thread, err := dataService.CreateThread(context.Background(), testActorA())
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	seedSummaryMessages(t, database.pool, thread.ID, 1, 32)
	setSummaryTarget(t, database.pool, thread.ID, 1)

	type claimResult struct {
		claim    agentsummary.Claim
		acquired bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claim, acquired, claimErr := repositories.summary.Claim(
				context.Background(),
				summaryWorkerConfiguration(),
			)
			results <- claimResult{claim: claim, acquired: acquired, err: claimErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	acquired := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("claim Summary: %v", result.err)
		}
		if result.acquired {
			acquired++
			if result.claim.OwnerID != agentTestUserA || result.claim.ThreadID != thread.ID {
				t.Fatalf("claim = %#v", result.claim)
			}
		}
	}
	if acquired != 1 {
		t.Fatalf("acquired claims = %d, want 1", acquired)
	}
}

func TestThreadSummaryClaimLocksUserBeforeThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	dataService, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA())
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	seedSummaryMessages(t, database.pool, thread.ID, 1, 32)
	setSummaryTarget(t, database.pool, thread.ID, 1)

	ownerLock, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner lock: %v", err)
	}
	defer func() { _ = ownerLock.Rollback(ctx) }()
	if _, err := ownerLock.Exec(ctx, `
SELECT id FROM users WHERE id = $1 FOR UPDATE`, agentTestUserA); err != nil {
		t.Fatalf("lock owner: %v", err)
	}

	type claimResult struct {
		claim    agentsummary.Claim
		acquired bool
		err      error
	}
	result := make(chan claimResult, 1)
	go func() {
		claim, acquired, claimErr := repositories.summary.Claim(
			context.Background(),
			summaryWorkerConfiguration(),
		)
		result <- claimResult{claim: claim, acquired: acquired, err: claimErr}
	}()
	select {
	case early := <-result:
		t.Fatalf("Summary claim bypassed owner lock: %#v", early)
	case <-time.After(100 * time.Millisecond):
	}
	if err := ownerLock.Rollback(ctx); err != nil {
		t.Fatalf("release owner lock: %v", err)
	}
	select {
	case claimed := <-result:
		if claimed.err != nil || !claimed.acquired || claimed.claim.ThreadID != thread.ID {
			t.Fatalf("claim after owner unlock = %#v", claimed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Summary claim did not resume after owner unlock")
	}
}

func TestThreadSummaryFailureIsExplicitAndNotImmediatelyReclaimed(t *testing.T) {
	database := newAgentTestDatabase(t)
	dataService, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA())
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	seedSummaryMessages(t, database.pool, thread.ID, 1, 32)
	setSummaryTarget(t, database.pool, thread.ID, 1)
	configuration := summaryWorkerConfiguration()
	claim, acquired, err := repositories.summary.Claim(ctx, configuration)
	if err != nil || !acquired {
		t.Fatalf("claim Summary: acquired=%t err=%v", acquired, err)
	}
	retry, err := repositories.summary.Fail(
		ctx,
		claim,
		"invalid_response",
		false,
		configuration,
	)
	if err != nil || retry {
		t.Fatalf("fail Summary: retry=%t err=%v", retry, err)
	}
	var failure string
	if err := database.pool.QueryRow(ctx, `
SELECT summary_error
FROM agent_threads
WHERE id = $1`, thread.ID).Scan(&failure); err != nil {
		t.Fatalf("read Summary failure: %v", err)
	}
	if failure != "invalid_response" {
		t.Fatalf("Summary failure = %q", failure)
	}
	if _, found, err := repositories.summary.Claim(ctx, configuration); err != nil || found {
		t.Fatalf("failed Summary reclaimed: found=%t err=%v", found, err)
	}
	if _, err := repositories.summary.FindSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		1,
	); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("failed Summary lookup = %v, want not found", err)
	}
}

func summaryWorkerConfiguration() agentsummary.WorkerConfiguration {
	return agentsummary.WorkerConfiguration{
		MaxContextCharacters: testRunConfiguration.MaxInputCharacters,
		LeaseDuration:        time.Minute,
		MaxAttempts:          agentsummary.DefaultWorkerMaxAttempts,
		Generation: agentsummary.Configuration{
			Provider: "fake",
			Model:    "configured-model",
		},
	}
}

func summaryValidJSON() string {
	return `{"current_intents":["Continue the conversation"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`
}

func seedSummaryMessages(
	t *testing.T,
	pool *pgxpool.Pool,
	threadID string,
	count int,
	characters int,
) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Summary source seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND user_id = $2
FOR UPDATE`, threadID, agentTestUserA).Scan(&nextSequence); err != nil {
		t.Fatalf("lock Summary source Thread: %v", err)
	}
	ids := identity.NewUUIDv4Generator(nil)
	for offset := range count {
		messageID, err := ids.NewID()
		if err != nil {
			t.Fatalf("generate Summary Message ID: %v", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, client_message_id, content, modality
) VALUES ($1, $2, $3, 'user', $4, $5, 'text')`,
			messageID,
			threadID,
			nextSequence+int64(offset),
			"summary-source-"+messageID,
			strings.Repeat("x", characters),
		); err != nil {
			t.Fatalf("insert Summary source Message: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET next_message_sequence = next_message_sequence + $3,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE id = $1 AND user_id = $2`, threadID, agentTestUserA, count); err != nil {
		t.Fatalf("advance Summary source Thread: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit Summary source seed: %v", err)
	}
}

func setSummaryTarget(
	t *testing.T,
	pool *pgxpool.Pool,
	threadID string,
	target int64,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
UPDATE agent_threads
SET summary_target_sequence = $2,
    summary_available_at = CURRENT_TIMESTAMP,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE id = $1`, threadID, target); err != nil {
		t.Fatalf("set Summary target: %v", err)
	}
}
