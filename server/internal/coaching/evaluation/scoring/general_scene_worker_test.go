package scoring

import (
	"context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestGeneralSceneWorkerClaimsAcrossSceneTypesAndCompletes(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneOverseasWorkplace,
		scene.PracticeExperienceWorkplace,
		scene.SceneCategoryWorkplaceGeneral,
		scene.PracticeModeFullSimulation,
		"The release is delayed because one integration test is failing.",
	)
	configuration := generalSceneRuntimeConfigurationFixture()
	repository := &generalSceneRuntimeRepositoryStub{
		claim: generalSceneClaimFixture(snapshot, configuration),
	}
	worker, err := NewGeneralSceneWorker(
		repository,
		NewGeneralSceneEngine(&generalSceneProviderStub{}),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (GeneralSceneSweepResult{Claimed: 1, Completed: 1}) ||
		repository.completed != 1 || repository.failed != 0 {
		t.Fatalf("sweep=%#v repository=%#v", sweep, repository)
	}
}

func generalSceneRuntimeConfigurationFixture() GeneralSceneRuntimeConfiguration {
	return GeneralSceneRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Minute,
		StrategyRef:     GeneralSceneStrategyRef,
		PipelineVersion: GeneralScenePipelineVersion,
		FullConfigHash:  [32]byte{1},
		PromptVersion:   GeneralScenePromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
}

func generalSceneClaimFixture(
	snapshot evidence.EvidenceSnapshot,
	configuration GeneralSceneRuntimeConfiguration,
) GeneralSceneClaim {
	return GeneralSceneClaim{
		OutboxID:             "20000000-0000-4000-8000-000000000001",
		ModuleRunID:          "20000000-0000-4000-8000-000000000002",
		EvaluationID:         "20000000-0000-4000-8000-000000000003",
		EvaluationRevisionID: "20000000-0000-4000-8000-000000000004",
		OwnerUserID:          snapshot.OwnerUserID,
		Revision:             1,
		SceneType:            snapshot.SceneType,
		StrategyRef:          configuration.StrategyRef,
		PipelineVersion:      configuration.PipelineVersion,
		AttemptCount:         1,
		FencingToken:         1,
		LeaseExpiresAt:       time.Now().UTC().Add(time.Minute),
		FullConfigHash:       configuration.FullConfigHash,
		PromptVersion:        configuration.PromptVersion,
		Provider:             configuration.Provider,
		Model:                configuration.Model,
		Snapshot:             snapshot,
	}
}

type generalSceneRuntimeRepositoryStub struct {
	claim     GeneralSceneClaim
	claimed   bool
	completed int
	failed    int
}

func (repository *generalSceneRuntimeRepositoryStub) ClaimGeneralScene(
	_ context.Context,
	sceneType evaluation.SceneType,
	_ GeneralSceneRuntimeConfiguration,
) (GeneralSceneClaim, bool, error) {
	if repository.claimed || sceneType != repository.claim.SceneType {
		return GeneralSceneClaim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *generalSceneRuntimeRepositoryStub) CompleteGeneralScene(
	context.Context,
	GeneralSceneClaim,
	GeneralSceneResult,
) error {
	repository.completed++
	return nil
}

func (repository *generalSceneRuntimeRepositoryStub) FailGeneralScene(
	context.Context,
	GeneralSceneClaim,
	GeneralSceneFailure,
	GeneralSceneRuntimeConfiguration,
) (GeneralSceneRuntimeStatus, error) {
	repository.failed++
	return GeneralSceneRuntimeFailed, nil
}
