package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/jackc/pgx/v5"
)

func selectEvidenceSnapshotByID(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	snapshotID string,
) (evidence.EvidenceSnapshot, error) {
	snapshot, err := scanEvidenceSnapshot(db.QueryRow(ctx, evidenceSnapshotSelect+`
		FROM evaluation_evidence_snapshots AS snapshot
		WHERE snapshot.id = $1
		  AND snapshot.owner_user_id = $2
	`, snapshotID, ownerUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return evidence.EvidenceSnapshot{}, evaluation.ErrNotFound
	}
	if err != nil {
		return evidence.EvidenceSnapshot{}, fmt.Errorf(
			"read EvidenceSnapshot: %w",
			err,
		)
	}
	return snapshot, nil
}

func scanEvidenceSnapshot(row pgx.Row) (evidence.EvidenceSnapshot, error) {
	var snapshot evidence.EvidenceSnapshot
	var sourceManifestHash []byte
	var snapshotHash []byte
	if err := row.Scan(
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
	); err != nil {
		return evidence.EvidenceSnapshot{}, err
	}
	if len(sourceManifestHash) != sha256.Size ||
		len(snapshotHash) != sha256.Size {
		return evidence.EvidenceSnapshot{}, evaluation.ErrInvalidRequest
	}
	copy(snapshot.SourceManifestHash[:], sourceManifestHash)
	copy(snapshot.SnapshotHash[:], snapshotHash)
	canonical, err := evidence.CanonicalPayload(snapshot.Payload)
	if err != nil {
		return evidence.EvidenceSnapshot{}, err
	}
	snapshot.Payload = canonical
	if !snapshot.Valid() {
		return evidence.EvidenceSnapshot{}, evaluation.ErrInvalidRequest
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
