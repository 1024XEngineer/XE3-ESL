package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type rowScanner interface {
	Scan(...any) error
}

const runSelectColumns = `
    id::text,
    owner_user_id::text,
    thread_id::text,
    input_message_id::text,
    attempt_no,
    COALESCE(retry_of_run_id::text, ''),
    COALESCE(retry_client_id, ''),
    status,
    requested_provider,
    requested_model,
    max_output_tokens,
    max_input_characters,
    COALESCE(worker_lease_token::text, ''),
    worker_lease_expires_at,
    COALESCE(assistant_message_id::text, ''),
    COALESCE(provider_completion_id, ''),
    COALESCE(provider_model, ''),
    COALESCE(finish_reason, ''),
    input_tokens,
    output_tokens,
    total_tokens,
    COALESCE(failure_kind, ''),
    failure_retryable,
    created_at,
    started_at,
    completed_at,
    updated_at`

const toolCallSelectColumns = `
    id,
    run_id::text,
    owner_user_id::text,
    thread_id::text,
    tool_name,
    schema_version,
    input,
    status,
    result,
    COALESCE(error_category, ''),
    COALESCE(request_id, ''),
    source_refs,
    handoffs,
    proposed_at,
    started_at,
    completed_at,
    updated_at`

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

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		threadID,
		ownerID,
	).Scan(&nextSequence); err != nil {
		return agentrun.Submission{}, mapRunPostgresError(err)
	}

	message, found, err := findInputMessageByClientIDInTransaction(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
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
		if existing, exists, findErr := findInitialRunByInput(
			ctx,
			tx,
			ownerID,
			threadID,
			message.ID,
		); findErr != nil {
			return agentrun.Submission{}, findErr
		} else if exists {
			if err := tx.Commit(ctx); err != nil {
				return agentrun.Submission{}, agentrun.ErrRepository
			}
			return agentrun.Submission{
				Run:         existing,
				UserMessage: message,
				Created:     false,
			}, nil
		}
		return agentrun.Submission{}, agentrun.ErrConflict
	} else {
		messageID, idErr := r.ids.NewID()
		if idErr != nil {
			return agentrun.Submission{}, agentrun.ErrRepository
		}
		var role string
		var modality string
		if err := tx.QueryRow(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at
) VALUES ($1, $2, $3, $4, 'user', $5, 'text', $6, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    client_message_id,
    modality,
    content,
    created_at`,
			messageID,
			ownerID,
			threadID,
			nextSequence,
			clientMessageID,
			content,
		).Scan(
			&message.ID,
			&message.OwnerID,
			&message.ThreadID,
			&message.Sequence,
			&role,
			&message.ClientMessageID,
			&modality,
			&message.Content,
			&message.CreatedAt,
		); err != nil {
			return agentrun.Submission{}, mapRunPostgresError(err)
		}
		message.Role = conversation.MessageRole(role)
		message.Modality = conversation.MessageModality(modality)
		if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
			threadID,
			ownerID,
		); err != nil {
			return agentrun.Submission{}, mapRunPostgresError(err)
		}
	}

	runID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	run, err := insertPendingRun(
		ctx,
		tx,
		runID,
		ownerID,
		threadID,
		message.ID,
		1,
		"",
		"",
		configuration,
	)
	if err != nil {
		return agentrun.Submission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Submission{}, agentrun.ErrRepository
	}
	return agentrun.Submission{
		Run:         run,
		UserMessage: message,
		Created:     true,
	}, nil
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
		ctx,
		tx,
		ownerID,
		original.ThreadID,
		retryClientID,
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
WHERE owner_user_id = $1
  AND thread_id = $2
  AND input_message_id = $3`,
		ownerID,
		original.ThreadID,
		original.InputMessageID,
	).Scan(&nextAttempt); err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	newRunID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Retry{}, agentrun.ErrRepository
	}
	run, err := insertPendingRun(
		ctx,
		tx,
		newRunID,
		ownerID,
		original.ThreadID,
		original.InputMessageID,
		nextAttempt,
		original.ID,
		retryClientID,
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
	workerLeaseToken, err := r.ids.NewID()
	if err != nil {
		return agentrun.Run{}, false, agentrun.ErrRepository
	}
	row := r.database.QueryRow(ctx, `
UPDATE agent_runs
SET
    status = 'running',
    started_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    worker_lease_token = $3,
    worker_lease_expires_at = CURRENT_TIMESTAMP + INTERVAL '10 minutes',
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND status = 'pending'
RETURNING `+runSelectColumns,
		runID,
		ownerID,
		workerLeaseToken,
	)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		current, findErr := r.Find(ctx, ownerID, runID)
		return current, false, findErr
	}
	if err != nil {
		return agentrun.Run{}, false, mapRunPostgresError(err)
	}
	return run, true, nil
}

func (r *Repository) Find(
	ctx context.Context,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	return findRun(ctx, r.database, ownerID, runID)
}

func (r *Repository) ProposeToolCall(
	ctx context.Context,
	record agentrun.ToolCall,
) (agentrun.ToolCall, error) {
	input, err := json.Marshal(record.Input)
	if err != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	if !validToolCallRecordIdentity(record) ||
		record.Name == "" ||
		record.SchemaVersion == "" ||
		len(record.Input) == 0 {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	return scanToolCall(r.database.QueryRow(ctx, `
INSERT INTO agent_tool_calls (
    id,
    run_id,
    owner_user_id,
    thread_id,
    tool_name,
    schema_version,
    input,
    status,
    source_refs,
    proposed_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7::jsonb, 'proposed', '[]'::jsonb,
    CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
ON CONFLICT (run_id, id) DO UPDATE
SET
    updated_at = agent_tool_calls.updated_at
RETURNING `+toolCallSelectColumns,
		record.ID,
		record.RunID,
		record.OwnerID,
		record.ThreadID,
		record.Name,
		record.SchemaVersion,
		input,
	))
}

func (r *Repository) StartToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	requestID string,
) (agentrun.ToolCall, error) {
	record, err := scanToolCall(r.database.QueryRow(ctx, `
UPDATE agent_tool_calls
SET
    status = 'running',
    request_id = $4,
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE run_id = $1
  AND owner_user_id = $2
  AND id = $3
  AND status IN ('proposed', 'running')
RETURNING `+toolCallSelectColumns,
		runID,
		ownerID,
		toolCallID,
		requestID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.ToolCall{}, mapRunPostgresError(err)
	}
	return record, nil
}

func (r *Repository) CompleteToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	result json.RawMessage,
	sourceRefs []agentrun.ToolSourceRef,
	handoffs []agenthandoff.Item,
) (agentrun.ToolCall, error) {
	if err := agenthandoff.ValidateItems(handoffs); err != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	handoffs = agenthandoff.CloneItems(handoffs)
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	refsJSON, err := json.Marshal(sourceRefs)
	if err != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	handoffsJSON, err := json.Marshal(handoffs)
	if err != nil {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	record, err := scanToolCall(r.database.QueryRow(ctx, `
UPDATE agent_tool_calls
SET
    status = 'succeeded',
    result = $4::jsonb,
    source_refs = $5::jsonb,
    handoffs = $6::jsonb,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE run_id = $1
  AND owner_user_id = $2
  AND id = $3
  AND status = 'running'
RETURNING `+toolCallSelectColumns,
		runID,
		ownerID,
		toolCallID,
		resultJSON,
		refsJSON,
		handoffsJSON,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.ToolCall{}, mapRunPostgresError(err)
	}
	return record, nil
}

func (r *Repository) FailToolCall(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	status agentrun.ToolCallStatus,
	errorCategory string,
) (agentrun.ToolCall, error) {
	if status != agentrun.ToolCallFailed && status != agentrun.ToolCallRejected {
		return agentrun.ToolCall{}, agentrun.ErrInvalidRequest
	}
	record, err := scanToolCall(r.database.QueryRow(ctx, `
UPDATE agent_tool_calls
SET
    status = $4,
    error_category = $5,
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
    completed_at = GREATEST(CURRENT_TIMESTAMP, COALESCE(started_at, proposed_at)),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE run_id = $1
  AND owner_user_id = $2
  AND id = $3
  AND status IN ('proposed', 'running')
RETURNING `+toolCallSelectColumns,
		runID,
		ownerID,
		toolCallID,
		string(status),
		errorCategory,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.ToolCall{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.ToolCall{}, mapRunPostgresError(err)
	}
	return record, nil
}

func (r *Repository) ListToolCalls(
	ctx context.Context,
	ownerID string,
	runID string,
) ([]agentrun.ToolCall, error) {
	rows, err := r.database.Query(ctx, `
SELECT `+toolCallSelectColumns+`
FROM agent_tool_calls
WHERE owner_user_id = $1 AND run_id = $2
ORDER BY proposed_at ASC, id ASC`,
		ownerID,
		runID,
	)
	if err != nil {
		return nil, agentrun.ErrRepository
	}
	defer rows.Close()
	records := make([]agentrun.ToolCall, 0)
	for rows.Next() {
		record, err := scanToolCall(rows)
		if err != nil {
			return nil, mapRunPostgresError(err)
		}
		records = append(records, record)
	}
	if rows.Err() != nil {
		return nil, agentrun.ErrRepository
	}
	return records, nil
}

func (r *Repository) Complete(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	completion agentrun.Completion,
) (agentrun.Run, error) {
	// Persistence for structured assistant attachments is intentionally a
	// later vertical slice. Reject non-empty projections instead of silently
	// dropping a successful enrichment decision.
	if !completion.Valid() || len(completion.Enrichment.Memes) != 0 {
		return agentrun.Run{}, agentrun.ErrInvalidRequest
	}
	content := completion.Content
	result := completion.Result
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
	if run.Status != agentrun.StatusRunning ||
		run.WorkerLeaseToken != workerLeaseToken {
		return agentrun.Run{}, agentrun.ErrConflict
	}

	var nextSequence int64
	if err := tx.QueryRow(ctx, `
SELECT next_message_sequence
FROM agent_threads
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		run.ThreadID,
		ownerID,
	).Scan(&nextSequence); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	messageID, err := r.ids.NewID()
	if err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    produced_by_run_id,
    modality,
    content,
    created_at
) VALUES (
    $1, $2, $3, $4, 'assistant', NULL, $5, 'text', $6, CURRENT_TIMESTAMP
)`,
		messageID,
		ownerID,
		run.ThreadID,
		nextSequence,
		run.ID,
		content,
	); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE agent_threads
SET
    next_message_sequence = next_message_sequence + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
		run.ThreadID,
		ownerID,
	); err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}

	completed, err := scanRun(tx.QueryRow(ctx, `
UPDATE agent_runs
SET
    status = 'completed',
    assistant_message_id = $3,
    provider_completion_id = $4,
    provider_model = $5,
    finish_reason = $6,
    input_tokens = $7,
    output_tokens = $8,
    total_tokens = $9,
    worker_lease_token = NULL,
    worker_lease_expires_at = NULL,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND worker_lease_token = $10
  AND worker_lease_expires_at > CURRENT_TIMESTAMP
RETURNING `+runSelectColumns,
		runID,
		ownerID,
		messageID,
		result.ID,
		result.Model,
		result.FinishReason,
		result.Usage.InputTokens,
		result.Usage.OutputTokens,
		result.Usage.TotalTokens,
		workerLeaseToken,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.Run{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return agentrun.Run{}, agentrun.ErrRepository
	}
	return completed, nil
}

func (r *Repository) Fail(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	failureKind string,
	retryable bool,
) (agentrun.Run, error) {
	run, err := scanRun(r.database.QueryRow(ctx, `
UPDATE agent_runs
SET
    status = 'failed',
    failure_kind = $3,
    failure_retryable = $4,
    worker_lease_token = NULL,
    worker_lease_expires_at = NULL,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND status = 'running'
  AND worker_lease_token = $5
  AND worker_lease_expires_at > CURRENT_TIMESTAMP
RETURNING `+runSelectColumns,
		runID,
		ownerID,
		failureKind,
		retryable,
		workerLeaseToken,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		current, findErr := r.Find(ctx, ownerID, runID)
		if findErr != nil {
			return agentrun.Run{}, findErr
		}
		if current.Status == agentrun.StatusFailed {
			return current, nil
		}
		return agentrun.Run{}, agentrun.ErrConflict
	}
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	return run, nil
}

func (r *Repository) RecoverInterrupted(
	ctx context.Context,
) (int64, error) {
	command, err := r.database.Exec(ctx, `
UPDATE agent_runs
SET
    status = 'failed',
    failure_kind = 'interrupted',
    failure_retryable = true,
    worker_lease_token = NULL,
    worker_lease_expires_at = NULL,
    completed_at = GREATEST(CURRENT_TIMESTAMP, started_at),
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE status = 'running'
  AND worker_lease_expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, agentrun.ErrRepository
	}
	return command.RowsAffected(), nil
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
	var retryOf any
	var retryClient any
	if retryOfRunID != "" {
		retryOf = retryOfRunID
		retryClient = retryClientID
	}
	run, err := scanRun(tx.QueryRow(ctx, `
INSERT INTO agent_runs (
    id,
    owner_user_id,
    thread_id,
    input_message_id,
    attempt_no,
    retry_of_run_id,
    retry_client_id,
    status,
    requested_provider,
    requested_model,
    max_output_tokens,
    max_input_characters,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $10, $11,
    CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
RETURNING `+runSelectColumns,
		runID,
		ownerID,
		threadID,
		inputMessageID,
		attempt,
		retryOf,
		retryClient,
		configuration.Provider,
		configuration.Model,
		configuration.MaxOutputTokens,
		configuration.MaxInputCharacters,
	))
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	return run, nil
}

func findInitialRunByInput(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	inputMessageID string,
) (agentrun.Run, bool, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE owner_user_id = $1
  AND thread_id = $2
  AND input_message_id = $3
  AND attempt_no = 1`,
		ownerID,
		threadID,
		inputMessageID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.Run{}, false, nil
	}
	if err != nil {
		return agentrun.Run{}, false, mapRunPostgresError(err)
	}
	return run, true, nil
}

func findRunByRetryClientID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	retryClientID string,
) (agentrun.Run, bool, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE owner_user_id = $1
  AND thread_id = $2
  AND retry_client_id = $3`,
		ownerID,
		threadID,
		retryClientID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrun.Run{}, false, nil
	}
	if err != nil {
		return agentrun.Run{}, false, mapRunPostgresError(err)
	}
	return run, true, nil
}

func findRunForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	runID string,
) (agentrun.Run, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		runID,
		ownerID,
	))
	if err != nil {
		return agentrun.Run{}, mapRunPostgresError(err)
	}
	return run, nil
}

func scanRun(row rowScanner) (agentrun.Run, error) {
	var result agentrun.Run
	var status string
	var inputTokens pgtype.Int4
	var outputTokens pgtype.Int4
	var totalTokens pgtype.Int4
	var failureRetryable pgtype.Bool
	var startedAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	var workerLeaseExpiresAt pgtype.Timestamptz
	err := row.Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.InputMessageID,
		&result.Attempt,
		&result.RetryOfRunID,
		&result.RetryClientID,
		&status,
		&result.RequestedProvider,
		&result.RequestedModel,
		&result.MaxOutputTokens,
		&result.MaxInputCharacters,
		&result.WorkerLeaseToken,
		&workerLeaseExpiresAt,
		&result.AssistantMessageID,
		&result.ProviderCompletionID,
		&result.ProviderModel,
		&result.FinishReason,
		&inputTokens,
		&outputTokens,
		&totalTokens,
		&result.FailureKind,
		&failureRetryable,
		&result.CreatedAt,
		&startedAt,
		&completedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return agentrun.Run{}, err
	}
	result.Status = agentrun.Status(status)
	if inputTokens.Valid {
		result.Usage.InputTokens = int(inputTokens.Int32)
	}
	if outputTokens.Valid {
		result.Usage.OutputTokens = int(outputTokens.Int32)
	}
	if totalTokens.Valid {
		result.Usage.TotalTokens = int(totalTokens.Int32)
	}
	if failureRetryable.Valid {
		result.FailureRetryable = failureRetryable.Bool
	}
	if startedAt.Valid {
		result.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		result.CompletedAt = completedAt.Time
	}
	if workerLeaseExpiresAt.Valid {
		result.WorkerLeaseExpiresAt = workerLeaseExpiresAt.Time
	}
	return result, nil
}

func scanToolCall(row rowScanner) (agentrun.ToolCall, error) {
	var result agentrun.ToolCall
	var status string
	var inputJSON []byte
	var resultJSON []byte
	var sourceRefsJSON []byte
	var handoffsJSON []byte
	var startedAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	err := row.Scan(
		&result.ID,
		&result.RunID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Name,
		&result.SchemaVersion,
		&inputJSON,
		&status,
		&resultJSON,
		&result.ErrorCategory,
		&result.RequestID,
		&sourceRefsJSON,
		&handoffsJSON,
		&result.ProposedAt,
		&startedAt,
		&completedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return agentrun.ToolCall{}, err
	}
	result.Status = agentrun.ToolCallStatus(status)
	result.Input = append(json.RawMessage(nil), inputJSON...)
	if len(resultJSON) > 0 {
		result.Result = append(json.RawMessage(nil), resultJSON...)
	}
	if len(sourceRefsJSON) > 0 {
		if err := json.Unmarshal(sourceRefsJSON, &result.SourceRefs); err != nil {
			return agentrun.ToolCall{}, agentrun.ErrRepository
		}
	}
	if len(handoffsJSON) > 0 {
		if err := json.Unmarshal(handoffsJSON, &result.Handoffs); err != nil ||
			agenthandoff.ValidateItems(result.Handoffs) != nil {
			return agentrun.ToolCall{}, agentrun.ErrRepository
		}
		result.Handoffs = agenthandoff.CloneItems(result.Handoffs)
	}
	if startedAt.Valid {
		result.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		result.CompletedAt = completedAt.Time
	}
	return result, nil
}

func validToolCallRecordIdentity(record agentrun.ToolCall) bool {
	return agentrun.ValidModelID(record.ID) &&
		agentrun.ValidUUID(record.RunID) &&
		agentrun.ValidUUID(record.OwnerID) &&
		agentrun.ValidUUID(record.ThreadID)
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
