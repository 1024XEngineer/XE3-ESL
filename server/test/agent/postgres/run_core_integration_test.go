package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentRunClaimLocksUserBeforeThreadAndRun(t *testing.T) {
	database := newAgentTestDatabase(t)
	service, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := service.CreateThread(ctx, actor)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := repositories.run.CreateInitial(
		ctx,
		actor.UserID,
		thread.ID,
		"run-lock-order",
		"Verify the Run lock order.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create pending Run: %v", err)
	}

	ownerLock, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner lock: %v", err)
	}
	defer func() { _ = ownerLock.Rollback(ctx) }()
	if _, err := ownerLock.Exec(ctx, `
SELECT id FROM users WHERE id = $1 FOR UPDATE`, actor.UserID); err != nil {
		t.Fatalf("lock owner: %v", err)
	}
	type claimResult struct {
		acquired bool
		err      error
	}
	result := make(chan claimResult, 1)
	go func() {
		_, acquired, claimErr := repositories.run.Claim(
			context.Background(),
			actor.UserID,
			submission.Run.ID,
		)
		result <- claimResult{acquired: acquired, err: claimErr}
	}()
	select {
	case early := <-result:
		t.Fatalf("Run claim bypassed owner lock: %#v", early)
	case <-time.After(100 * time.Millisecond):
	}
	if err := ownerLock.Rollback(ctx); err != nil {
		t.Fatalf("release owner lock: %v", err)
	}
	select {
	case claimed := <-result:
		if claimed.err != nil || !claimed.acquired {
			t.Fatalf("claim after owner unlock = %#v", claimed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run claim did not resume after owner unlock")
	}
}

func TestConversationWriteLocksUserBeforeThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	service, _, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := service.CreateThread(ctx, actor)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}

	ownerLock, err := database.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner lock: %v", err)
	}
	defer func() { _ = ownerLock.Rollback(ctx) }()
	if _, err := ownerLock.Exec(ctx, `
SELECT id FROM users WHERE id = $1 FOR UPDATE`, actor.UserID); err != nil {
		t.Fatalf("lock owner: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, appendErr := service.AppendUserMessage(
			context.Background(),
			actor,
			thread.ID,
			"user-lock-order",
			"Verify the Conversation lock order.",
		)
		result <- appendErr
	}()
	select {
	case early := <-result:
		t.Fatalf("Conversation write bypassed owner lock: %v", early)
	case <-time.After(100 * time.Millisecond):
	}
	if err := ownerLock.Rollback(ctx); err != nil {
		t.Fatalf("release owner lock: %v", err)
	}
	select {
	case appendErr := <-result:
		if appendErr != nil {
			t.Fatalf("append after owner unlock: %v", appendErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Conversation write did not resume after owner unlock")
	}
}

func TestDeletingUserCascadesAgentCoreGraph(t *testing.T) {
	database := newAgentTestDatabase(t)
	service, _, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := service.CreateThread(ctx, actor)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	submission, err := repositories.run.CreateInitial(
		ctx,
		actor.UserID,
		thread.ID,
		"account-delete-cascade",
		"Create a complete Agent graph.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	claimed, acquired, err := repositories.run.Claim(
		ctx,
		actor.UserID,
		submission.Run.ID,
	)
	if err != nil || !acquired {
		t.Fatalf("claim Run: acquired=%t err=%v", acquired, err)
	}
	assistantMessageID, err := repositories.run.NewAssistantMessageID()
	if err != nil {
		t.Fatalf("allocate Assistant Message ID: %v", err)
	}
	if _, err := repositories.run.Complete(
		ctx,
		actor.UserID,
		claimed.ID,
		claimed.WorkerLeaseToken,
		agentrun.AssistantOutput{
			ID: assistantMessageID, RunID: claimed.ID,
			Content: "The complete graph may be removed with its owner.",
		},
		successfulTextResult(),
	); err != nil {
		t.Fatalf("complete Run: %v", err)
	}

	audioID := "60000000-0000-4000-8000-000000000091"
	imageID := "60000000-0000-4000-8000-000000000092"
	if _, err := database.pool.Exec(ctx, `
INSERT INTO media_assets (
    id, user_id, kind, upload_request_id, object_key, content_type,
    size_bytes, checksum_sha256, etag, duration_ns, sample_rate,
    status, expires_at
) VALUES (
    $1, $2, 'audio', 'account-delete-voice-upload',
    'audio/v1/media/60000000-0000-4000-8000-000000000091.wav',
    'audio/wav', 128, repeat('a', 64), 'voice-etag', 100000000, 16000,
    'ready', CURRENT_TIMESTAMP + interval '1 hour'
), (
    $3, $2, 'image', 'account-delete-image-upload',
    'image/v1/media/60000000-0000-4000-8000-000000000092.png',
    'image/png', 128, repeat('b', 64), 'image-etag', NULL, NULL,
    'ready', NULL
)`, audioID, actor.UserID, imageID); err != nil {
		t.Fatalf("insert Media assets: %v", err)
	}
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_voice_drafts (
    id, thread_id, status, asr_attempt, version, asr_fencing_token,
    failure_kind, failure_retryable
) VALUES ($1, $2, 'failed', 1, 1, 1, 'provider_unavailable', true)`,
		audioID, thread.ID,
	); err != nil {
		t.Fatalf("insert Voice draft: %v", err)
	}
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_message_attachments (message_id, asset_id, position)
VALUES ($1, $2, 0)`, submission.UserMessage.ID, imageID); err != nil {
		t.Fatalf("attach Image asset: %v", err)
	}

	tag, err := database.pool.Exec(ctx, "DELETE FROM users WHERE id = $1", actor.UserID)
	if err != nil {
		t.Fatalf("delete Agent owner: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted owners = %d, want 1", tag.RowsAffected())
	}
	for _, table := range []string{
		"agent_threads",
		"agent_messages",
		"agent_runs",
		"media_assets",
		"agent_message_attachments",
		"agent_voice_drafts",
	} {
		var count int
		if err := database.pool.QueryRow(
			ctx,
			"SELECT count(*) FROM "+table,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after owner deletion = %d, want 0", table, count)
		}
	}
}

func TestTerminalRunTransitionsCompactExecutionScratchData(t *testing.T) {
	for _, terminal := range []string{"complete", "domain-complete", "fail", "recover"} {
		t.Run(terminal, func(t *testing.T) {
			database := newAgentTestDatabase(t)
			service, _, repositories := newAgentRunServices(
				t,
				database.pool,
				newFixedTextGenerator(successfulTextResult()),
				testRunConfiguration,
			)
			ctx := context.Background()
			actor := testActorA()
			thread, err := service.CreateThread(ctx, actor)
			if err != nil {
				t.Fatalf("create Thread: %v", err)
			}
			submission, err := repositories.run.CreateInitial(
				ctx,
				actor.UserID,
				thread.ID,
				"terminal-trace-"+terminal,
				"Exercise terminal tool trace compaction.",
				testRunConfiguration,
			)
			if err != nil {
				t.Fatalf("create Run: %v", err)
			}
			claimed, acquired, err := repositories.run.Claim(
				ctx,
				actor.UserID,
				submission.Run.ID,
			)
			if err != nil || !acquired {
				t.Fatalf("claim Run: acquired=%t err=%v", acquired, err)
			}
			toolCallID := "70000000-0000-4000-8000-000000000001"
			if _, err := repositories.run.ProposeToolCall(
				ctx,
				agentrun.ToolCall{
					ID: toolCallID, RunID: claimed.ID, OwnerID: actor.UserID,
					ThreadID: thread.ID, Name: "review.search.v2",
					SchemaVersion: "tool-schema-v1",
					Input:         json.RawMessage(`{"private_query":"must disappear"}`),
				},
				claimed.WorkerLeaseToken,
			); err != nil {
				t.Fatalf("propose Tool Call: %v", err)
			}

			expectedActions := 0
			switch terminal {
			case "complete", "domain-complete":
				if _, err := repositories.run.StartToolCall(
					ctx, actor.UserID, claimed.ID, claimed.WorkerLeaseToken,
					toolCallID, "provider-request-secret",
				); err != nil {
					t.Fatalf("start Tool Call: %v", err)
				}
				action, err := clientaction.New(
					"practice.plan.confirm",
					json.RawMessage(`{"practice_plan_id":"80000000-0000-4000-8000-000000000001"}`),
				)
				if err != nil {
					t.Fatalf("new ClientAction: %v", err)
				}
				if _, err := repositories.run.CompleteToolCall(
					ctx, actor.UserID, claimed.ID, claimed.WorkerLeaseToken,
					toolCallID,
					json.RawMessage(`{"private_report":"must disappear"}`),
					[]agentrun.ToolSourceRef{{
						Type: "evaluation_report",
						ID:   "80000000-0000-4000-8000-000000000002",
					}},
					[]clientaction.Action{action},
				); err != nil {
					t.Fatalf("complete Tool Call: %v", err)
				}
				assistantMessageID, err := repositories.run.NewAssistantMessageID()
				if err != nil {
					t.Fatalf("allocate Assistant Message ID: %v", err)
				}
				completion := successfulTextResult()
				if terminal == "domain-complete" {
					completion = agentrun.TextResult{
						CompletionSource: agentrun.CompletionSourceDomain,
						Content:          "Terminal projection completed.",
						DomainToolCallID: toolCallID,
						DomainToolName:   "review.search.v2",
					}
				}
				completed, err := repositories.run.Complete(
					ctx,
					actor.UserID,
					claimed.ID,
					claimed.WorkerLeaseToken,
					agentrun.AssistantOutput{
						ID: assistantMessageID, RunID: claimed.ID,
						Content: "Terminal projection completed.",
					},
					completion,
				)
				if err != nil {
					t.Fatalf("complete Run: %v", err)
				}
				if completed.AssistantMessageID != assistantMessageID {
					t.Fatalf(
						"Assistant Message ID = %q, want %q",
						completed.AssistantMessageID,
						assistantMessageID,
					)
				}
				if terminal == "domain-complete" &&
					(completed.CompletionSource != agentrun.CompletionSourceDomain ||
						completed.DomainToolCallID != toolCallID ||
						completed.DomainToolName != "review.search.v2") {
					t.Fatalf("domain completion = %#v", completed)
				}
				expectedActions = 1
			case "fail":
				if _, err := repositories.run.Fail(
					ctx,
					actor.UserID,
					claimed.ID,
					claimed.WorkerLeaseToken,
					agentrun.FailureInternal,
					true,
				); err != nil {
					t.Fatalf("fail Run: %v", err)
				}
			case "recover":
				if _, err := repositories.run.StartToolCall(
					ctx, actor.UserID, claimed.ID, claimed.WorkerLeaseToken,
					toolCallID, "provider-request-secret",
				); err != nil {
					t.Fatalf("start Tool Call: %v", err)
				}
				if _, err := database.pool.Exec(ctx, `
UPDATE agent_runs
SET lease_expires_at = updated_at + INTERVAL '1 microsecond'
WHERE id = $1`, claimed.ID); err != nil {
					t.Fatalf("expire Run lease: %v", err)
				}
				recovered, err := repositories.run.RecoverInterrupted(ctx)
				if err != nil || recovered != 1 {
					t.Fatalf("recover Run: count=%d err=%v", recovered, err)
				}
			}

			found, err := repositories.run.Find(ctx, actor.UserID, claimed.ID)
			if err != nil || (found.Status != agentrun.StatusCompleted &&
				found.Status != agentrun.StatusFailed) {
				t.Fatalf("find terminal Run: %#v err=%v", found, err)
			}
			actions, err := repositories.run.ListClientActions(
				ctx,
				actor.UserID,
				claimed.ID,
			)
			if err != nil || len(actions) != expectedActions {
				t.Fatalf("ClientActions = %#v err=%v", actions, err)
			}
			assertCompactTerminalToolTrace(t, database.pool, claimed.ID)
		})
	}
}

func assertCompactTerminalToolTrace(
	t *testing.T,
	database *pgxpool.Pool,
	runID string,
) {
	t.Helper()
	var raw []byte
	if err := database.QueryRow(
		context.Background(),
		"SELECT tool_trace FROM agent_runs WHERE id = $1",
		runID,
	).Scan(&raw); err != nil {
		t.Fatalf("read terminal tool trace: %v", err)
	}
	for _, forbidden := range []string{
		"private_query", "private_report", "provider-request-secret",
		"input", "result", "request_id", "proposed_at", "started_at",
		"completed_at", "updated_at",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("terminal tool trace retained %q: %s", forbidden, raw)
		}
	}
	var calls []map[string]any
	if err := json.Unmarshal(raw, &calls); err != nil || len(calls) != 1 {
		t.Fatalf("terminal tool trace = %s err=%v", raw, err)
	}
	allowed := map[string]bool{
		"id": true, "name": true, "schema_version": true, "status": true,
		"error_category": true, "source_refs": true, "client_actions": true,
	}
	for key := range calls[0] {
		if !allowed[key] {
			t.Fatalf("terminal tool trace contains unexpected key %q", key)
		}
	}
}
