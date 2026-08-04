package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	aifake "github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestCompletedAgentRunQueuesAndAppliesMemoryExtraction(
	t *testing.T,
) {
	database := newMemoryTestDatabase(t)
	ctx := context.Background()
	actor := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	ids := identity.NewUUIDv4Generator(nil)
	goalRepository, err := goal.NewPostgresRepository(database, ids)
	if err != nil {
		t.Fatalf("new Goal repository: %v", err)
	}
	goalService, err := goal.NewService(goalRepository)
	if err != nil {
		t.Fatalf("new Goal service: %v", err)
	}
	conversationRepository, err := conversationpostgres.New(database, ids)
	if err != nil {
		t.Fatalf("new Agent Conversation repository: %v", err)
	}
	contextRepository, err := contextpostgres.New(database)
	if err != nil {
		t.Fatalf("new Agent Context repository: %v", err)
	}
	runRepository, err := runpostgres.New(database, ids)
	if err != nil {
		t.Fatalf("new Agent Run repository: %v", err)
	}
	agentService, err := conversation.NewService(
		conversationRepository,
		goalService,
	)
	if err != nil {
		t.Fatalf("new Agent service: %v", err)
	}
	thread, err := agentService.CreateThread(
		ctx,
		actor,
		integrationGoalA,
	)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	assembler, err := agentcontext.NewAssembler(
		contextRepository,
		goalService,
		emptyAgentLearningProfileReader{},
		emptyAgentStableProfileReader{},
		emptyAgentMemorySearcher{},
		emptyAgentMemoryExtractionBarrier{},
	)
	if err != nil {
		t.Fatalf("NewContextAssembler: %v", err)
	}
	runConfiguration := agentrun.Configuration{
		Provider:           "qianwen",
		Model:              "qwen-plus",
		MaxOutputTokens:    128,
		MaxInputCharacters: 8192,
	}
	generator := aifake.NewTextGenerator(ai.TextResult{
		ID:           "completion-memory-1",
		Provider:     runConfiguration.Provider,
		Model:        runConfiguration.Model,
		Content:      "Thanks, I will tailor the interview practice.",
		FinishReason: "stop",
		Usage: ai.TokenUsage{
			InputTokens:  10,
			OutputTokens: 8,
			TotalTokens:  18,
		},
	})
	runService, err := agentrun.NewService(
		runRepository,
		conversationRepository,
		contextRepository,
		assembler,
		generator,
		runConfiguration,
	)
	if err != nil {
		t.Fatalf("NewRunService: %v", err)
	}
	submission, err := runService.SubmitText(
		ctx,
		actor,
		thread.ID,
		"memory-source-message-1",
		"I am a Java backend engineer.",
	)
	if err != nil {
		t.Fatalf("SubmitText: %v", err)
	}
	if submission.Run.Status != agentrun.StatusCompleted {
		t.Fatalf("completed Run = %#v", submission.Run)
	}

	repository, err := NewPostgresRepository(database, ids)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	configuration := testExtractionConfig()
	claim, acquired, err := repository.ClaimExtraction(
		ctx,
		configuration,
	)
	if err != nil {
		t.Fatalf("ClaimExtraction: %v", err)
	}
	if !acquired ||
		claim.RunID != submission.Run.ID ||
		claim.OwnerID != actor.UserID ||
		claim.InputMessageID != submission.Run.InputMessageID ||
		claim.AssistantMessageID != submission.Run.AssistantMessageID {
		t.Fatalf("claim = %#v, acquired=%t", claim, acquired)
	}

	userText := "I am a Java backend engineer."
	batch := ExtractionBatch{
		CandidateCount: 2,
		Decisions: []MemoryDecision{
			{
				Action:       CandidateUpsert,
				Type:         TypeProfile,
				CanonicalKey: "career.role",
				Content:      "Java backend engineer",
				Scope:        ScopeUser,
			},
			{
				Action:       CandidateUpsert,
				Type:         TypeGoal,
				CanonicalKey: "goal.current",
				Content:      "Prepare for backend interview",
				Scope:        ScopeGoal,
				GoalID:       integrationGoalA,
			},
		},
		Source: SourceInput{
			Type:     SourceAgentRun,
			SourceID: claim.RunID,
			Version:  int64(claim.SourceAttempt),
			Checksum: sha256.Sum256([]byte(userText)),
		},
	}
	completed, err := repository.CompleteExtraction(ctx, claim, batch)
	if err != nil {
		t.Fatalf("CompleteExtraction: %v", err)
	}
	if completed.Status != ExtractionCompleted ||
		completed.CandidateCount != 2 ||
		completed.AppliedCount != 2 ||
		completed.RejectedCount != 0 {
		t.Fatalf("completed extraction = %#v", completed)
	}
	userMemories, err := repository.ListActive(ctx, actor, ScopeFilter{
		Scope: ScopeUser,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListActive user: %v", err)
	}
	goalMemories, err := repository.ListActive(ctx, actor, ScopeFilter{
		Scope:  ScopeGoal,
		GoalID: integrationGoalA,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListActive goal: %v", err)
	}
	if len(userMemories) != 1 ||
		userMemories[0].CanonicalKey != "career.role" ||
		len(goalMemories) != 1 ||
		goalMemories[0].CanonicalKey != "goal.current" {
		t.Fatalf(
			"user=%#v goal=%#v",
			userMemories,
			goalMemories,
		)
	}
	sources, err := repository.ListSources(
		ctx,
		actor,
		userMemories[0].ID,
	)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 ||
		sources[0].SourceID != submission.Run.ID {
		t.Fatalf("sources = %#v", sources)
	}

	second, err := runService.SubmitText(
		ctx,
		actor,
		thread.ID,
		"memory-source-message-2",
		"I prefer concise feedback.",
	)
	if err != nil {
		t.Fatalf("SubmitText second: %v", err)
	}
	oldClaim, acquired, err := repository.ClaimExtraction(
		ctx,
		configuration,
	)
	if err != nil || !acquired || oldClaim.RunID != second.Run.ID {
		t.Fatalf("old claim = %#v acquired=%t err=%v", oldClaim, acquired, err)
	}
	if _, err := database.Exec(ctx, `
UPDATE agent_memory_extraction_jobs
SET lease_expires_at = updated_at + INTERVAL '1 microsecond'
WHERE source_run_id = $1`,
		oldClaim.RunID,
	); err != nil {
		t.Fatalf("expire extraction lease: %v", err)
	}
	newClaim, acquired, err := repository.ClaimExtraction(
		ctx,
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("reclaim = %#v acquired=%t err=%v", newClaim, acquired, err)
	}
	if newClaim.RunID != oldClaim.RunID ||
		newClaim.AttemptCount != oldClaim.AttemptCount+1 ||
		newClaim.LeaseToken == oldClaim.LeaseToken {
		t.Fatalf("reclaimed job = %#v old=%#v", newClaim, oldClaim)
	}
	staleBatch := ExtractionBatch{
		CandidateCount: 0,
		Source: SourceInput{
			Type:     SourceAgentRun,
			SourceID: oldClaim.RunID,
			Version:  int64(oldClaim.SourceAttempt),
			Checksum: sha256.Sum256([]byte("stale")),
		},
	}
	if _, err := repository.CompleteExtraction(
		ctx,
		oldClaim,
		staleBatch,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CompleteExtraction error = %v", err)
	}
	requeued, err := repository.FailExtraction(
		ctx,
		newClaim,
		"provider_unavailable",
		true,
		configuration,
	)
	if err != nil {
		t.Fatalf("FailExtraction retry: %v", err)
	}
	if requeued.Status != ExtractionPending {
		t.Fatalf("requeued job = %#v", requeued)
	}
	if _, err := database.Exec(ctx, `
UPDATE agent_memory_extraction_jobs
SET next_attempt_at = transaction_timestamp()
WHERE source_run_id = $1`,
		newClaim.RunID,
	); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	finalClaim, acquired, err := repository.ClaimExtraction(
		ctx,
		configuration,
	)
	if err != nil || !acquired ||
		finalClaim.AttemptCount != configuration.MaxAttempts {
		t.Fatalf("final claim = %#v acquired=%t err=%v", finalClaim, acquired, err)
	}
	failed, err := repository.FailExtraction(
		ctx,
		finalClaim,
		"provider_unavailable",
		true,
		configuration,
	)
	if err != nil {
		t.Fatalf("FailExtraction terminal: %v", err)
	}
	if failed.Status != ExtractionFailed ||
		failed.FailureKind != "provider_unavailable" {
		t.Fatalf("failed job = %#v", failed)
	}
	if _, err := database.Exec(ctx, `
UPDATE agent_memory_extraction_jobs
SET
    status = 'running',
    lease_token = 'f0000000-0000-4000-8000-000000000001',
    lease_expires_at = updated_at + INTERVAL '1 microsecond',
    failure_kind = NULL,
    completed_at = NULL
WHERE source_run_id = $1`,
		finalClaim.RunID,
	); err != nil {
		t.Fatalf("seed exhausted running job: %v", err)
	}
	if _, acquired, err := repository.ClaimExtraction(
		ctx,
		configuration,
	); acquired || !errors.Is(err, ErrExtractionExhausted) {
		t.Fatalf(
			"exhausted recovery acquired=%t error=%v",
			acquired,
			err,
		)
	}

	var jobCount int
	if err := database.QueryRow(ctx, `
SELECT count(*)
FROM agent_memory_extraction_jobs
WHERE source_run_id IN ($1, $2)`,
		submission.Run.ID,
		second.Run.ID,
	).Scan(&jobCount); err != nil {
		t.Fatalf("count extraction jobs: %v", err)
	}
	if jobCount != 2 {
		t.Fatalf("job count = %d, want 2", jobCount)
	}
}

type emptyAgentMemorySearcher struct{}

type emptyAgentStableProfileReader struct{}

type emptyAgentLearningProfileReader struct{}

func (emptyAgentLearningProfileReader) ReadLearningProfile(
	context.Context,
	agentcontext.LearningProfileReadRequest,
) ([]agentcontext.LearningProfileDimension, error) {
	return []agentcontext.LearningProfileDimension{}, nil
}

func (emptyAgentStableProfileReader) ReadStableProfile(
	context.Context,
	agentcontext.StableProfileReadRequest,
) ([]agentcontext.StableProfileMemory, error) {
	return []agentcontext.StableProfileMemory{}, nil
}

func (emptyAgentMemorySearcher) Search(
	context.Context,
	agentcontext.MemorySearchRequest,
) ([]agentcontext.MemorySearchHit, error) {
	return []agentcontext.MemorySearchHit{}, nil
}

type emptyAgentMemoryExtractionBarrier struct{}

func (emptyAgentMemoryExtractionBarrier) Await(
	_ context.Context,
	request agentcontext.MemoryExtractionBarrierRequest,
) (agentcontext.MemoryExtractionBarrierResult, error) {
	return agentcontext.MemoryExtractionBarrierResult{
		PolicyVersion: agentcontext.MemoryExtractionBarrierPolicyV1,
		Cutoff:        request.Cutoff,
		Status:        agentcontext.MemoryExtractionBarrierReady,
	}, nil
}
