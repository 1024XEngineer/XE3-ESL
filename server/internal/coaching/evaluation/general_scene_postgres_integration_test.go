package evaluation

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresGeneralSceneCompletionIsIdempotentAndDeletesCleanly(
	t *testing.T,
) {
	pool, repository, claim := prepareGeneralScenePostgresRuntime(
		t,
		"The release is delayed because one integration test is failing.",
	)
	result, err := NewGeneralSceneEngine(&generalSceneProviderStub{}).Evaluate(
		context.Background(),
		claim.Snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteGeneralScene(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("complete general Scene: %v", err)
	}
	if err := repository.CompleteGeneralScene(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("replay general Scene completion: %v", err)
	}
	assertGeneralSceneProjectionCounts(t, pool, claim.OwnerUserID, 1, 4, 4)
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, claim.OwnerUserID); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		DeleteUserDataCommand{
			OwnerUserID:        claim.OwnerUserID,
			DeletionGeneration: 1,
		},
	); err != nil {
		t.Fatalf("delete general Scene data: %v", err)
	}
	assertGeneralSceneProjectionCounts(t, pool, claim.OwnerUserID, 0, 0, 0)
}

func TestPostgresInsufficientGeneralSceneDoesNotChangeLearningProfile(
	t *testing.T,
) {
	pool, repository, claim := prepareGeneralScenePostgresRuntime(t, "Okay.")
	provider := &generalSceneProviderStub{}
	result, err := NewGeneralSceneEngine(provider).Evaluate(
		context.Background(),
		claim.Snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 ||
		result.ScoreabilityStatus != ReportScoreabilityInsufficient {
		t.Fatalf("provider calls=%d result=%#v", provider.calls, result)
	}
	if err := repository.CompleteGeneralScene(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("complete insufficient general Scene: %v", err)
	}
	assertGeneralSceneProjectionCounts(t, pool, claim.OwnerUserID, 1, 0, 0)
}

func prepareGeneralScenePostgresRuntime(
	t *testing.T,
	transcript string,
) (*pgxpool.Pool, *PostgresRepository, GeneralSceneClaim) {
	t.Helper()
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	repository := NewPostgresRepository(pool)
	snapshot := generalSceneTestSnapshot(
		t,
		SceneOverseasWorkplace,
		scene.SceneFamilyWorkplace,
		scene.SceneModelProgressAndRiskUpdate,
		transcript,
	)
	command := EnsureEvidenceSnapshotCommand{
		SnapshotID:         snapshot.ID,
		OwnerUserID:        snapshot.OwnerUserID,
		PracticeSessionID:  snapshot.PracticeSessionID,
		Scope:              snapshot.Scope,
		SceneType:          snapshot.SceneType,
		SourceManifestHash: snapshot.SourceManifestHash,
		CanonicalPayload:   snapshot.Payload,
	}
	installEvidenceAuthorities(t, pool, command)
	persisted, replayed, err := repository.EnsureEvidenceSnapshot(
		context.Background(),
		command,
	)
	if err != nil || replayed {
		t.Fatalf("ensure general Scene evidence replayed=%t error=%v", replayed, err)
	}
	created, replayed, err := NewService(repository, repository).CreateCompleted(
		context.Background(),
		testOwnerA,
		CreateRequest{
			PracticeSessionID: persisted.PracticeSessionID,
			InputSnapshotID:   persisted.ID,
			InputRevision:     persisted.InputRevision,
			Scope:             ScopeSession,
			SceneType:         SceneOverseasWorkplace,
			Channels:          []Channel{ChannelScene},
			SceneStrategyRef:  GeneralSceneStrategyRef,
			PipelineVersion:   GeneralScenePipelineVersion,
		},
	)
	if err != nil || replayed {
		t.Fatalf("create general Scene Evaluation replayed=%t error=%v", replayed, err)
	}
	configuration := GeneralSceneRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   5 * time.Second,
		StrategyRef:     GeneralSceneStrategyRef,
		PipelineVersion: GeneralScenePipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("general-scene-integration-config/v1"),
		),
		PromptVersion: GeneralScenePromptVersion,
		Provider:      "qianwen",
		Model:         "qwen-plus",
	}
	claim, acquired, err := repository.ClaimGeneralScene(
		context.Background(),
		SceneOverseasWorkplace,
		configuration,
	)
	if err != nil || !acquired || claim.EvaluationID != created.ID {
		t.Fatalf("claim general Scene acquired=%t claim=%#v error=%v", acquired, claim, err)
	}
	return pool, repository, claim
}

func assertGeneralSceneProjectionCounts(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
	wantReports int,
	wantContributions int,
	wantDimensions int,
) {
	t.Helper()
	var results, reports, contributions, dimensions int
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM evaluation_general_scene_results
			 WHERE owner_user_id = $1),
			(SELECT count(*) FROM evaluation_formal_reports
			 WHERE owner_user_id = $1),
			(SELECT count(*) FROM learning_profile_contributions
			 WHERE owner_user_id = $1),
			(SELECT count(*) FROM learning_profile_dimensions
			 WHERE owner_user_id = $1)
	`, ownerUserID).Scan(
		&results,
		&reports,
		&contributions,
		&dimensions,
	); err != nil {
		t.Fatal(err)
	}
	if results != wantReports || reports != wantReports ||
		contributions != wantContributions || dimensions != wantDimensions {
		t.Fatalf(
			"general Scene counts result/report/contribution/dimension=%d/%d/%d/%d, want %d/%d/%d/%d",
			results,
			reports,
			contributions,
			dimensions,
			wantReports,
			wantReports,
			wantContributions,
			wantDimensions,
		)
	}
}
