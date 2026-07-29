package persistence

import (
	"context"
	"encoding/json"
	"errors"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/jackc/pgx/v5"
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
    proposed_at,
    started_at,
    completed_at,
    updated_at`

func (r *PostgresRepository) CreateInitialRun(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
	configuration RunConfiguration,
) (RunSubmission, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return RunSubmission{}, ErrRepository
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
		return RunSubmission{}, mapPostgresError(err)
	}

	message, found, err := findMessageByClientID(
		ctx,
		tx,
		ownerID,
		threadID,
		clientMessageID,
	)
	if err != nil {
		return RunSubmission{}, err
	}
	if found {
		if message.Content != content ||
			message.Role != MessageRoleUser ||
			message.Modality != MessageModalityText {
			return RunSubmission{}, ErrIdempotencyConflict
		}
		if existing, exists, findErr := findInitialRunByInput(
			ctx,
			tx,
			ownerID,
			threadID,
			message.ID,
		); findErr != nil {
			return RunSubmission{}, findErr
		} else if exists {
			if err := tx.Commit(ctx); err != nil {
				return RunSubmission{}, ErrRepository
			}
			return RunSubmission{
				Run:         existing,
				UserMessage: message,
				Created:     false,
			}, nil
		}
		return RunSubmission{}, ErrConflict
	} else {
		messageID, idErr := r.ids.NewID()
		if idErr != nil {
			return RunSubmission{}, ErrRepository
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
			return RunSubmission{}, mapPostgresError(err)
		}
		message.Role = MessageRole(role)
		message.Modality = MessageModality(modality)
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
			return RunSubmission{}, mapPostgresError(err)
		}
	}

	runID, err := r.ids.NewID()
	if err != nil {
		return RunSubmission{}, ErrRepository
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
		return RunSubmission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RunSubmission{}, ErrRepository
	}
	return RunSubmission{
		Run:         run,
		UserMessage: message,
		Created:     true,
	}, nil
}

func (r *PostgresRepository) CreateRetryRun(
	ctx context.Context,
	ownerID string,
	runID string,
	retryClientID string,
	configuration RunConfiguration,
) (RunRetry, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return RunRetry{}, ErrRepository
	}
	defer rollback(tx)

	original, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return RunRetry{}, err
	}
	existing, found, err := findRunByRetryClientID(
		ctx,
		tx,
		ownerID,
		original.ThreadID,
		retryClientID,
	)
	if err != nil {
		return RunRetry{}, err
	}
	if found {
		if existing.RetryOfRunID != original.ID ||
			existing.InputMessageID != original.InputMessageID {
			return RunRetry{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return RunRetry{}, ErrRepository
		}
		return RunRetry{Run: existing, Created: false}, nil
	}
	if original.Status != RunStatusFailed || !original.FailureRetryable {
		return RunRetry{}, ErrConflict
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
		return RunRetry{}, ErrRepository
	}
	newRunID, err := r.ids.NewID()
	if err != nil {
		return RunRetry{}, ErrRepository
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
		return RunRetry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RunRetry{}, ErrRepository
	}
	return RunRetry{Run: run, Created: true}, nil
}

func (r *PostgresRepository) ClaimRun(
	ctx context.Context,
	ownerID string,
	runID string,
) (Run, bool, error) {
	workerLeaseToken, err := r.ids.NewID()
	if err != nil {
		return Run{}, false, ErrRepository
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
		current, findErr := r.FindRun(ctx, ownerID, runID)
		return current, false, findErr
	}
	if err != nil {
		return Run{}, false, mapPostgresError(err)
	}
	return run, true, nil
}

func (r *PostgresRepository) FindRun(
	ctx context.Context,
	ownerID string,
	runID string,
) (Run, error) {
	run, err := scanRun(r.database.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE id = $1 AND owner_user_id = $2`,
		runID,
		ownerID,
	))
	if err != nil {
		return Run{}, mapPostgresError(err)
	}
	return run, nil
}

func (r *PostgresRepository) FindMessage(
	ctx context.Context,
	ownerID string,
	threadID string,
	messageID string,
) (Message, error) {
	var result Message
	var role string
	var modality string
	err := r.database.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    COALESCE(client_message_id, ''),
    COALESCE(produced_by_run_id::text, ''),
    modality,
    content,
    created_at
FROM agent_messages
WHERE id = $1
  AND owner_user_id = $2
  AND thread_id = $3`,
		messageID,
		ownerID,
		threadID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.ThreadID,
		&result.Sequence,
		&role,
		&result.ClientMessageID,
		&result.ProducedByRunID,
		&modality,
		&result.Content,
		&result.CreatedAt,
	)
	if err != nil {
		return Message{}, mapPostgresError(err)
	}
	result.Role = MessageRole(role)
	result.Modality = MessageModality(modality)
	return result, nil
}

func (r *PostgresRepository) ListMessagesForContext(
	ctx context.Context,
	ownerID string,
	threadID string,
	minSequenceExclusive int64,
	maxSequence int64,
	characterBudget int,
) ([]Message, int, error) {
	// Every committed message contains at least one Unicode character, so a
	// character budget also bounds the maximum row count read from the index.
	// Thread sequence numbers are contiguous and never reused, which makes the
	// omitted count derivable without counting the full history.
	rows, err := r.database.Query(ctx, `
WITH recent AS (
    SELECT
        id,
        owner_user_id,
        thread_id,
        sequence_no,
        role,
        client_message_id,
        produced_by_run_id,
        content,
        created_at
    FROM agent_messages
    WHERE owner_user_id = $1
      AND thread_id = $2
      AND sequence_no > $3
      AND sequence_no <= $4
    ORDER BY sequence_no DESC
    LIMIT $5
),
eligible AS (
    SELECT
        recent.*,
        SUM(char_length(content)) OVER (
            ORDER BY sequence_no DESC
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS cumulative_characters
    FROM recent
),
selected AS (
    SELECT *
    FROM eligible
    WHERE cumulative_characters <= $5
)
SELECT
    id::text,
    owner_user_id::text,
    thread_id::text,
    sequence_no,
    role,
    COALESCE(client_message_id, ''),
    COALESCE(produced_by_run_id::text, ''),
    content,
    created_at
FROM selected
ORDER BY sequence_no ASC`,
		ownerID,
		threadID,
		minSequenceExclusive,
		maxSequence,
		characterBudget,
	)
	if err != nil {
		return nil, 0, ErrRepository
	}
	defer rows.Close()

	result := make([]Message, 0)
	for rows.Next() {
		var item Message
		var role string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.ThreadID,
			&item.Sequence,
			&role,
			&item.ClientMessageID,
			&item.ProducedByRunID,
			&item.Content,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, ErrRepository
		}
		item.Role = MessageRole(role)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, 0, ErrRepository
	}
	return result, int(maxSequence) - len(result), nil
}

func (r *PostgresRepository) SaveContextManifest(
	ctx context.Context,
	manifest ContextManifest,
) (ContextManifest, error) {
	selectedMessages, err := json.Marshal(manifest.SelectedMessages)
	if err != nil {
		return ContextManifest{}, ErrInvalidRequest
	}
	selectedMemories, err := json.Marshal(manifest.SelectedMemories)
	if err != nil {
		return ContextManifest{}, ErrInvalidRequest
	}
	var activeMatterID any
	var activeMatterVersion any
	if manifest.ActiveMatterID != "" {
		activeMatterID = manifest.ActiveMatterID
		activeMatterVersion = manifest.ActiveMatterVersion
	}
	var summaryCheckpointID any
	var summarySourceFromSequence any
	var summaryCoveredThroughSequence any
	var summaryPolicyVersion any
	var summaryPromptVersion any
	var summaryProvider any
	var summaryModel any
	if manifest.SelectedSummary != nil {
		summaryCheckpointID = manifest.SelectedSummary.CheckpointID
		summarySourceFromSequence =
			manifest.SelectedSummary.SourceFromSequence
		summaryCoveredThroughSequence =
			manifest.SelectedSummary.CoveredThroughSequence
		summaryPolicyVersion = manifest.SelectedSummary.PolicyVersion
		summaryPromptVersion = manifest.SelectedSummary.PromptVersion
		summaryProvider = manifest.SelectedSummary.Provider
		summaryModel = manifest.SelectedSummary.Model
	}
	var result ContextManifest
	var selectedJSON []byte
	var selectedMemoriesJSON []byte
	var exposedToolsJSON []byte
	var toolSchemaHashesJSON []byte
	var persistedMatterID pgtype.Text
	var persistedMatterVersion pgtype.Int8
	var persistedSummaryCheckpointID pgtype.Text
	var persistedSummarySourceFromSequence pgtype.Int8
	var persistedSummaryCoveredThroughSequence pgtype.Int8
	var persistedSummaryPolicyVersion pgtype.Text
	var persistedSummaryPromptVersion pgtype.Text
	var persistedSummaryProvider pgtype.Text
	var persistedSummaryModel pgtype.Text
	err = r.database.QueryRow(ctx, `
INSERT INTO agent_context_manifests (
    run_id,
    owner_user_id,
    thread_id,
    input_message_id,
    active_matter_id,
    active_matter_version,
    instruction_version,
    memory_context_policy_version,
    selected_memories,
    summary_context_policy_version,
    summary_context_status,
    selected_summary_checkpoint_id,
    selected_summary_source_from_sequence,
    selected_summary_covered_through_sequence,
    selected_summary_policy_version,
    selected_summary_prompt_version,
    selected_summary_provider,
    selected_summary_model,
    selected_messages,
    omitted_message_count,
    trim_reason,
    max_input_characters,
    used_input_characters,
    requested_provider,
    requested_model,
    max_output_tokens,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb,
    $10, $11, $12, $13, $14, $15, $16, $17, $18,
    $19::jsonb, $20, $21, $22, $23, $24, $25, $26,
    CURRENT_TIMESTAMP
)
RETURNING
    run_id::text,
    owner_user_id::text,
    thread_id::text,
    input_message_id::text,
    active_matter_id::text,
    active_matter_version,
    instruction_version,
    memory_context_policy_version,
    selected_memories,
    summary_context_policy_version,
    summary_context_status,
    selected_summary_checkpoint_id::text,
    selected_summary_source_from_sequence,
    selected_summary_covered_through_sequence,
    selected_summary_policy_version,
    selected_summary_prompt_version,
    selected_summary_provider,
    selected_summary_model,
    selected_messages,
    omitted_message_count,
    trim_reason,
    max_input_characters,
    used_input_characters,
    requested_provider,
    requested_model,
    max_output_tokens,
    exposed_tools,
    tool_schema_hashes,
    created_at`,
		manifest.RunID,
		manifest.OwnerID,
		manifest.ThreadID,
		manifest.InputMessageID,
		activeMatterID,
		activeMatterVersion,
		manifest.InstructionVersion,
		manifest.MemoryContextPolicyVersion,
		selectedMemories,
		manifest.SummaryContextPolicyVersion,
		manifest.SummaryContextStatus,
		summaryCheckpointID,
		summarySourceFromSequence,
		summaryCoveredThroughSequence,
		summaryPolicyVersion,
		summaryPromptVersion,
		summaryProvider,
		summaryModel,
		selectedMessages,
		manifest.OmittedMessageCount,
		manifest.TrimReason,
		manifest.MaxInputCharacters,
		manifest.UsedInputCharacters,
		manifest.RequestedProvider,
		manifest.RequestedModel,
		manifest.MaxOutputTokens,
	).Scan(
		&result.RunID,
		&result.OwnerID,
		&result.ThreadID,
		&result.InputMessageID,
		&persistedMatterID,
		&persistedMatterVersion,
		&result.InstructionVersion,
		&result.MemoryContextPolicyVersion,
		&selectedMemoriesJSON,
		&result.SummaryContextPolicyVersion,
		&result.SummaryContextStatus,
		&persistedSummaryCheckpointID,
		&persistedSummarySourceFromSequence,
		&persistedSummaryCoveredThroughSequence,
		&persistedSummaryPolicyVersion,
		&persistedSummaryPromptVersion,
		&persistedSummaryProvider,
		&persistedSummaryModel,
		&selectedJSON,
		&result.OmittedMessageCount,
		&result.TrimReason,
		&result.MaxInputCharacters,
		&result.UsedInputCharacters,
		&result.RequestedProvider,
		&result.RequestedModel,
		&result.MaxOutputTokens,
		&exposedToolsJSON,
		&toolSchemaHashesJSON,
		&result.CreatedAt,
	)
	if err != nil {
		return ContextManifest{}, mapPostgresError(err)
	}
	if err := decodeManifestOptionals(
		&result,
		persistedMatterID,
		persistedMatterVersion,
		persistedSummaryCheckpointID,
		persistedSummarySourceFromSequence,
		persistedSummaryCoveredThroughSequence,
		persistedSummaryPolicyVersion,
		persistedSummaryPromptVersion,
		persistedSummaryProvider,
		persistedSummaryModel,
		selectedJSON,
		selectedMemoriesJSON,
		exposedToolsJSON,
		toolSchemaHashesJSON,
	); err != nil {
		return ContextManifest{}, err
	}
	return result, nil
}

func (r *PostgresRepository) FindContextManifest(
	ctx context.Context,
	ownerID string,
	runID string,
) (ContextManifest, error) {
	var result ContextManifest
	var selectedJSON []byte
	var selectedMemoriesJSON []byte
	var exposedToolsJSON []byte
	var toolSchemaHashesJSON []byte
	var activeMatterID pgtype.Text
	var activeMatterVersion pgtype.Int8
	var selectedSummaryCheckpointID pgtype.Text
	var selectedSummarySourceFromSequence pgtype.Int8
	var selectedSummaryCoveredThroughSequence pgtype.Int8
	var selectedSummaryPolicyVersion pgtype.Text
	var selectedSummaryPromptVersion pgtype.Text
	var selectedSummaryProvider pgtype.Text
	var selectedSummaryModel pgtype.Text
	err := r.database.QueryRow(ctx, `
SELECT
    run_id::text,
    owner_user_id::text,
    thread_id::text,
    input_message_id::text,
    active_matter_id::text,
    active_matter_version,
    instruction_version,
    memory_context_policy_version,
    selected_memories,
    summary_context_policy_version,
    summary_context_status,
    selected_summary_checkpoint_id::text,
    selected_summary_source_from_sequence,
    selected_summary_covered_through_sequence,
    selected_summary_policy_version,
    selected_summary_prompt_version,
    selected_summary_provider,
    selected_summary_model,
    selected_messages,
    omitted_message_count,
    trim_reason,
    max_input_characters,
    used_input_characters,
    requested_provider,
    requested_model,
    max_output_tokens,
    exposed_tools,
    tool_schema_hashes,
    created_at
FROM agent_context_manifests
WHERE run_id = $1 AND owner_user_id = $2`,
		runID,
		ownerID,
	).Scan(
		&result.RunID,
		&result.OwnerID,
		&result.ThreadID,
		&result.InputMessageID,
		&activeMatterID,
		&activeMatterVersion,
		&result.InstructionVersion,
		&result.MemoryContextPolicyVersion,
		&selectedMemoriesJSON,
		&result.SummaryContextPolicyVersion,
		&result.SummaryContextStatus,
		&selectedSummaryCheckpointID,
		&selectedSummarySourceFromSequence,
		&selectedSummaryCoveredThroughSequence,
		&selectedSummaryPolicyVersion,
		&selectedSummaryPromptVersion,
		&selectedSummaryProvider,
		&selectedSummaryModel,
		&selectedJSON,
		&result.OmittedMessageCount,
		&result.TrimReason,
		&result.MaxInputCharacters,
		&result.UsedInputCharacters,
		&result.RequestedProvider,
		&result.RequestedModel,
		&result.MaxOutputTokens,
		&exposedToolsJSON,
		&toolSchemaHashesJSON,
		&result.CreatedAt,
	)
	if err != nil {
		return ContextManifest{}, mapPostgresError(err)
	}
	if err := decodeManifestOptionals(
		&result,
		activeMatterID,
		activeMatterVersion,
		selectedSummaryCheckpointID,
		selectedSummarySourceFromSequence,
		selectedSummaryCoveredThroughSequence,
		selectedSummaryPolicyVersion,
		selectedSummaryPromptVersion,
		selectedSummaryProvider,
		selectedSummaryModel,
		selectedJSON,
		selectedMemoriesJSON,
		exposedToolsJSON,
		toolSchemaHashesJSON,
	); err != nil {
		return ContextManifest{}, err
	}
	return result, nil
}

func (r *PostgresRepository) SaveContextToolSnapshot(
	ctx context.Context,
	manifest ContextManifest,
) (ContextManifest, error) {
	exposedTools, err := json.Marshal(nonNilStrings(manifest.ExposedTools))
	if err != nil {
		return ContextManifest{}, ErrInvalidRequest
	}
	schemaHashes, err := json.Marshal(nonNilStringMap(manifest.ToolSchemaHashes))
	if err != nil {
		return ContextManifest{}, ErrInvalidRequest
	}
	command, err := r.database.Exec(ctx, `
UPDATE agent_context_manifests
SET
    exposed_tools = $3::jsonb,
    tool_schema_hashes = $4::jsonb
WHERE run_id = $1 AND owner_user_id = $2`,
		manifest.RunID,
		manifest.OwnerID,
		exposedTools,
		schemaHashes,
	)
	if err != nil {
		return ContextManifest{}, mapPostgresError(err)
	}
	if command.RowsAffected() == 0 {
		return ContextManifest{}, ErrNotFound
	}
	return r.FindContextManifest(ctx, manifest.OwnerID, manifest.RunID)
}

func (r *PostgresRepository) SaveToolCallProposed(
	ctx context.Context,
	record ToolCallRecord,
) (ToolCallRecord, error) {
	input, err := json.Marshal(record.Input)
	if err != nil {
		return ToolCallRecord{}, ErrInvalidRequest
	}
	if !validToolCallRecordIdentity(record) ||
		record.Name == "" ||
		record.SchemaVersion == "" ||
		len(record.Input) == 0 {
		return ToolCallRecord{}, ErrInvalidRequest
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

func (r *PostgresRepository) MarkToolCallRunning(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	requestID string,
) (ToolCallRecord, error) {
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
		return ToolCallRecord{}, ErrConflict
	}
	if err != nil {
		return ToolCallRecord{}, mapPostgresError(err)
	}
	return record, nil
}

func (r *PostgresRepository) MarkToolCallSucceeded(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	result json.RawMessage,
	sourceRefs []ToolSourceRef,
) (ToolCallRecord, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return ToolCallRecord{}, ErrInvalidRequest
	}
	refsJSON, err := json.Marshal(sourceRefs)
	if err != nil {
		return ToolCallRecord{}, ErrInvalidRequest
	}
	record, err := scanToolCall(r.database.QueryRow(ctx, `
UPDATE agent_tool_calls
SET
    status = 'succeeded',
    result = $4::jsonb,
    source_refs = $5::jsonb,
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
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolCallRecord{}, ErrConflict
	}
	if err != nil {
		return ToolCallRecord{}, mapPostgresError(err)
	}
	return record, nil
}

func (r *PostgresRepository) MarkToolCallFailed(
	ctx context.Context,
	ownerID string,
	runID string,
	toolCallID string,
	status ToolCallStatus,
	errorCategory string,
) (ToolCallRecord, error) {
	if status != ToolCallStatusFailed && status != ToolCallStatusRejected {
		return ToolCallRecord{}, ErrInvalidRequest
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
		return ToolCallRecord{}, ErrConflict
	}
	if err != nil {
		return ToolCallRecord{}, mapPostgresError(err)
	}
	return record, nil
}

func (r *PostgresRepository) ListToolCalls(
	ctx context.Context,
	ownerID string,
	runID string,
) ([]ToolCallRecord, error) {
	rows, err := r.database.Query(ctx, `
SELECT `+toolCallSelectColumns+`
FROM agent_tool_calls
WHERE owner_user_id = $1 AND run_id = $2
ORDER BY proposed_at ASC, id ASC`,
		ownerID,
		runID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	records := make([]ToolCallRecord, 0)
	for rows.Next() {
		record, err := scanToolCall(rows)
		if err != nil {
			return nil, mapPostgresError(err)
		}
		records = append(records, record)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return records, nil
}

func (r *PostgresRepository) CompleteRun(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	content string,
	result ai.TextResult,
) (Run, error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Run{}, ErrRepository
	}
	defer rollback(tx)
	run, err := findRunForUpdate(ctx, tx, ownerID, runID)
	if err != nil {
		return Run{}, err
	}
	if run.Status == RunStatusCompleted {
		if err := tx.Commit(ctx); err != nil {
			return Run{}, ErrRepository
		}
		return run, nil
	}
	if run.Status != RunStatusRunning ||
		run.WorkerLeaseToken != workerLeaseToken {
		return Run{}, ErrConflict
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
		return Run{}, mapPostgresError(err)
	}
	messageID, err := r.ids.NewID()
	if err != nil {
		return Run{}, ErrRepository
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
		return Run{}, mapPostgresError(err)
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
		return Run{}, mapPostgresError(err)
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
		return Run{}, ErrConflict
	}
	if err != nil {
		return Run{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, ErrRepository
	}
	return completed, nil
}

func (r *PostgresRepository) FailRun(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	failureKind string,
	retryable bool,
) (Run, error) {
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
		current, findErr := r.FindRun(ctx, ownerID, runID)
		if findErr != nil {
			return Run{}, findErr
		}
		if current.Status == RunStatusFailed {
			return current, nil
		}
		return Run{}, ErrConflict
	}
	if err != nil {
		return Run{}, mapPostgresError(err)
	}
	return run, nil
}

func (r *PostgresRepository) RecoverInterruptedRuns(
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
		return 0, ErrRepository
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
	configuration RunConfiguration,
) (Run, error) {
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
		return Run{}, mapPostgresError(err)
	}
	return run, nil
}

func findInitialRunByInput(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	inputMessageID string,
) (Run, bool, error) {
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
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, mapPostgresError(err)
	}
	return run, true, nil
}

func findRunByRetryClientID(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	threadID string,
	retryClientID string,
) (Run, bool, error) {
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
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, mapPostgresError(err)
	}
	return run, true, nil
}

func findRunForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	runID string,
) (Run, error) {
	run, err := scanRun(tx.QueryRow(ctx, `
SELECT `+runSelectColumns+`
FROM agent_runs
WHERE id = $1 AND owner_user_id = $2
FOR UPDATE`,
		runID,
		ownerID,
	))
	if err != nil {
		return Run{}, mapPostgresError(err)
	}
	return run, nil
}

func scanRun(row rowScanner) (Run, error) {
	var result Run
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
		return Run{}, err
	}
	result.Status = RunStatus(status)
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

func scanToolCall(row rowScanner) (ToolCallRecord, error) {
	var result ToolCallRecord
	var status string
	var inputJSON []byte
	var resultJSON []byte
	var sourceRefsJSON []byte
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
		&result.ProposedAt,
		&startedAt,
		&completedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return ToolCallRecord{}, err
	}
	result.Status = ToolCallStatus(status)
	result.Input = append(json.RawMessage(nil), inputJSON...)
	if len(resultJSON) > 0 {
		result.Result = append(json.RawMessage(nil), resultJSON...)
	}
	if len(sourceRefsJSON) > 0 {
		if err := json.Unmarshal(sourceRefsJSON, &result.SourceRefs); err != nil {
			return ToolCallRecord{}, ErrRepository
		}
	}
	if startedAt.Valid {
		result.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		result.CompletedAt = completedAt.Time
	}
	return result, nil
}

func validToolCallRecordIdentity(record ToolCallRecord) bool {
	return ValidModelID(record.ID) &&
		ValidUUID(record.RunID) &&
		ValidUUID(record.OwnerID) &&
		ValidUUID(record.ThreadID)
}

func decodeManifestOptionals(
	manifest *ContextManifest,
	activeMatterID pgtype.Text,
	activeMatterVersion pgtype.Int8,
	selectedSummaryCheckpointID pgtype.Text,
	selectedSummarySourceFromSequence pgtype.Int8,
	selectedSummaryCoveredThroughSequence pgtype.Int8,
	selectedSummaryPolicyVersion pgtype.Text,
	selectedSummaryPromptVersion pgtype.Text,
	selectedSummaryProvider pgtype.Text,
	selectedSummaryModel pgtype.Text,
	selectedJSON []byte,
	selectedMemoriesJSON []byte,
	exposedToolsJSON []byte,
	toolSchemaHashesJSON []byte,
) error {
	if activeMatterID.Valid {
		manifest.ActiveMatterID = activeMatterID.String
	}
	if activeMatterVersion.Valid {
		manifest.ActiveMatterVersion = activeMatterVersion.Int64
	}
	summaryFieldsValid := selectedSummaryCheckpointID.Valid &&
		selectedSummarySourceFromSequence.Valid &&
		selectedSummaryCoveredThroughSequence.Valid &&
		selectedSummaryPolicyVersion.Valid &&
		selectedSummaryPromptVersion.Valid &&
		selectedSummaryProvider.Valid &&
		selectedSummaryModel.Valid
	summaryFieldsEmpty := !selectedSummaryCheckpointID.Valid &&
		!selectedSummarySourceFromSequence.Valid &&
		!selectedSummaryCoveredThroughSequence.Valid &&
		!selectedSummaryPolicyVersion.Valid &&
		!selectedSummaryPromptVersion.Valid &&
		!selectedSummaryProvider.Valid &&
		!selectedSummaryModel.Valid
	switch {
	case summaryFieldsValid:
		manifest.SelectedSummary = &ContextSummarySource{
			CheckpointID:           selectedSummaryCheckpointID.String,
			SourceFromSequence:     selectedSummarySourceFromSequence.Int64,
			CoveredThroughSequence: selectedSummaryCoveredThroughSequence.Int64,
			PolicyVersion:          selectedSummaryPolicyVersion.String,
			PromptVersion:          selectedSummaryPromptVersion.String,
			Provider:               selectedSummaryProvider.String,
			Model:                  selectedSummaryModel.String,
		}
	case !summaryFieldsEmpty:
		return ErrRepository
	}
	if err := json.Unmarshal(selectedJSON, &manifest.SelectedMessages); err != nil {
		return ErrRepository
	}
	if err := json.Unmarshal(
		selectedMemoriesJSON,
		&manifest.SelectedMemories,
	); err != nil {
		return ErrRepository
	}
	if len(exposedToolsJSON) > 0 {
		if err := json.Unmarshal(exposedToolsJSON, &manifest.ExposedTools); err != nil {
			return ErrRepository
		}
	}
	if len(toolSchemaHashesJSON) > 0 {
		if err := json.Unmarshal(
			toolSchemaHashesJSON,
			&manifest.ToolSchemaHashes,
		); err != nil {
			return ErrRepository
		}
	}
	return nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
