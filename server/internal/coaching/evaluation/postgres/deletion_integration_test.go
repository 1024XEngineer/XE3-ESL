package postgres

import (
	"context"
	"errors"
	"testing"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

func TestPostgresEvaluationDeletionPurgesImmutableUserData(t *testing.T) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	ctx := context.Background()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led the migration and reduced release risk.",
	)
	persistEvidenceSnapshotFixture(t, pool, snapshot)
	if _, err := pool.Exec(ctx, `
		INSERT INTO evaluation_ledgers (
			owner_user_id,
			root_idempotency_key,
			root_request_fingerprint,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scope,
			scene_type
		)
		VALUES (
			$1,
			decode(repeat('ab', 32), 'hex'),
			decode(repeat('cd', 32), 'hex'),
			$2,
			$3,
			$4,
			$5,
			$6
		)
	`, testOwnerA, snapshot.PracticeSessionID, snapshot.ID,
		snapshot.InputRevision, snapshot.Scope, snapshot.SceneType); err != nil {
		t.Fatalf("create Evaluation ledger: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("set owner deleting: %v", err)
	}
	command := evaluationcore.DeleteUserDataCommand{
		OwnerUserID:        testOwnerA,
		DeletionGeneration: 2,
	}
	repository := NewPostgresDeletionRepository(pool)
	if err := repository.DeleteUserData(ctx, command); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(ctx, command); err != nil {
		t.Fatalf("idempotent DeleteUserData: %v", err)
	}
	stale := command
	stale.DeletionGeneration = 1
	if err := repository.DeleteUserData(ctx, stale); !errors.Is(
		err,
		evaluationcore.ErrDeletionGenerationStale,
	) {
		t.Fatalf("stale deletion generation error = %v", err)
	}
	var snapshots, ledgers int
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM evaluation_evidence_snapshots),
			(SELECT count(*) FROM evaluation_ledgers),
			(SELECT deletion_generation
			 FROM evaluation_deletion_fences
			 WHERE owner_user_id = $1)
	`, testOwnerA).Scan(&snapshots, &ledgers, &generation); err != nil {
		t.Fatalf("inspect deleted Evaluation data: %v", err)
	}
	if snapshots != 0 || ledgers != 0 || generation != 2 {
		t.Fatalf(
			"deleted counts snapshots=%d ledgers=%d generation=%d",
			snapshots,
			ledgers,
			generation,
		)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'active'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("restore active status for fence test: %v", err)
	}
	_, _, err := evidence.NewPostgresRepository(pool).EnsureEvidenceSnapshot(
		ctx,
		evidence.EnsureEvidenceSnapshotCommand{
			SnapshotID:         snapshot.ID,
			OwnerUserID:        snapshot.OwnerUserID,
			PracticeSessionID:  snapshot.PracticeSessionID,
			Scope:              snapshot.Scope,
			SceneType:          snapshot.SceneType,
			SourceManifestHash: snapshot.SourceManifestHash,
			CanonicalPayload:   snapshot.Payload,
		},
	)
	if !errors.Is(err, evaluationcore.ErrAccountUnavailable) {
		t.Fatalf("persisted deletion fence Ensure error = %v", err)
	}
}
