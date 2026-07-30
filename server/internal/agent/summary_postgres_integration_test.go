package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSummaryCheckpointChainVisibilityAndOwnership(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-message-1",
	)

	first, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, "", 1, 2, "first source"),
	)
	if err != nil {
		t.Fatalf("create first checkpoint: %v", err)
	}
	if !first.Valid() {
		t.Fatalf("invalid persisted checkpoint: %#v", first)
	}
	if _, err := repository.FindLatestSummaryCheckpoint(
		ctx,
		agentTestUserA,
		thread.ID,
		1,
	); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("lookup before first coverage = %v, want not found", err)
	}

	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-message-2",
	)
	second, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, first.ID, 3, 4, "second source"),
	)
	if err != nil {
		t.Fatalf("create second checkpoint: %v", err)
	}
	if second.PreviousCheckpointID != first.ID {
		t.Fatalf("previous checkpoint = %q, want %q", second.PreviousCheckpointID, first.ID)
	}

	visibleAtThree, err := repository.FindLatestSummaryCheckpoint(
		ctx,
		agentTestUserA,
		thread.ID,
		3,
	)
	if err != nil {
		t.Fatalf("find visible checkpoint at sequence 3: %v", err)
	}
	if visibleAtThree.ID != first.ID {
		t.Fatalf("checkpoint at sequence 3 = %q, want first %q", visibleAtThree.ID, first.ID)
	}
	visibleAtFour, err := repository.FindLatestSummaryCheckpoint(
		ctx,
		agentTestUserA,
		thread.ID,
		4,
	)
	if err != nil {
		t.Fatalf("find visible checkpoint at sequence 4: %v", err)
	}
	if visibleAtFour.ID != second.ID {
		t.Fatalf("checkpoint at sequence 4 = %q, want second %q", visibleAtFour.ID, second.ID)
	}
	if _, err := repository.FindLatestSummaryCheckpoint(
		ctx,
		agentTestUserB,
		thread.ID,
		4,
	); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-owner lookup = %v, want not found", err)
	}
}

func TestPostgresSummaryCheckpointRejectsInvalidChainAndFutureCoverage(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-invalid-1",
	)
	first, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, "", 1, 2, "first source"),
	)
	if err != nil {
		t.Fatalf("create first checkpoint: %v", err)
	}

	future := summaryCommand(thread.ID, first.ID, 3, 4, "future source")
	if _, err := repository.CreateSummaryCheckpoint(
		ctx,
		future,
	); !errors.Is(err, core.ErrInvalidRequest) {
		t.Fatalf("future coverage error = %v, want invalid request", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-invalid-2",
	)
	wrongPrevious := summaryCommand(
		thread.ID,
		"50000000-0000-4000-8000-000000000001",
		3,
		4,
		"wrong previous",
	)
	if _, err := repository.CreateSummaryCheckpoint(
		ctx,
		wrongPrevious,
	); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("wrong previous error = %v, want conflict", err)
	}
}

func TestPostgresSummaryCheckpointConcurrentCreateDoesNotFork(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-race-1",
	)
	first, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, "", 1, 2, "race first"),
	)
	if err != nil {
		t.Fatalf("create first checkpoint: %v", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-race-2",
	)
	command := summaryCommand(thread.ID, first.ID, 3, 4, "race second")

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, createErr := repository.CreateSummaryCheckpoint(ctx, command)
			results <- createErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for createErr := range results {
		switch {
		case createErr == nil:
			successes++
		case errors.Is(createErr, core.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent create error: %v", createErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

func TestPostgresSummaryCheckpointCascadesWithThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    modality
) VALUES
    ($1, $2, $3, 1, 'user', $4, $5, 'text'),
    ($6, $2, $3, 2, 'user', $7, $8, 'text')`,
		"60000000-0000-4000-8000-000000000001",
		agentTestUserA,
		thread.ID,
		"summary-cascade-1",
		"First source message for cascade validation.",
		"60000000-0000-4000-8000-000000000002",
		"summary-cascade-2",
		"Second source message for cascade validation.",
	); err != nil {
		t.Fatalf("insert source message: %v", err)
	}
	if _, err := database.pool.Exec(ctx, `
UPDATE agent_threads
SET next_message_sequence = 3, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND owner_user_id = $2`,
		thread.ID,
		agentTestUserA,
	); err != nil {
		t.Fatalf("advance thread sequence: %v", err)
	}
	first, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, "", 1, 1, "cascade source"),
	)
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if _, err := repository.CreateSummaryCheckpoint(
		ctx,
		summaryCommand(thread.ID, first.ID, 2, 2, "cascade source 2"),
	); err != nil {
		t.Fatalf("create continuation checkpoint: %v", err)
	}
	if _, err := database.pool.Exec(
		ctx,
		"DELETE FROM agent_threads WHERE id = $1 AND owner_user_id = $2",
		thread.ID,
		agentTestUserA,
	); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	var count int
	if err := database.pool.QueryRow(
		ctx,
		"SELECT count(*) FROM agent_thread_summary_checkpoints WHERE thread_id = $1",
		thread.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count checkpoints: %v", err)
	}
	if count != 0 {
		t.Fatalf("checkpoint count after thread delete = %d, want 0", count)
	}
}

func TestPostgresSummarySourceAndGenerationService(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, _, repository := newAgentRunServices(
		t,
		database.pool,
		fake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-generate-1",
	)

	messages, err := repository.ListMessagesForSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		1,
		2,
	)
	if err != nil {
		t.Fatalf("list first summary source: %v", err)
	}
	if len(messages) != 2 ||
		messages[0].Sequence != 1 ||
		messages[0].Role != MessageRoleUser ||
		messages[1].Sequence != 2 ||
		messages[1].Role != MessageRoleUser {
		t.Fatalf("unexpected summary source: %#v", messages)
	}
	if _, err := repository.ListMessagesForSummary(
		ctx,
		agentTestUserB,
		thread.ID,
		1,
		2,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner source error = %v, want not found", err)
	}
	if _, err := repository.ListMessagesForSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		2,
		3,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("future source error = %v, want not found", err)
	}

	generator := fake.NewTextGenerator(summaryGenerationResult())
	service, err := NewSummaryService(
		repository,
		generator,
		SummaryConfiguration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      "qianwen",
			Model:         "qwen-plus",
		},
	)
	if err != nil {
		t.Fatalf("new summary service: %v", err)
	}
	first, err := service.GenerateCheckpoint(
		ctx,
		GenerateSummaryCheckpointCommand{
			OwnerID:                agentTestUserA,
			ThreadID:               thread.ID,
			CoveredThroughSequence: 2,
		},
	)
	if err != nil {
		t.Fatalf("generate first checkpoint: %v", err)
	}
	if !first.Valid() ||
		first.PreviousCheckpointID != "" ||
		first.SourceFromSequence != 1 ||
		first.CoveredThroughSequence != 2 {
		t.Fatalf("unexpected first generated checkpoint: %#v", first)
	}

	seedSummarySourceMessages(
		t,
		database.pool,
		thread.ID,
		"summary-generate-2",
	)
	second, err := service.GenerateCheckpoint(
		ctx,
		GenerateSummaryCheckpointCommand{
			OwnerID:                agentTestUserA,
			ThreadID:               thread.ID,
			CoveredThroughSequence: 4,
		},
	)
	if err != nil {
		t.Fatalf("generate rolling checkpoint: %v", err)
	}
	if !second.Valid() ||
		second.PreviousCheckpointID != first.ID ||
		second.SourceFromSequence != 3 ||
		second.CoveredThroughSequence != 4 ||
		second.SourceChecksum == first.SourceChecksum {
		t.Fatalf("unexpected rolling checkpoint: %#v", second)
	}
}

func TestPostgresSummarySourceRejectsCharacterOverflow(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService := newAgentDataServices(t, database.pool)
	repository, err := NewPostgresRepository(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	ctx := context.Background()
	thread, err := dataService.CreateThread(ctx, testActorA(), "")
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	for sequence := 1; sequence <= 16; sequence++ {
		if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    modality
) VALUES ($1, $2, $3, $4, 'user', $5, $6, 'text')`,
			fmt.Sprintf(
				"70000000-0000-4000-8000-%012d",
				sequence,
			),
			agentTestUserA,
			thread.ID,
			sequence,
			fmt.Sprintf("summary-large-%d", sequence),
			strings.Repeat("界", 4096),
		); err != nil {
			t.Fatalf("insert source message %d: %v", sequence, err)
		}
	}
	if _, err := repository.ListMessagesForSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		1,
		16,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("overflow source error = %v, want invalid request", err)
	}
	if _, err := repository.ListMessagesForSummary(
		ctx,
		agentTestUserA,
		thread.ID,
		1,
		core.MaxSummarySourceMessages+1,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("row overflow error = %v, want invalid request", err)
	}
}

func summaryCommand(
	threadID string,
	previousCheckpointID string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
	source string,
) CreateThreadSummaryCheckpointCommand {
	return CreateThreadSummaryCheckpointCommand{
		OwnerID:                agentTestUserA,
		ThreadID:               threadID,
		PreviousCheckpointID:   previousCheckpointID,
		SourceFromSequence:     sourceFromSequence,
		CoveredThroughSequence: coveredThroughSequence,
		Content: ThreadSummaryContent{
			Goals:         []string{"Prepare for an English product interview"},
			Background:    []string{},
			Progress:      []string{},
			Decisions:     []string{},
			OpenQuestions: []string{},
			NextSteps:     []string{"Practice a quantified STAR answer"},
		},
		PolicyVersion:  "summary-policy-v1",
		PromptVersion:  "summary-prompt-v1",
		Provider:       "qwen",
		Model:          "qwen-plus",
		SourceChecksum: sha256.Sum256([]byte(source)),
	}
}

func summaryGenerationResult() ai.TextResult {
	return ai.TextResult{
		ID:       "summary-completion-1",
		Provider: "qianwen",
		Model:    "qwen-plus",
		Content: `{"goals":["Prepare for an English product interview"],` +
			`"background":[],"progress":[],"decisions":[],` +
			`"open_questions":[],"next_steps":["Practice a STAR answer"]}`,
		FinishReason: "stop",
	}
}

func seedSummarySourceMessages(
	t *testing.T,
	pool *pgxpool.Pool,
	threadID string,
	clientMessageIDPrefix string,
) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed source transaction: %v", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		threadID,
		agentTestUserA,
	).Scan(&nextSequence); err != nil {
		t.Fatalf("lock source thread: %v", err)
	}
	ids := identity.NewUUIDv4Generator(nil)
	for offset := range int64(2) {
		messageID, err := ids.NewID()
		if err != nil {
			t.Fatalf("generate source message ID: %v", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    modality
) VALUES ($1, $2, $3, $4, 'user', $5, $6, 'text')`,
			messageID,
			agentTestUserA,
			threadID,
			nextSequence+offset,
			fmt.Sprintf("%s-%d", clientMessageIDPrefix, offset+1),
			fmt.Sprintf("Summary source message %d.", nextSequence+offset),
		); err != nil {
			t.Fatalf("insert summary source message: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 2,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
		threadID,
		agentTestUserA,
	); err != nil {
		t.Fatalf("advance summary source thread: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit summary source messages: %v", err)
	}
}
