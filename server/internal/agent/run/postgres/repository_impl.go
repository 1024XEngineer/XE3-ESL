package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type rowScanner interface {
	Scan(...any) error
}

type storedModelConfiguration struct {
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	MaxInputCharacters int    `json:"max_input_characters"`
}

type storedModelResult struct {
	CompletionID string `json:"completion_id"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	FinishReason string `json:"finish_reason"`
}

type storedDomainResult struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

type storedUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type storedFailure struct {
	Kind      string `json:"kind"`
	Retryable bool   `json:"retryable"`
}

const maxToolTraceBytes = 512 << 10

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

type storedToolCall struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	SchemaVersion string                     `json:"schema_version"`
	Input         json.RawMessage            `json:"input"`
	Status        agentrun.ToolCallStatus    `json:"status"`
	Result        json.RawMessage            `json:"result,omitempty"`
	ErrorCategory string                     `json:"error_category,omitempty"`
	RequestID     string                     `json:"request_id,omitempty"`
	SourceRefs    []agentrun.ToolSourceRef   `json:"source_refs"`
	ClientActions []agentclientaction.Action `json:"client_actions"`
	ProposedAt    time.Time                  `json:"proposed_at"`
	StartedAt     time.Time                  `json:"started_at,omitempty"`
	CompletedAt   time.Time                  `json:"completed_at,omitempty"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

// storedTerminalToolCall is the durable projection kept after a Run stops.
// Provider inputs and capability results are execution scratch data and are
// deliberately removed; conversation history only consumes ClientActions.
type storedTerminalToolCall struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	SchemaVersion string                     `json:"schema_version"`
	Status        agentrun.ToolCallStatus    `json:"status"`
	ErrorCategory string                     `json:"error_category,omitempty"`
	SourceRefs    []agentrun.ToolSourceRef   `json:"source_refs"`
	ClientActions []agentclientaction.Action `json:"client_actions"`
}

// terminalToolTraceProjectionSQL is the single durable boundary for tool
// execution scratch data. Every terminal transition keeps only the fields
// consumed by conversation ClientActions and bounded source/status metadata.
const terminalToolTraceProjectionSQL = `(
    SELECT COALESCE(
        jsonb_agg(
            jsonb_strip_nulls(jsonb_build_object(
                'id', item.value->'id',
                'name', item.value->'name',
                'schema_version', item.value->'schema_version',
                'status', item.value->'status',
                'error_category', NULLIF(item.value->>'error_category', ''),
                'source_refs', item.value->'source_refs',
                'client_actions', item.value->'client_actions'
            ))
            ORDER BY item.position
        ),
        '[]'::jsonb
    )
    FROM jsonb_array_elements(runs.tool_trace)
        WITH ORDINALITY AS item(value, position)
)`

func (r *Repository) CreateInitial(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
	configuration agentrun.Configuration,
) (agentrun.Submission, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	defer rollback(tx)

	nextSequence, err := lockOwnedThread(ctx, tx, ownerID, threadID)
	if err != nil {
		return agentrun.Submission{}, err
	}
	message, found, err := findInputMessageByClientIDInTransaction(
		ctx, tx, ownerID, threadID, clientMessageID,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if found {
		if message.Content != content ||
			message.Role != conversation.MessageRoleUser ||
			message.Modality != conversation.MessageModalityText {
			return agentrun.Submission{}, agentrun.ErrIdempotencyConflict
		}
		existing, exists, findErr := findInitialRunByInput(
			ctx, tx, ownerID, threadID, message.ID,
		)
		if findErr != nil {
			return agentrun.Submission{}, findErr
		}
		if !exists {
			return agentrun.Submission{}, agentrun.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return agentrun.Submission{}, agentrun.ErrRepository
		}
		return agentrun.Submission{
			Run: existing, UserMessage: message, Created: false,
		}, nil
	}

	messageID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, client_message_id, modality, content
) VALUES ($1, $2, $3, 'user', $4, 'text', $5)`,
		messageID, threadID, nextSequence, clientMessageID, content,
	); err != nil {
		return agentrun.Submission{}, mapRunPostgresError(err)
	}
	message, err = conversationpostgres.FindMessageInTransaction(
		ctx, tx, ownerID, threadID, messageID,
	)
	if err != nil {
		return agentrun.Submission{}, mapConversationError(err)
	}
	if err := advanceThreadSequence(ctx, tx, ownerID, threadID, content); err != nil {
		return agentrun.Submission{}, err
	}

	runID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	run, err := insertPendingRun(
		ctx, tx, runID, ownerID, threadID, message.ID, 1, "", "", configuration,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	return agentrun.Submission{Run: run, UserMessage: message, Created: true}, nil
}

func (r *Repository) CreateRetry(
	ctx context.Context,
	ownerID string,
	runID string,
	retryClientID string,
	configuration agentrun.Configuration,
) (agentrun.Retry, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	defer rollback(tx)
	original, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Retry{}, err
	}
	existing, found, err := findRunByRetryClientID(
		ctx, tx, ownerID, original.ThreadID, retryClientID,
	)
	if err != nil {
		return agentrun.Retry{}, err
	}
	if found {
		if existing.RetryOfRunID != original.ID ||
			existing.InputMessageID != original.InputMessageID {
			return agentrun.Retry{}, agentrun.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return agentrun.Retry{}, agentrun.ErrRepository
		}
		return agentrun.Retry{Run: existing, Created: false}, nil
	}
	if original.Status != agentrun.StatusFailed || !original.FailureRetryable {
		return agentrun.Retry{}, agentrun.ErrConflict
	}
	var nextAttempt int
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(MAX(attempt_no), 0) + 1
FROM agent_runs
WHERE thread_id = $1 AND input_message_id = $2`,
		original.ThreadID, original.InputMessageID,
	).Scan(&nextAttempt); err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	newRunID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	run, err := insertPendingRun(
		ctx, tx, newRunID, ownerID, original.ThreadID,
		original.InputMessageID, nextAttempt, original.ID, retryClientID,
		configuration,
	)
	if err != nil {
		return agentrun.Retry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	return agentrun.Retry{Run: run, Created: true}, nil
}

func (r *Repository) Claim(
	ctx context.Context,
	ownerID string,
	runID string,
) (agentrun.Run, bool, error) {
	leaseToken, err := r.ids.NewID()
	if err != nil {
		return agentrun.Run{}, false, agentrun.ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Run{}, false, agentrun.ErrRepository
	}
	defer rollback(tx)
	if _, err := findRunForUpdate(ctx, tx, ownerID, runID); err != nil {
		return agentrun.Run{}, false, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs
SET status = 'running', phase = 'context', started_at = CURRENT_TIMESTAMP,
    lease_token = $2,
    lease_expires_at = CURRENT_TIMESTAMP + INTERVAL '10 minutes',
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND status = 'pending'`, runID, leaseToken)
	if err != nil {
		return agentrun.Run{}, false, mapRunPostgresError(err)
	}
	claimed := tag.RowsAffected() == 1
	run, err := findRun(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Run{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Run{}, false, agentrun.ErrRepository
	}
	return run, claimed, nil
}

func (r *Repository) Find(
	ctx context.Context,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	return findRun(ctx, r.database, ownerID, runID)
}

func (r *Repository) SaveContextSnapshot(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	manifest agentcontext.Manifest,
) error {
	if !manifest.Valid() || manifest.RunID != runID || manifest.OwnerID != ownerID {
		return agentrun.ErrInvalidRequest
	}
	snapshot, err := json.Marshal(manifest)
	if err != nil {
		return agentrun.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.ErrRepository
	}
	defer rollback(tx)
	if _, err := findRunForUpdate(ctx, tx, ownerID, runID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs
SET context_snapshot = $3::jsonb, phase = 'model', updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > CURRENT_TIMESTAMP`,
		runID, leaseToken, snapshot,
	)
	if err != nil {
		return mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.ErrRepository
	}
	return nil
}

func (r *Repository) ProposeToolCall(
	ctx context.Context,
	record agentrun.ToolCall,
	leaseToken string,
) (agentrun.ToolCall, error) {
	if !validToolCallRecordIdentity(record) || record.Name == "" ||
		record.SchemaVersion == "" || !jsonObject(record.Input) {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	return r.mutateToolTrace(ctx, record.OwnerID, record.RunID, leaseToken,
		func(calls []storedToolCall, now time.Time) ([]storedToolCall, int, error) {
			for index := range calls {
				if calls[index].ID != record.ID {
					continue
				}
				if calls[index].Name != record.Name ||
					calls[index].SchemaVersion != record.SchemaVersion ||
					!bytes.Equal(calls[index].Input, record.Input) {
					return nil, 0, agentrun.ErrConflict
				}
				return calls, index, nil
			}
			calls = append(calls, storedToolCall{
				ID: record.ID, Name: record.Name,
				SchemaVersion: record.SchemaVersion,
				Input:         append(json.RawMessage(nil), record.Input...),
				Status:        agentrun.ToolCallProposed,
				SourceRefs:    []agentrun.ToolSourceRef{},
				ClientActions: []agentclientaction.Action{},
				ProposedAt:    now, UpdatedAt: now,
			})
			return calls, len(calls) - 1, nil
		})
}

func (r *Repository) StartToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	toolCallID string,
	requestID string,
) (agentrun.ToolCall, error) {
	if requestID == "" {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	return r.mutateToolTrace(ctx, ownerID, runID, leaseToken,
		func(calls []storedToolCall, now time.Time) ([]storedToolCall, int, error) {
			index := storedToolIndex(calls, toolCallID)
			if index < 0 {
				return nil, 0, agentrun.ErrConflict
			}
			if calls[index].Status == agentrun.ToolCallRunning &&
				calls[index].RequestID == requestID {
				return calls, index, nil
			}
			if calls[index].Status != agentrun.ToolCallProposed {
				return nil, 0, agentrun.ErrConflict
			}
			calls[index].Status = agentrun.ToolCallRunning
			calls[index].RequestID = requestID
			calls[index].StartedAt = now
			calls[index].UpdatedAt = now
			return calls, index, nil
		})
}

func (r *Repository) CompleteToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	toolCallID string,
	result json.RawMessage,
	sourceRefs []agentrun.ToolSourceRef,
	clientActions []agentclientaction.Action,
) (agentrun.ToolCall, error) {
	if !jsonObject(result) || !agentrun.ValidToolSourceRefs(sourceRefs) ||
		agentclientaction.ValidateItems(clientActions) != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	return r.mutateToolTrace(ctx, ownerID, runID, leaseToken,
		func(calls []storedToolCall, now time.Time) ([]storedToolCall, int, error) {
			index := storedToolIndex(calls, toolCallID)
			if index < 0 || calls[index].Status != agentrun.ToolCallRunning {
				return nil, 0, agentrun.ErrConflict
			}
			actionCount := len(clientActions)
			for callIndex := range calls {
				if callIndex != index {
					actionCount += len(calls[callIndex].ClientActions)
				}
			}
			if actionCount > agentclientaction.MaxItems {
				return nil, 0, agentrun.ErrInvalidRequest
			}
			calls[index].Status = agentrun.ToolCallSucceeded
			calls[index].Result = append(json.RawMessage(nil), result...)
			calls[index].SourceRefs = append([]agentrun.ToolSourceRef(nil), sourceRefs...)
			calls[index].ClientActions = agentclientaction.CloneItems(clientActions)
			calls[index].CompletedAt = now
			calls[index].UpdatedAt = now
			return calls, index, nil
		})
}

func (r *Repository) FailToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	toolCallID string,
	status agentrun.ToolCallStatus,
	errorCategory string,
) (agentrun.ToolCall, error) {
	if (status != agentrun.ToolCallFailed && status != agentrun.ToolCallRejected) ||
		errorCategory == "" {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	return r.mutateToolTrace(ctx, ownerID, runID, leaseToken,
		func(calls []storedToolCall, now time.Time) ([]storedToolCall, int, error) {
			index := storedToolIndex(calls, toolCallID)
			if index < 0 || (calls[index].Status != agentrun.ToolCallProposed &&
				calls[index].Status != agentrun.ToolCallRunning) {
				return nil, 0, agentrun.ErrConflict
			}
			calls[index].Status = status
			calls[index].ErrorCategory = errorCategory
			if calls[index].StartedAt.IsZero() {
				calls[index].StartedAt = now
			}
			calls[index].CompletedAt = now
			calls[index].UpdatedAt = now
			return calls, index, nil
		})
}

func (r *Repository) ListClientActions(
	ctx context.Context,
	ownerID string,
	runID string,
) ([]agentclientaction.Action, error) {
	var status string
	var raw []byte
	err := r.database.QueryRow(ctx, `
SELECT runs.status, runs.tool_trace
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE runs.id = $1 AND threads.user_id = $2 AND threads.deleted_at IS NULL`,
		runID, ownerID,
	).Scan(&status, &raw)
	if err != nil {
		return nil, mapRunPostgresError(err)
	}
	if agentrun.Status(status) != agentrun.StatusCompleted {
		return []agentclientaction.Action{}, nil
	}
	stored, err := decodeTerminalToolTrace(raw)
	if err != nil {
		return nil, err
	}
	result := make([]agentclientaction.Action, 0, agentclientaction.MaxItems)
	for _, call := range stored {
		if call.Status == agentrun.ToolCallSucceeded {
			result = append(result, call.ClientActions...)
		}
	}
	if err := agentclientaction.ValidateItems(result); err != nil {
		return nil, agentrun.ErrRepository
	}
	return agentclientaction.CloneItems(result), nil
}

func (r *Repository) mutateToolTrace(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	mutate func([]storedToolCall, time.Time) ([]storedToolCall, int, error),
) (agentrun.ToolCall, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.ToolCall{}, agentrun.ErrRepository
	}
	defer rollback(tx)
	locked, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		if errors.Is(err, agentrun.ErrNotFound) {
			return agentrun.ToolCall{}, agentrun.ErrConflict
		}
		return agentrun.ToolCall{}, err
	}
	if locked.Status != agentrun.StatusRunning || locked.WorkerLeaseToken != leaseToken {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	var raw []byte
	err = tx.QueryRow(ctx, `
SELECT tool_trace
FROM agent_runs
WHERE id = $1`, runID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.ToolCall{}, mapRunPostgresError(err)
	}
	calls, err := decodeActiveToolTrace(raw)
	if err != nil {
		return agentrun.ToolCall{}, err
	}
	calls, selected, err := mutate(calls, time.Now().UTC())
	if err != nil {
		return agentrun.ToolCall{}, err
	}
	encoded, err := json.Marshal(calls)
	if err != nil || len(encoded) > maxToolTraceBytes {
		return agentrun.ToolCall{}, agentrun.ErrRepository
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs
SET tool_trace = $3::jsonb, phase = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND lease_token = $2 AND status = 'running'
  AND lease_expires_at > CURRENT_TIMESTAMP`,
		runID, leaseToken, encoded, toolTracePhase(calls, selected),
	)
	if err != nil {
		return agentrun.ToolCall{}, mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.ToolCall{}, agentrun.ErrRepository
	}
	return convertStoredToolCall(calls[selected], ownerID, locked.ThreadID, runID)
}

func toolTracePhase(calls []storedToolCall, selected int) string {
	if selected >= 0 && selected < len(calls) &&
		(calls[selected].Status == agentrun.ToolCallProposed ||
			calls[selected].Status == agentrun.ToolCallRunning) {
		return "tool"
	}
	return "model"
}

func (r *Repository) Complete(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	output agentrun.AssistantOutput,
	result agentrun.TextResult,
) (agentrun.Run, error) {
	if !agentrun.ValidUUID(output.ID) || output.RunID != runID ||
		!conversation.ValidMessageContent(output.Content) {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	defer rollback(tx)
	run, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Run{}, err
	}
	if run.Status == agentrun.StatusCompleted {
		if err := tx.Commit(ctx); err != nil {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		return run, nil
	}
	if run.Status != agentrun.StatusRunning || run.WorkerLeaseToken != leaseToken {
		return agentrun.Run{}, agentrun.ErrConflict
	}
	nextSequence, err := lockOwnedThread(ctx, tx, ownerID, run.ThreadID)
	if err != nil {
		return agentrun.Run{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, produced_by_run_id, modality, content
) VALUES ($1, $2, $3, 'assistant', $4, 'text', $5)`,
		output.ID, run.ThreadID, nextSequence, run.ID, output.Content,
	); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	var modelResult []byte
	var usage []byte
	var domainResult []byte
	if result.CompletionSource == agentrun.CompletionSourceDomain {
		domainResult, err = json.Marshal(storedDomainResult{
			ToolCallID: result.DomainToolCallID,
			ToolName:   result.DomainToolName,
		})
	} else {
		modelResult, err = json.Marshal(storedModelResult{
			CompletionID: result.ID, Provider: result.Provider,
			Model: result.Model, FinishReason: result.FinishReason,
		})
		if err == nil {
			usage, err = json.Marshal(storedUsage{
				InputTokens:  result.Usage.InputTokens,
				OutputTokens: result.Usage.OutputTokens,
				TotalTokens:  result.Usage.TotalTokens,
			})
		}
	}
	if err != nil {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs AS runs
SET status = 'completed', phase = 'completed',
    model_result = $3::jsonb, usage = $4::jsonb,
    domain_result = $5::jsonb, error = NULL,
    tool_trace = `+terminalToolTraceProjectionSQL+`,
    lease_token = NULL, lease_expires_at = NULL,
    completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND status = 'running' AND lease_token = $2
  AND lease_expires_at > CURRENT_TIMESTAMP`,
		runID, leaseToken, nullableJSON(modelResult), nullableJSON(usage),
		nullableJSON(domainResult),
	)
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.Run{}, agentrun.ErrConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond'),
    summary_target_sequence = GREATEST(
        COALESCE(summary_target_sequence, 0), $3
    ),
    summary_attempt_count = CASE
        WHEN summary_error IS NOT NULL THEN 0
        ELSE summary_attempt_count
    END,
    summary_available_at = CASE
        WHEN summary_error IS NOT NULL THEN CURRENT_TIMESTAMP
        ELSE summary_available_at
    END,
    summary_error = NULL
WHERE id = $1 AND user_id = $2`, run.ThreadID, ownerID, nextSequence); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	completed, err := findRun(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	return completed, nil
}

func (r *Repository) NewAssistantMessageID() (string, error) {
	return r.ids.NewID()
}

func (r *Repository) Fail(
	ctx context.Context,
	ownerID string,
	runID string,
	leaseToken string,
	failureKind string,
	retryable bool,
) (agentrun.Run, error) {
	failure, err := json.Marshal(storedFailure{Kind: failureKind, Retryable: retryable})
	if err != nil {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	defer rollback(tx)
	current, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Run{}, err
	}
	if current.Status == agentrun.StatusFailed {
		if err := tx.Commit(ctx); err != nil {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		return current, nil
	}
	if current.Status != agentrun.StatusRunning ||
		current.WorkerLeaseToken != leaseToken {
		return agentrun.Run{}, agentrun.ErrConflict
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs AS runs
SET status = 'failed', phase = 'failed', error = $3::jsonb,
    model_result = NULL, usage = NULL,
    tool_trace = `+terminalToolTraceProjectionSQL+`,
    lease_token = NULL, lease_expires_at = NULL,
    completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE runs.id = $1 AND runs.status = 'running'
  AND runs.lease_token = $2
  AND runs.lease_expires_at > CURRENT_TIMESTAMP`,
		runID, leaseToken, failure,
	)
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.Run{}, agentrun.ErrConflict
	}
	failed, err := findRun(ctx, tx, ownerID, runID)
	if err != nil {
		return agentrun.Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	return failed, nil
}

func (r *Repository) RecoverInterrupted(ctx context.Context) (int64, error) {
	failure, _ := json.Marshal(storedFailure{
		Kind: agentrun.FailureInterrupted, Retryable: true,
	})
	var recovered int64
	for {
		changed, found, err := r.recoverOneInterrupted(ctx, failure)
		if err != nil {
			return recovered, err
		}
		if !found {
			return recovered, nil
		}
		if changed {
			recovered++
		}
	}
}

func (r *Repository) recoverOneInterrupted(
	ctx context.Context,
	failure []byte,
) (bool, bool, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return false, false, agentrun.ErrRepository
	}
	defer rollback(tx)
	var ownerID string
	var runID string
	err = tx.QueryRow(ctx, `
SELECT threads.user_id::text, runs.id::text
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
INNER JOIN users AS owner ON owner.id = threads.user_id
WHERE runs.status = 'running'
  AND runs.lease_expires_at <= CURRENT_TIMESTAMP
  AND threads.deleted_at IS NULL
  AND owner.status = 'active'
ORDER BY runs.lease_expires_at, runs.id
LIMIT 1`).Scan(&ownerID, &runID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return false, false, agentrun.ErrRepository
		}
		return false, false, nil
	}
	if err != nil {
		return false, false, mapRunPostgresError(err)
	}
	current, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return false, true, err
	}
	if current.Status != agentrun.StatusRunning {
		if err := tx.Commit(ctx); err != nil {
			return false, true, agentrun.ErrRepository
		}
		return false, true, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE agent_runs AS runs
SET status = 'failed', phase = 'failed', error = $1::jsonb,
    model_result = NULL, usage = NULL,
    tool_trace = `+terminalToolTraceProjectionSQL+`,
    lease_token = NULL, lease_expires_at = NULL,
    completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE runs.id = $2
  AND runs.status = 'running'
  AND runs.lease_expires_at <= CURRENT_TIMESTAMP`, failure, runID)
	if err != nil {
		return false, true, mapRunPostgresError(err)
	}
	changed := tag.RowsAffected() == 1
	if err := tx.Commit(ctx); err != nil {
		return false, true, agentrun.ErrRepository
	}
	return changed, true, nil
}

func insertPendingRun(
	ctx context.Context,
	tx pgx.Tx,
	runID string,
	ownerID string,
	threadID string,
	inputMessageID string,
	attempt int,
	retryOfRunID string,
	retryClientID string,
	configuration agentrun.Configuration,
) (agentrun.Run, error) {
	if !configuration.Valid() {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	modelConfiguration, err := json.Marshal(storedModelConfiguration{
		Provider:           configuration.Provider,
		Model:              configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
		MaxInputCharacters: configuration.MaxInputCharacters,
	})
	if err != nil {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	var retryOf any
	var retryClient any
	if retryOfRunID != "" {
		retryOf = retryOfRunID
		retryClient = retryClientID
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO agent_runs (
    id, thread_id, input_message_id, attempt_no, retry_of_run_id,
    retry_client_id, status, phase, model_configuration, tool_trace
)
SELECT $1, threads.id, messages.id, $5, $6, $7,
       'pending', 'queued', $8::jsonb, '[]'::jsonb
FROM agent_threads AS threads
INNER JOIN users AS owner ON owner.id = threads.user_id
INNER JOIN agent_messages AS messages
    ON messages.id = $4 AND messages.thread_id = threads.id
WHERE threads.id = $3 AND threads.user_id = $2
  AND threads.deleted_at IS NULL AND owner.status = 'active'`,
		runID, ownerID, threadID, inputMessageID, attempt,
		retryOf, retryClient, modelConfiguration,
	)
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.Run{}, agentrun.ErrNotFound
	}
	return findRun(ctx, tx, ownerID, runID)
}

func findInitialRunByInput(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	inputMessageID string,
) (agentrun.Run, bool, error) {
	return findOptionalRun(ctx, tx, ownerID, `
SELECT `+runSelectColumns+`
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE threads.user_id = $1 AND runs.thread_id = $2
  AND runs.input_message_id = $3 AND runs.attempt_no = 1`,
		ownerID, threadID, inputMessageID)
}

func findRunByRetryClientID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	retryClientID string,
) (agentrun.Run, bool, error) {
	return findOptionalRun(ctx, tx, ownerID, `
SELECT `+runSelectColumns+`
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE threads.user_id = $1 AND runs.thread_id = $2
  AND runs.retry_client_id = $3`, ownerID, threadID, retryClientID)
}

func findOptionalRun(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	query string,
	arguments ...any,
) (agentrun.Run, bool, error) {
	run, err := scanRun(tx.QueryRow(ctx, query, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.Run{}, false, nil
	}
	if err != nil {
		return agentrun.Run{}, false, mapRunPostgresError(err)
	}
	if run.OwnerID != ownerID {
		return agentrun.Run{}, false, agentrun.ErrNotFound
	}
	return run, true, nil
}

func findRunForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return agentrun.Run{}, err
	}
	var threadID string
	if err := tx.QueryRow(ctx, `
SELECT runs.thread_id::text
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE runs.id = $1 AND threads.user_id = $2
  AND threads.deleted_at IS NULL`, runID, ownerID).Scan(&threadID); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if _, err := lockOwnedThreadAfterOwner(ctx, tx, ownerID, threadID); err != nil {
		return agentrun.Run{}, err
	}
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs AS runs
INNER JOIN agent_threads AS threads ON threads.id = runs.thread_id
WHERE runs.id = $1 AND threads.user_id = $2
	AND runs.thread_id = $3
FOR UPDATE OF runs`, runID, ownerID, threadID))
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	return run, nil
}

const runSelectColumns = `
    runs.id::text,
    threads.user_id::text,
    runs.thread_id::text,
    runs.input_message_id::text,
    runs.attempt_no,
    COALESCE(runs.retry_of_run_id::text, ''),
    COALESCE(runs.retry_client_id, ''),
    runs.status,
    runs.phase,
    runs.model_configuration,
    COALESCE(runs.lease_token::text, ''),
    runs.lease_expires_at,
    COALESCE((
        SELECT message.id::text
        FROM agent_messages AS message
        WHERE message.produced_by_run_id = runs.id
    ), ''),
    runs.model_result,
    runs.usage,
    runs.domain_result,
    runs.error,
    runs.context_snapshot,
    runs.tool_trace,
    runs.created_at,
    runs.started_at,
    runs.completed_at,
    runs.updated_at`

func scanRun(row rowScanner) (agentrun.Run, error) {
	var result agentrun.Run
	var status string
	var modelConfigurationJSON []byte
	var modelResultJSON []byte
	var usageJSON []byte
	var domainResultJSON []byte
	var failureJSON []byte
	var contextSnapshotJSON []byte
	var toolTraceJSON []byte
	var leaseExpiresAt pgtype.Timestamptz
	var startedAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	if err := row.Scan(
		&result.ID, &result.OwnerID, &result.ThreadID, &result.InputMessageID,
		&result.Attempt, &result.RetryOfRunID, &result.RetryClientID,
		&status, &result.Phase, &modelConfigurationJSON,
		&result.WorkerLeaseToken, &leaseExpiresAt, &result.AssistantMessageID,
		&modelResultJSON, &usageJSON, &domainResultJSON, &failureJSON, &contextSnapshotJSON,
		&toolTraceJSON, &result.CreatedAt, &startedAt, &completedAt,
		&result.UpdatedAt,
	); err != nil {
		return agentrun.Run{}, err
	}
	result.Status = agentrun.Status(status)
	var configuration storedModelConfiguration
	if strictJSON(modelConfigurationJSON, &configuration) != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	publicConfiguration := agentrun.Configuration{
		Provider: configuration.Provider, Model: configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
		MaxInputCharacters: configuration.MaxInputCharacters,
	}
	if !publicConfiguration.Valid() {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	result.RequestedProvider = configuration.Provider
	result.RequestedModel = configuration.Model
	result.MaxOutputTokens = configuration.MaxOutputTokens
	result.MaxInputCharacters = configuration.MaxInputCharacters
	if result.Status == agentrun.StatusPending ||
		result.Status == agentrun.StatusRunning {
		if _, err := decodeActiveToolTrace(toolTraceJSON); err != nil {
			return agentrun.Run{}, err
		}
	} else if _, err := decodeTerminalToolTrace(toolTraceJSON); err != nil {
		return agentrun.Run{}, err
	}
	if len(contextSnapshotJSON) > 0 {
		var manifest agentcontext.Manifest
		if strictJSON(contextSnapshotJSON, &manifest) != nil || !manifest.Valid() ||
			manifest.RunID != result.ID || manifest.OwnerID != result.OwnerID ||
			manifest.ThreadID != result.ThreadID ||
			manifest.InputMessageID != result.InputMessageID {
			return agentrun.Run{}, agentrun.ErrRepository
		}
	}
	if len(modelResultJSON) > 0 {
		var stored storedModelResult
		if strictJSON(modelResultJSON, &stored) != nil {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		result.ProviderCompletionID = stored.CompletionID
		result.ProviderModel = stored.Model
		result.FinishReason = stored.FinishReason
		result.CompletionSource = agentrun.CompletionSourceModel
	}
	if len(domainResultJSON) > 0 {
		var stored storedDomainResult
		if strictJSON(domainResultJSON, &stored) != nil ||
			!agentrun.ValidOpaqueID(stored.ToolCallID) ||
			!agentrun.ValidOpaqueID(stored.ToolName) {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		result.CompletionSource = agentrun.CompletionSourceDomain
		result.DomainToolCallID = stored.ToolCallID
		result.DomainToolName = stored.ToolName
	}
	if len(usageJSON) > 0 {
		var stored storedUsage
		if strictJSON(usageJSON, &stored) != nil || stored.InputTokens < 0 ||
			stored.OutputTokens < 0 || stored.TotalTokens < 0 {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		result.Usage = agentrun.TokenUsage{
			InputTokens: stored.InputTokens, OutputTokens: stored.OutputTokens,
			TotalTokens: stored.TotalTokens,
		}
	}
	if len(failureJSON) > 0 {
		var stored storedFailure
		if strictJSON(failureJSON, &stored) != nil || stored.Kind == "" {
			return agentrun.Run{}, agentrun.ErrRepository
		}
		result.FailureKind = stored.Kind
		result.FailureRetryable = stored.Retryable
	}
	if leaseExpiresAt.Valid {
		result.WorkerLeaseExpiresAt = leaseExpiresAt.Time.UTC()
	}
	if startedAt.Valid {
		result.StartedAt = startedAt.Time.UTC()
	}
	if completedAt.Valid {
		result.CompletedAt = completedAt.Time.UTC()
	}
	if !validStoredRun(
		result,
		modelResultJSON,
		usageJSON,
		domainResultJSON,
		failureJSON,
	) {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	return result, nil
}

func validStoredRun(
	run agentrun.Run,
	modelResult []byte,
	usage []byte,
	domainResult []byte,
	failure []byte,
) bool {
	if !run.Status.Valid() || run.Attempt < 1 {
		return false
	}
	switch run.Status {
	case agentrun.StatusPending:
		return run.Phase == "queued" && run.StartedAt.IsZero() &&
			run.CompletedAt.IsZero() && run.WorkerLeaseToken == "" &&
			len(modelResult) == 0 && len(usage) == 0 &&
			len(domainResult) == 0 && len(failure) == 0
	case agentrun.StatusRunning:
		return (run.Phase == "context" || run.Phase == "model" || run.Phase == "tool") &&
			!run.StartedAt.IsZero() && run.CompletedAt.IsZero() &&
			run.WorkerLeaseToken != "" && !run.WorkerLeaseExpiresAt.IsZero() &&
			len(modelResult) == 0 && len(usage) == 0 &&
			len(domainResult) == 0 && len(failure) == 0
	case agentrun.StatusCompleted:
		return run.Phase == "completed" && !run.StartedAt.IsZero() &&
			!run.CompletedAt.IsZero() && run.AssistantMessageID != "" &&
			run.WorkerLeaseToken == "" && len(failure) == 0 &&
			((len(modelResult) > 0 && len(usage) > 0 && len(domainResult) == 0) ||
				(len(modelResult) == 0 && len(usage) == 0 && len(domainResult) > 0))
	case agentrun.StatusFailed:
		return run.Phase == "failed" && !run.StartedAt.IsZero() &&
			!run.CompletedAt.IsZero() && run.AssistantMessageID == "" &&
			run.WorkerLeaseToken == "" && len(modelResult) == 0 &&
			len(usage) == 0 && len(domainResult) == 0 && len(failure) > 0
	default:
		return false
	}
}

func decodeActiveToolTrace(raw []byte) ([]storedToolCall, error) {
	if len(raw) == 0 || len(raw) > maxToolTraceBytes {
		return nil, agentrun.ErrRepository
	}
	var calls []storedToolCall
	if strictJSON(raw, &calls) != nil || calls == nil ||
		len(calls) > agentrun.MaxToolCallsPerRun {
		return nil, agentrun.ErrRepository
	}
	seen := make(map[string]struct{}, len(calls))
	actions := make([]agentclientaction.Action, 0, agentclientaction.MaxItems)
	for _, call := range calls {
		if _, duplicate := seen[call.ID]; duplicate || !validStoredToolCall(call) {
			return nil, agentrun.ErrRepository
		}
		seen[call.ID] = struct{}{}
		actions = append(actions, call.ClientActions...)
	}
	if agentclientaction.ValidateItems(actions) != nil {
		return nil, agentrun.ErrRepository
	}
	return calls, nil
}

func decodeTerminalToolTrace(raw []byte) ([]storedTerminalToolCall, error) {
	if len(raw) == 0 || len(raw) > maxToolTraceBytes {
		return nil, agentrun.ErrRepository
	}
	var calls []storedTerminalToolCall
	if strictJSON(raw, &calls) != nil || calls == nil ||
		len(calls) > agentrun.MaxToolCallsPerRun {
		return nil, agentrun.ErrRepository
	}
	seen := make(map[string]struct{}, len(calls))
	actions := make([]agentclientaction.Action, 0, agentclientaction.MaxItems)
	for _, call := range calls {
		if _, duplicate := seen[call.ID]; duplicate ||
			!validStoredTerminalToolCall(call) {
			return nil, agentrun.ErrRepository
		}
		seen[call.ID] = struct{}{}
		actions = append(actions, call.ClientActions...)
	}
	if agentclientaction.ValidateItems(actions) != nil {
		return nil, agentrun.ErrRepository
	}
	return calls, nil
}

func validStoredTerminalToolCall(call storedTerminalToolCall) bool {
	if !agentrun.ValidOpaqueID(call.ID) ||
		!agentrun.ValidOpaqueID(call.Name) ||
		!agentrun.ValidOpaqueID(call.SchemaVersion) || !call.Status.Valid() ||
		!agentrun.ValidToolSourceRefs(call.SourceRefs) ||
		agentclientaction.ValidateItems(call.ClientActions) != nil {
		return false
	}
	if call.Status == agentrun.ToolCallFailed ||
		call.Status == agentrun.ToolCallRejected {
		return call.ErrorCategory != "" && len(call.ClientActions) == 0
	}
	return call.ErrorCategory == "" &&
		(call.Status == agentrun.ToolCallSucceeded ||
			call.Status == agentrun.ToolCallProposed ||
			call.Status == agentrun.ToolCallRunning)
}

func validStoredToolCall(call storedToolCall) bool {
	if !agentrun.ValidOpaqueID(call.ID) || !agentrun.ValidOpaqueID(call.Name) ||
		call.SchemaVersion == "" || !jsonObject(call.Input) ||
		!call.Status.Valid() || call.ProposedAt.IsZero() || call.UpdatedAt.IsZero() ||
		!agentrun.ValidToolSourceRefs(call.SourceRefs) ||
		agentclientaction.ValidateItems(call.ClientActions) != nil {
		return false
	}
	switch call.Status {
	case agentrun.ToolCallProposed:
		return call.RequestID == "" && call.Result == nil &&
			call.ErrorCategory == "" && call.StartedAt.IsZero() &&
			call.CompletedAt.IsZero()
	case agentrun.ToolCallRunning:
		return call.RequestID != "" && call.Result == nil &&
			call.ErrorCategory == "" && !call.StartedAt.IsZero() &&
			call.CompletedAt.IsZero()
	case agentrun.ToolCallSucceeded:
		return call.RequestID != "" && jsonObject(call.Result) &&
			call.ErrorCategory == "" && !call.StartedAt.IsZero() &&
			!call.CompletedAt.IsZero()
	case agentrun.ToolCallFailed, agentrun.ToolCallRejected:
		return call.Result == nil && call.ErrorCategory != "" &&
			!call.StartedAt.IsZero() && !call.CompletedAt.IsZero()
	default:
		return false
	}
}

func convertStoredToolCall(
	stored storedToolCall,
	ownerID string,
	threadID string,
	runID string,
) (agentrun.ToolCall, error) {
	if !validStoredToolCall(stored) {
		return agentrun.ToolCall{}, agentrun.ErrRepository
	}
	return agentrun.ToolCall{
		ID: stored.ID, RunID: runID, OwnerID: ownerID, ThreadID: threadID,
		Name: stored.Name, SchemaVersion: stored.SchemaVersion,
		Input: append(json.RawMessage(nil), stored.Input...), Status: stored.Status,
		Result:        append(json.RawMessage(nil), stored.Result...),
		ErrorCategory: stored.ErrorCategory, RequestID: stored.RequestID,
		SourceRefs:    append([]agentrun.ToolSourceRef(nil), stored.SourceRefs...),
		ClientActions: agentclientaction.CloneItems(stored.ClientActions),
		ProposedAt:    stored.ProposedAt, StartedAt: stored.StartedAt,
		CompletedAt: stored.CompletedAt, UpdatedAt: stored.UpdatedAt,
	}, nil
}

func storedToolIndex(calls []storedToolCall, id string) int {
	for index := range calls {
		if calls[index].ID == id {
			return index
		}
	}
	return -1
}

func strictJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("agent run: persisted JSON contains trailing data")
	}
	return nil
}

func jsonObject(raw []byte) bool {
	if !json.Valid(raw) {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validToolCallRecordIdentity(record agentrun.ToolCall) bool {
	return agentrun.ValidOpaqueID(record.ID) && agentrun.ValidUUID(record.RunID) &&
		agentrun.ValidUUID(record.OwnerID) && agentrun.ValidUUID(record.ThreadID)
}

func lockOwnedThread(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
) (int64, error) {
	if err := lockActiveOwner(ctx, tx, ownerID); err != nil {
		return 0, err
	}
	return lockOwnedThreadAfterOwner(ctx, tx, ownerID, threadID)
}

func lockActiveOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
) error {
	var status string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM users
WHERE id = $1
FOR SHARE`, ownerID).Scan(&status); err != nil {
		return mapRunPostgresError(err)
	}
	if status != "active" {
		return agentrun.ErrNotFound
	}
	return nil
}

func lockOwnedThreadAfterOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
) (int64, error) {
	var nextSequence int64
	err := tx.QueryRow(ctx, `
SELECT threads.next_message_sequence
FROM agent_threads AS threads
WHERE threads.id = $1 AND threads.user_id = $2
	AND threads.deleted_at IS NULL
FOR UPDATE OF threads`, threadID, ownerID).Scan(&nextSequence)
	if err != nil {
		return 0, mapRunPostgresError(err)
	}
	return nextSequence, nil
}

func advanceThreadSequence(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	content string,
) error {
	title := conversation.DeriveThreadTitle(content)
	tag, err := tx.Exec(ctx, `
UPDATE agent_threads
SET next_message_sequence = next_message_sequence + 1,
    title = COALESCE(title, NULLIF($3, '')),
    updated_at = GREATEST(CURRENT_TIMESTAMP, updated_at + INTERVAL '1 microsecond')
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		threadID, ownerID, title,
	)
	if err != nil {
		return mapRunPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return agentrun.ErrNotFound
	}
	return nil
}

func mapRunPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return agentrun.ErrNotFound
		case "23505":
			return agentrun.ErrConflict
		case "23514":
			return agentrun.ErrInvalidRequest
		}
	}
	return agentrun.ErrRepository
}

var _ agentrun.Repository = (*Repository)(nil)
