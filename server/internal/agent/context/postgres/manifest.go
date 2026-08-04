package postgres

import (
	"context"
	"encoding/json"
	"errors"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) SaveManifest(
	ctx context.Context,
	manifest agentcontext.Manifest,
) (agentcontext.Manifest, error) {
	selectedMessages, err := json.Marshal(manifest.SelectedMessages)
	if err != nil {
		return agentcontext.Manifest{}, agentcontext.ErrInvalidContext
	}
	selectedMemories, err := json.Marshal(manifest.SelectedMemories)
	if err != nil {
		return agentcontext.Manifest{}, agentcontext.ErrInvalidContext
	}
	selectedStableProfile, err := json.Marshal(manifest.SelectedStableProfile)
	if err != nil {
		return agentcontext.Manifest{}, agentcontext.ErrInvalidContext
	}
	var activeGoalID any
	var activeGoalVersion any
	if manifest.ActiveGoalID != "" {
		activeGoalID = manifest.ActiveGoalID
		activeGoalVersion = manifest.ActiveGoalVersion
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
	var result agentcontext.Manifest
	var selectedJSON []byte
	var selectedMemoriesJSON []byte
	var selectedStableProfileJSON []byte
	var exposedToolsJSON []byte
	var toolSchemaHashesJSON []byte
	var persistedGoalID pgtype.Text
	var persistedGoalVersion pgtype.Int8
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
    active_goal_id,
    active_goal_version,
    instruction_version,
    stable_profile_context_policy_version,
    selected_stable_profile,
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
    $10, $11::jsonb, $12, $13, $14, $15, $16, $17, $18,
    $19, $20,
    $21::jsonb, $22, $23, $24, $25, $26, $27, $28,
    CURRENT_TIMESTAMP
)
RETURNING
    run_id::text,
    owner_user_id::text,
    thread_id::text,
    input_message_id::text,
    active_goal_id::text,
    active_goal_version,
    instruction_version,
    stable_profile_context_policy_version,
    selected_stable_profile,
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
		activeGoalID,
		activeGoalVersion,
		manifest.InstructionVersion,
		manifest.StableProfileContextPolicyVersion,
		selectedStableProfile,
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
		&persistedGoalID,
		&persistedGoalVersion,
		&result.InstructionVersion,
		&result.StableProfileContextPolicyVersion,
		&selectedStableProfileJSON,
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
		return agentcontext.Manifest{}, mapContextPostgresError(err)
	}
	if err := decodeManifestOptionals(
		&result,
		persistedGoalID,
		persistedGoalVersion,
		persistedSummaryCheckpointID,
		persistedSummarySourceFromSequence,
		persistedSummaryCoveredThroughSequence,
		persistedSummaryPolicyVersion,
		persistedSummaryPromptVersion,
		persistedSummaryProvider,
		persistedSummaryModel,
		selectedJSON,
		selectedStableProfileJSON,
		selectedMemoriesJSON,
		exposedToolsJSON,
		toolSchemaHashesJSON,
	); err != nil {
		return agentcontext.Manifest{}, err
	}
	return result, nil
}

func (r *Repository) FindManifest(
	ctx context.Context,
	ownerID string,
	runID string,
) (agentcontext.Manifest, error) {
	var result agentcontext.Manifest
	var selectedJSON []byte
	var selectedMemoriesJSON []byte
	var selectedStableProfileJSON []byte
	var exposedToolsJSON []byte
	var toolSchemaHashesJSON []byte
	var activeGoalID pgtype.Text
	var activeGoalVersion pgtype.Int8
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
    active_goal_id::text,
    active_goal_version,
    instruction_version,
    stable_profile_context_policy_version,
    selected_stable_profile,
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
		&activeGoalID,
		&activeGoalVersion,
		&result.InstructionVersion,
		&result.StableProfileContextPolicyVersion,
		&selectedStableProfileJSON,
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
		return agentcontext.Manifest{}, mapContextPostgresError(err)
	}
	if err := decodeManifestOptionals(
		&result,
		activeGoalID,
		activeGoalVersion,
		selectedSummaryCheckpointID,
		selectedSummarySourceFromSequence,
		selectedSummaryCoveredThroughSequence,
		selectedSummaryPolicyVersion,
		selectedSummaryPromptVersion,
		selectedSummaryProvider,
		selectedSummaryModel,
		selectedJSON,
		selectedStableProfileJSON,
		selectedMemoriesJSON,
		exposedToolsJSON,
		toolSchemaHashesJSON,
	); err != nil {
		return agentcontext.Manifest{}, err
	}
	return result, nil
}

func (r *Repository) SaveToolSnapshot(
	ctx context.Context,
	manifest agentcontext.Manifest,
) (agentcontext.Manifest, error) {
	exposedTools, err := json.Marshal(nonNilStrings(manifest.ExposedTools))
	if err != nil {
		return agentcontext.Manifest{}, agentcontext.ErrInvalidContext
	}
	schemaHashes, err := json.Marshal(nonNilStringMap(manifest.ToolSchemaHashes))
	if err != nil {
		return agentcontext.Manifest{}, agentcontext.ErrInvalidContext
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
		return agentcontext.Manifest{}, mapContextPostgresError(err)
	}
	if command.RowsAffected() == 0 {
		return agentcontext.Manifest{}, agentcontext.ErrNotFound
	}
	return r.FindManifest(ctx, manifest.OwnerID, manifest.RunID)
}

func decodeManifestOptionals(
	manifest *agentcontext.Manifest,
	activeGoalID pgtype.Text,
	activeGoalVersion pgtype.Int8,
	selectedSummaryCheckpointID pgtype.Text,
	selectedSummarySourceFromSequence pgtype.Int8,
	selectedSummaryCoveredThroughSequence pgtype.Int8,
	selectedSummaryPolicyVersion pgtype.Text,
	selectedSummaryPromptVersion pgtype.Text,
	selectedSummaryProvider pgtype.Text,
	selectedSummaryModel pgtype.Text,
	selectedJSON []byte,
	selectedStableProfileJSON []byte,
	selectedMemoriesJSON []byte,
	exposedToolsJSON []byte,
	toolSchemaHashesJSON []byte,
) error {
	if activeGoalID.Valid {
		manifest.ActiveGoalID = activeGoalID.String
	}
	if activeGoalVersion.Valid {
		manifest.ActiveGoalVersion = activeGoalVersion.Int64
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
		manifest.SelectedSummary = &agentcontext.SummarySource{
			CheckpointID:           selectedSummaryCheckpointID.String,
			SourceFromSequence:     selectedSummarySourceFromSequence.Int64,
			CoveredThroughSequence: selectedSummaryCoveredThroughSequence.Int64,
			PolicyVersion:          selectedSummaryPolicyVersion.String,
			PromptVersion:          selectedSummaryPromptVersion.String,
			Provider:               selectedSummaryProvider.String,
			Model:                  selectedSummaryModel.String,
		}
	case !summaryFieldsEmpty:
		return agentcontext.ErrRepository
	}
	if err := json.Unmarshal(selectedJSON, &manifest.SelectedMessages); err != nil {
		return agentcontext.ErrRepository
	}
	if err := json.Unmarshal(
		selectedStableProfileJSON,
		&manifest.SelectedStableProfile,
	); err != nil {
		return agentcontext.ErrRepository
	}
	if err := json.Unmarshal(
		selectedMemoriesJSON,
		&manifest.SelectedMemories,
	); err != nil {
		return agentcontext.ErrRepository
	}
	if len(exposedToolsJSON) > 0 {
		if err := json.Unmarshal(exposedToolsJSON, &manifest.ExposedTools); err != nil {
			return agentcontext.ErrRepository
		}
	}
	if len(toolSchemaHashesJSON) > 0 {
		if err := json.Unmarshal(
			toolSchemaHashesJSON,
			&manifest.ToolSchemaHashes,
		); err != nil {
			return agentcontext.ErrRepository
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

func mapContextPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentcontext.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return agentcontext.ErrNotFound
		case "23505":
			return agentcontext.ErrConflict
		case "23514":
			return agentcontext.ErrInvalidContext
		}
	}
	return agentcontext.ErrRepository
}
