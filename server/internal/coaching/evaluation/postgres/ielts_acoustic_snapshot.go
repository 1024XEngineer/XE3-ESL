package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/jackc/pgx/v5"
)

var _ scoring.IELTSAcousticSnapshotRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) FindPendingIELTSAcousticSnapshot(
	ctx context.Context,
) (scoring.IELTSAcousticSnapshotClaim, bool, error) {
	if r == nil || r.pool == nil || ctx == nil {
		return scoring.IELTSAcousticSnapshotClaim{}, false,
			evaluation.ErrInvalidRequest
	}
	var claim scoring.IELTSAcousticSnapshotClaim
	err := r.pool.QueryRow(ctx, `
		SELECT
			ledger.id::text,
			revision.id::text,
			ledger.owner_user_id::text,
			revision.created_at
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		JOIN evaluation_outbox AS outbox
		  ON outbox.evaluation_id = ledger.id
		 AND outbox.evaluation_revision_id = revision.id
		 AND outbox.owner_user_id = ledger.owner_user_id
		 AND outbox.channel = 'SCENE'
		JOIN identity_users AS owner
		  ON owner.id = ledger.owner_user_id
		WHERE ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'IELTS_SPEAKING'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $1
		  AND revision.pipeline_version = $2
		  AND revision.schema_version = $3
		  AND state.evaluation_status = 'VALIDATING'
		  AND state.completed_at IS NULL
		  AND outbox.delivery_status = 'PENDING'
		  AND owner.account_status = 'active'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.revision > revision.revision
		  )
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_deletion_fences AS fence
		      WHERE fence.owner_user_id = ledger.owner_user_id
		  )
		ORDER BY revision.created_at, revision.id
		LIMIT 1
	`, scoring.IELTSSpeakingShadowStrategyRef,
		scoring.IELTSSpeakingShadowPipelineVersion,
		evaluation.SchemaVersion).Scan(
		&claim.EvaluationID,
		&claim.EvaluationRevisionID,
		&claim.OwnerUserID,
		&claim.RevisionCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.IELTSAcousticSnapshotClaim{}, false, nil
	}
	if err != nil {
		return scoring.IELTSAcousticSnapshotClaim{}, false, fmt.Errorf(
			"find pending IELTS acoustic snapshot: %w",
			err,
		)
	}
	claim.Snapshot, err = scanEvidenceSnapshot(r.pool.QueryRow(
		ctx,
		evidenceSnapshotSelect+`
			FROM evaluation_evidence_snapshots AS snapshot
			JOIN evaluation_ledgers AS ledger
			  ON ledger.input_snapshot_id = snapshot.id
			 AND ledger.owner_user_id = snapshot.owner_user_id
			JOIN evaluation_revisions AS revision
			  ON revision.evaluation_id = ledger.id
			 AND revision.owner_user_id = ledger.owner_user_id
			WHERE ledger.id = $1
			  AND revision.id = $2
			  AND ledger.owner_user_id = $3
			  AND ledger.practice_session_id = snapshot.practice_session_id
			  AND ledger.input_revision = snapshot.input_revision
			  AND ledger.scope = snapshot.scope
			  AND ledger.scene_type = snapshot.scene_type
		`,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
		claim.OwnerUserID,
	))
	if err != nil {
		return scoring.IELTSAcousticSnapshotClaim{}, false, fmt.Errorf(
			"read pending IELTS acoustic base snapshot: %w",
			err,
		)
	}
	claim.RevisionCreatedAt = claim.RevisionCreatedAt.UTC()
	if !claim.Valid() {
		return scoring.IELTSAcousticSnapshotClaim{}, false,
			evaluation.ErrInvalidRequest
	}
	return claim, true, nil
}

func (r *PostgresRepository) EnsureIELTSAcousticSnapshot(
	ctx context.Context,
	claim scoring.IELTSAcousticSnapshotClaim,
	draft scoring.IELTSAcousticSnapshot,
) (scoring.IELTSAcousticSnapshot, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() ||
		!draft.ValidFor(claim.Snapshot) ||
		draft.EvaluationID != claim.EvaluationID ||
		draft.OwnerUserID != claim.OwnerUserID {
		return scoring.IELTSAcousticSnapshot{}, false,
			evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, fmt.Errorf(
			"begin IELTS acoustic snapshot ensure: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, claim.OwnerUserID); err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scoring.IELTSAcousticSnapshot{}, false,
				scoring.ErrRuntimeLeaseLost
		}
		return scoring.IELTSAcousticSnapshot{}, false, err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scoring.IELTSAcousticSnapshot{}, false,
				scoring.ErrRuntimeLeaseLost
		}
		return scoring.IELTSAcousticSnapshot{}, false, err
	}
	var status evaluation.Status
	err = tx.QueryRow(ctx, `
		SELECT state.evaluation_status
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		WHERE ledger.id = $1
		  AND revision.id = $2
		  AND ledger.owner_user_id = $3
		  AND ledger.input_snapshot_id = $4
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'IELTS_SPEAKING'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $5
		  AND revision.pipeline_version = $6
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.revision > revision.revision
		  )
		FOR UPDATE OF state
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID, claim.Snapshot.ID,
		scoring.IELTSSpeakingShadowStrategyRef,
		scoring.IELTSSpeakingShadowPipelineVersion).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.IELTSAcousticSnapshot{}, false,
			scoring.ErrRuntimeLeaseLost
	}
	if err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, fmt.Errorf(
			"lock IELTS acoustic snapshot revision: %w",
			err,
		)
	}
	if status != evaluation.StatusValidating && status != evaluation.StatusQueued {
		return scoring.IELTSAcousticSnapshot{}, false,
			scoring.ErrRuntimeLeaseLost
	}
	command, err := tx.Exec(ctx, `
		INSERT INTO evaluation_ielts_speaking_acoustic_snapshots (
			id,
			evaluation_id,
			owner_user_id,
			input_snapshot_id,
			input_snapshot_hash,
			schema_version,
			resolution,
			snapshot_hash,
			canonical_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (evaluation_id) DO NOTHING
	`, draft.ID, draft.EvaluationID, draft.OwnerUserID,
		draft.InputSnapshotID, draft.InputSnapshotHash[:],
		scoring.IELTSAcousticSnapshotSchemaVersion,
		draft.Resolution, draft.SnapshotHash[:], string(draft.Payload))
	if err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, fmt.Errorf(
			"insert IELTS acoustic snapshot: %w",
			err,
		)
	}
	stored, err := getIELTSAcousticSnapshot(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.Snapshot,
	)
	if err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, err
	}
	if stored.ID != draft.ID || stored.SnapshotHash != draft.SnapshotHash ||
		stored.Resolution != draft.Resolution ||
		!bytes.Equal(stored.Payload, draft.Payload) {
		return scoring.IELTSAcousticSnapshot{}, false,
			scoring.ErrRuntimeConfigurationConflict
	}
	if status == evaluation.StatusValidating {
		updated, updateErr := tx.Exec(ctx, `
			UPDATE evaluation_revision_states
			SET evaluation_status = 'QUEUED',
			    updated_at = transaction_timestamp()
			WHERE evaluation_id = $1
			  AND revision_id = $2
			  AND owner_user_id = $3
			  AND evaluation_status = 'VALIDATING'
			  AND completed_at IS NULL
		`, claim.EvaluationID, claim.EvaluationRevisionID,
			claim.OwnerUserID)
		if updateErr != nil {
			return scoring.IELTSAcousticSnapshot{}, false, fmt.Errorf(
				"queue frozen IELTS acoustic snapshot: %w",
				updateErr,
			)
		}
		if updated.RowsAffected() != 1 {
			return scoring.IELTSAcousticSnapshot{}, false,
				scoring.ErrRuntimeLeaseLost
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return scoring.IELTSAcousticSnapshot{}, false, fmt.Errorf(
			"commit IELTS acoustic snapshot ensure: %w",
			err,
		)
	}
	return stored, command.RowsAffected() == 0, nil
}

func (r *PostgresRepository) FailIELTSAcousticSnapshot(
	ctx context.Context,
	claim scoring.IELTSAcousticSnapshotClaim,
) error {
	if r == nil || r.pool == nil || ctx == nil || !claim.Valid() {
		return evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin IELTS acoustic snapshot failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, claim.OwnerUserID); err != nil {
		return err
	}
	if err := lockEvaluationLedgerAndRevisionRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scoring.ErrRuntimeLeaseLost
		}
		return err
	}
	if err := lockEvaluationRevisionRuntimeRows(
		ctx,
		tx,
		claim.OwnerUserID,
		claim.EvaluationID,
		claim.EvaluationRevisionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scoring.ErrRuntimeLeaseLost
		}
		return err
	}

	var revisionStatus, deliveryStatus, outboxFailureCode string
	var moduleStatus, moduleFailureCode string
	err = tx.QueryRow(ctx, `
		SELECT
			state.evaluation_status,
			outbox.delivery_status,
			coalesce(outbox.last_failure_code, ''),
			coalesce(run.run_status, ''),
			coalesce(run.last_failure_code, '')
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		JOIN evaluation_evidence_snapshots AS snapshot
		  ON snapshot.id = ledger.input_snapshot_id
		 AND snapshot.owner_user_id = ledger.owner_user_id
		JOIN evaluation_outbox AS outbox
		  ON outbox.evaluation_id = ledger.id
		 AND outbox.evaluation_revision_id = revision.id
		 AND outbox.owner_user_id = ledger.owner_user_id
		 AND outbox.channel = 'SCENE'
		LEFT JOIN evaluation_module_runs AS run
		  ON run.outbox_id = outbox.id
		 AND run.evaluation_id = ledger.id
		 AND run.evaluation_revision_id = revision.id
		 AND run.owner_user_id = ledger.owner_user_id
		 AND run.channel = outbox.channel
		WHERE ledger.id = $1
		  AND revision.id = $2
		  AND ledger.owner_user_id = $3
		  AND revision.created_at = $4
		  AND ledger.input_snapshot_id = $5
		  AND snapshot.snapshot_hash = $6
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'IELTS_SPEAKING'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $7
		  AND revision.pipeline_version = $8
		  AND revision.schema_version = $9
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.revision > revision.revision
		  )
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID, claim.RevisionCreatedAt,
		claim.Snapshot.ID, claim.Snapshot.SnapshotHash[:],
		scoring.IELTSSpeakingShadowStrategyRef,
		scoring.IELTSSpeakingShadowPipelineVersion,
		evaluation.SchemaVersion).Scan(
		&revisionStatus,
		&deliveryStatus,
		&outboxFailureCode,
		&moduleStatus,
		&moduleFailureCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.ErrRuntimeLeaseLost
	}
	if err != nil {
		return fmt.Errorf("read IELTS acoustic snapshot failure state: %w", err)
	}
	if revisionStatus == "FAILED" {
		moduleFailed := moduleStatus == "" ||
			(moduleStatus == "FAILED" &&
				moduleFailureCode == scoring.IELTSAcousticEvidenceFailureCode)
		if deliveryStatus == "FAILED" &&
			outboxFailureCode == scoring.IELTSAcousticEvidenceFailureCode &&
			moduleFailed {
			return nil
		}
		return scoring.ErrRuntimeConfigurationConflict
	}
	if revisionStatus != "VALIDATING" {
		return scoring.ErrRuntimeLeaseLost
	}
	if deliveryStatus != "PENDING" ||
		(moduleStatus != "" && moduleStatus != "RUNNING") {
		return scoring.ErrRuntimeConfigurationConflict
	}

	if moduleStatus == "RUNNING" {
		updated, updateErr := tx.Exec(ctx, `
			UPDATE evaluation_module_runs
			SET run_status = 'FAILED',
			    last_failure_code = $1,
			    updated_at = transaction_timestamp(),
			    completed_at = transaction_timestamp()
			WHERE evaluation_id = $2
			  AND evaluation_revision_id = $3
			  AND owner_user_id = $4
			  AND channel = 'SCENE'
			  AND strategy_ref = $5
			  AND run_status = 'RUNNING'
		`, scoring.IELTSAcousticEvidenceFailureCode,
			claim.EvaluationID, claim.EvaluationRevisionID,
			claim.OwnerUserID,
			scoring.IELTSSpeakingShadowStrategyRef)
		if updateErr != nil {
			return fmt.Errorf("fail IELTS acoustic module run: %w", updateErr)
		}
		if updated.RowsAffected() != 1 {
			return scoring.ErrRuntimeLeaseLost
		}
	}
	outboxUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_outbox
		SET delivery_status = 'FAILED',
		    lease_expires_at = NULL,
		    last_failure_code = $1,
		    failed_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE evaluation_id = $2
		  AND evaluation_revision_id = $3
		  AND owner_user_id = $4
		  AND channel = 'SCENE'
		  AND delivery_status = 'PENDING'
	`, scoring.IELTSAcousticEvidenceFailureCode,
		claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID)
	if err != nil {
		return fmt.Errorf("fail IELTS acoustic outbox: %w", err)
	}
	if outboxUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
	}
	stateUpdate, err := tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'FAILED',
		    is_final = false,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND revision_id = $2
		  AND owner_user_id = $3
		  AND evaluation_status = 'VALIDATING'
		  AND completed_at IS NULL
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID)
	if err != nil {
		return fmt.Errorf("fail IELTS acoustic revision: %w", err)
	}
	if stateUpdate.RowsAffected() != 1 {
		return scoring.ErrRuntimeLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit IELTS acoustic snapshot failure: %w", err)
	}
	return nil
}

func getIELTSAcousticSnapshot(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	evaluationID string,
	base evidence.EvidenceSnapshot,
) (scoring.IELTSAcousticSnapshot, error) {
	var result scoring.IELTSAcousticSnapshot
	var inputHash, snapshotHash []byte
	var schemaVersion string
	err := db.QueryRow(ctx, `
		SELECT
			id,
			evaluation_id::text,
			owner_user_id::text,
			input_snapshot_id,
			input_snapshot_hash,
			schema_version,
			resolution,
			snapshot_hash,
			canonical_payload,
			created_at
		FROM evaluation_ielts_speaking_acoustic_snapshots
		WHERE evaluation_id = $1
		  AND owner_user_id = $2
	`, evaluationID, ownerUserID).Scan(
		&result.ID,
		&result.EvaluationID,
		&result.OwnerUserID,
		&result.InputSnapshotID,
		&inputHash,
		&schemaVersion,
		&result.Resolution,
		&snapshotHash,
		&result.Payload,
		&result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scoring.IELTSAcousticSnapshot{}, evaluation.ErrNotFound
	}
	if err != nil {
		return scoring.IELTSAcousticSnapshot{}, fmt.Errorf(
			"read IELTS acoustic snapshot: %w",
			err,
		)
	}
	if len(inputHash) != sha256.Size || len(snapshotHash) != sha256.Size ||
		schemaVersion != scoring.IELTSAcousticSnapshotSchemaVersion {
		return scoring.IELTSAcousticSnapshot{},
			scoring.ErrRuntimeConfigurationConflict
	}
	copy(result.InputSnapshotHash[:], inputHash)
	copy(result.SnapshotHash[:], snapshotHash)
	result.CreatedAt = result.CreatedAt.UTC()
	if !result.ValidFor(base) {
		return scoring.IELTSAcousticSnapshot{},
			scoring.ErrRuntimeConfigurationConflict
	}
	return result, nil
}

func nullableAcousticSnapshotID(
	snapshot *scoring.IELTSAcousticSnapshot,
) any {
	if snapshot == nil {
		return nil
	}
	return snapshot.ID
}

func nullableAcousticSnapshotHash(
	snapshot *scoring.IELTSAcousticSnapshot,
) any {
	if snapshot == nil {
		return nil
	}
	return snapshot.SnapshotHash[:]
}

func nullableDigest(value [sha256.Size]byte) any {
	if value == ([sha256.Size]byte{}) {
		return nil
	}
	return value[:]
}
