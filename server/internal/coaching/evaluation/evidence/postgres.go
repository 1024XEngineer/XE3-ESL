package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) EnsureEvidenceSnapshot(
	ctx context.Context,
	command EnsureEvidenceSnapshotCommand,
) (EvidenceSnapshot, bool, error) {
	if r == nil || r.pool == nil || ctx == nil {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	command, err := normalizeEvidenceSnapshotCommand(command)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EvidenceSnapshot{}, false, fmt.Errorf(
			"begin EvidenceSnapshot ensure: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockActiveOwner(ctx, tx, command.OwnerUserID); err != nil {
		return EvidenceSnapshot{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended(
				jsonb_build_array($1::text, $2::text, $3::text)::text,
				0
			)
		)
	`, command.OwnerUserID, command.PracticeSessionID, command.Scope); err != nil {
		return EvidenceSnapshot{}, false, fmt.Errorf(
			"lock EvidenceSnapshot revision chain: %w",
			err,
		)
	}

	snapshot, err := selectEvidenceSnapshotBySource(
		ctx,
		tx,
		command.OwnerUserID,
		command.PracticeSessionID,
		command.Scope,
		command.SourceManifestHash,
	)
	switch {
	case err == nil:
		if snapshot.ID != command.SnapshotID ||
			snapshot.SceneType != command.SceneType ||
			!bytes.Equal(
				snapshot.Payload,
				command.CanonicalPayload,
			) {
			return EvidenceSnapshot{}, false, evaluation.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return EvidenceSnapshot{}, false, fmt.Errorf(
				"commit EvidenceSnapshot replay: %w",
				err,
			)
		}
		return snapshot, true, nil
	case errors.Is(err, evaluation.ErrNotFound):
	default:
		return EvidenceSnapshot{}, false, err
	}
	if err := lockCurrentEvidenceSources(ctx, tx, command); err != nil {
		return EvidenceSnapshot{}, false, err
	}
	if r.afterEvidenceSourceFence != nil {
		r.afterEvidenceSourceFence()
	}

	var revision int
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(max(input_revision), 0) + 1
		FROM evaluation_evidence_snapshots
		WHERE owner_user_id = $1
		  AND practice_session_id = $2
		  AND scope = $3
	`, command.OwnerUserID, command.PracticeSessionID, command.Scope).Scan(
		&revision,
	); err != nil {
		return EvidenceSnapshot{}, false, fmt.Errorf(
			"derive EvidenceSnapshot revision: %w",
			err,
		)
	}
	snapshotHash := sha256.Sum256(command.CanonicalPayload)
	row := tx.QueryRow(ctx, `
		INSERT INTO evaluation_evidence_snapshots (
			id,
			owner_user_id,
			practice_session_id,
			scope,
			scene_type,
			input_revision,
			source_manifest_hash,
			snapshot_hash,
			canonical_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING
			id::text,
			owner_user_id::text,
			practice_session_id,
			scope,
			scene_type,
			input_revision,
			source_manifest_hash,
			snapshot_hash,
			canonical_payload,
			created_at
	`, command.SnapshotID, command.OwnerUserID,
		command.PracticeSessionID, command.Scope, command.SceneType,
		revision, command.SourceManifestHash[:], snapshotHash[:],
		command.CanonicalPayload)
	snapshot, err = scanEvidenceSnapshot(row)
	if err != nil {
		return EvidenceSnapshot{}, false, fmt.Errorf(
			"insert EvidenceSnapshot: %w",
			err,
		)
	}
	if err := tx.Commit(ctx); err != nil {
		return EvidenceSnapshot{}, false, fmt.Errorf(
			"commit EvidenceSnapshot ensure: %w",
			err,
		)
	}
	return snapshot, false, nil
}

func (r *PostgresRepository) GetEvidenceSnapshot(
	ctx context.Context,
	ownerUserID string,
	snapshotID string,
) (EvidenceSnapshot, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validIdentifier(snapshotID) {
		return EvidenceSnapshot{}, evaluation.ErrInvalidRequest
	}
	snapshot, err := selectEvidenceSnapshotByID(
		ctx,
		r.pool,
		ownerUserID,
		snapshotID,
	)
	if errors.Is(err, evaluation.ErrNotFound) {
		return EvidenceSnapshot{}, evaluation.ErrNotFound
	}
	return snapshot, err
}

func (r *PostgresRepository) GetEvaluationEvidence(
	ctx context.Context,
	ownerUserID string,
	snapshotID string,
) (evaluation.EvidenceReference, error) {
	snapshot, err := r.GetEvidenceSnapshot(ctx, ownerUserID, snapshotID)
	if err != nil {
		return evaluation.EvidenceReference{}, err
	}
	reference := evaluation.EvidenceReference{
		ID:                snapshot.ID,
		OwnerUserID:       snapshot.OwnerUserID,
		PracticeSessionID: snapshot.PracticeSessionID,
		InputRevision:     snapshot.InputRevision,
		Scope:             snapshot.Scope,
		SceneType:         snapshot.SceneType,
	}
	if !reference.Valid() {
		return evaluation.EvidenceReference{}, evaluation.ErrInvalidRequest
	}
	return reference, nil
}

func selectEvidenceSnapshotBySource(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	practiceSessionID string,
	scope evaluation.Scope,
	sourceManifestHash [sha256.Size]byte,
) (EvidenceSnapshot, error) {
	row := db.QueryRow(ctx, evidenceSnapshotSelect+`
		FROM evaluation_evidence_snapshots AS snapshot
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.practice_session_id = $2
		  AND snapshot.scope = $3
		  AND snapshot.source_manifest_hash = $4
	`, ownerUserID, practiceSessionID, scope, sourceManifestHash[:])
	return scanEvidenceSnapshotResult(row)
}

func selectEvidenceSnapshotByID(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	snapshotID string,
) (EvidenceSnapshot, error) {
	row := db.QueryRow(ctx, evidenceSnapshotSelect+`
		FROM evaluation_evidence_snapshots AS snapshot
		WHERE snapshot.id = $1
		  AND snapshot.owner_user_id = $2
	`, snapshotID, ownerUserID)
	return scanEvidenceSnapshotResult(row)
}

func scanEvidenceSnapshotResult(row pgx.Row) (EvidenceSnapshot, error) {
	snapshot, err := scanEvidenceSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return EvidenceSnapshot{}, evaluation.ErrNotFound
	}
	if err != nil {
		return EvidenceSnapshot{}, fmt.Errorf("read EvidenceSnapshot: %w", err)
	}
	return snapshot, nil
}

func scanEvidenceSnapshot(row pgx.Row) (EvidenceSnapshot, error) {
	var snapshot EvidenceSnapshot
	var sourceManifestHash []byte
	var snapshotHash []byte
	err := row.Scan(
		&snapshot.ID,
		&snapshot.OwnerUserID,
		&snapshot.PracticeSessionID,
		&snapshot.Scope,
		&snapshot.SceneType,
		&snapshot.InputRevision,
		&sourceManifestHash,
		&snapshotHash,
		&snapshot.Payload,
		&snapshot.CreatedAt,
	)
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	if len(sourceManifestHash) != sha256.Size ||
		len(snapshotHash) != sha256.Size {
		return EvidenceSnapshot{}, evaluation.ErrInvalidRequest
	}
	copy(snapshot.SourceManifestHash[:], sourceManifestHash)
	copy(snapshot.SnapshotHash[:], snapshotHash)
	canonical, err := CanonicalPayload(snapshot.Payload)
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	snapshot.Payload = canonical
	if !snapshot.Valid() {
		return EvidenceSnapshot{}, evaluation.ErrInvalidRequest
	}
	return snapshot, nil
}

const evidenceSnapshotSelect = `
	SELECT
		snapshot.id::text,
		snapshot.owner_user_id::text,
		snapshot.practice_session_id,
		snapshot.scope,
		snapshot.scene_type,
		snapshot.input_revision,
		snapshot.source_manifest_hash,
		snapshot.snapshot_hash,
		snapshot.canonical_payload,
		snapshot.created_at
`
