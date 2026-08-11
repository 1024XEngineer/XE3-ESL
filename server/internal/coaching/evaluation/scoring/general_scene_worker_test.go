package scoring

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
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
		repository.completed != 1 || repository.failed != 0 ||
		repository.atomicLoads != 0 ||
		len(repository.atomicAttempts) != 0 {
		t.Fatalf("sweep=%#v repository=%#v", sweep, repository)
	}
}

func TestGeneralSceneWorkerCompletesInsufficientIELTSWithoutAtomicCalls(
	t *testing.T,
) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"Okay.",
	)
	configuration := generalSceneRuntimeConfigurationFixture()
	repository := &generalSceneRuntimeRepositoryStub{
		claim: generalSceneClaimFixture(snapshot, configuration),
	}
	provider := &generalSceneProviderStub{}
	worker, err := NewGeneralSceneWorker(
		repository,
		NewGeneralSceneEngine(provider),
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
		repository.completed != 1 || repository.atomicLoads != 0 ||
		len(repository.atomicAttempts) != 0 || provider.calls != 0 ||
		len(provider.atomicInputs()) != 0 {
		t.Fatalf("sweep=%#v repository=%#v provider=%#v", sweep, repository, provider)
	}
}

func TestGeneralSceneWorkerResumesOnlyMissingIELTSAtoms(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I read every evening because it helps me relax.",
	)
	configuration := generalSceneRuntimeConfigurationFixture()
	first := generalSceneClaimFixture(snapshot, configuration)
	second := first
	second.AttemptCount = 2
	second.FencingToken = 2
	repository := &generalSceneRuntimeRepositoryStub{
		claims:          []GeneralSceneClaim{first, second},
		readyAtoms:      make(map[GeneralSceneAtomicKey]GeneralSceneAtomicResult),
		failureStatuses: []GeneralSceneRuntimeStatus{GeneralSceneRuntimePending},
	}
	provider := &generalSceneProviderStub{
		failOnce: GeneralSceneDimensionClarity,
	}
	firstWorker, err := NewGeneralSceneWorker(
		repository,
		NewGeneralSceneEngine(provider),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstSweep, err := firstWorker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if firstSweep != (GeneralSceneSweepResult{Claimed: 1, Retried: 1}) ||
		len(repository.readyAtoms) != 3 || len(repository.atomicAttempts) != 4 {
		t.Fatalf("first sweep=%#v repository=%#v", firstSweep, repository)
	}

	secondWorker, err := NewGeneralSceneWorker(
		repository,
		NewGeneralSceneEngine(provider),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	secondSweep, err := secondWorker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if secondSweep != (GeneralSceneSweepResult{Claimed: 1, Completed: 1}) ||
		repository.completed != 1 || len(repository.readyAtoms) != 4 ||
		len(repository.atomicAttempts) != 5 {
		t.Fatalf("second sweep=%#v repository=%#v", secondSweep, repository)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	for _, dimension := range generalSceneDimensionOrder {
		want := 1
		if dimension == GeneralSceneDimensionClarity {
			want = 2
		}
		if provider.atomicCalls[dimension] != want {
			t.Fatalf(
				"dimension %s calls=%d want=%d",
				dimension,
				provider.atomicCalls[dimension],
				want,
			)
		}
	}
}

func TestGeneralSceneProviderRejectionStageIsStableAndContentFree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cause error
		want  string
	}{
		{name: "invalid JSON", cause: errGeneralSceneProviderInvalidJSON, want: "json_decode"},
		{name: "schema mismatch", cause: errGeneralSceneProviderSchemaMismatch, want: "schema_validation"},
		{name: "semantic mismatch", cause: ErrInvalidGeneralSceneResult, want: "semantic_validation"},
		{name: "invalid request", cause: evaluation.ErrInvalidRequest, want: "request_validation"},
		{name: "unrelated", cause: context.Canceled, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := generalSceneProviderRejectionStage(test.cause); got != test.want {
				t.Fatalf("stage = %q, want %q", got, test.want)
			}
		})
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
	claim            GeneralSceneClaim
	claims           []GeneralSceneClaim
	claimIndex       int
	claimed          bool
	completed        int
	failed           int
	readyAtoms       map[GeneralSceneAtomicKey]GeneralSceneAtomicResult
	atomicLoads      int
	atomicAttempts   []GeneralSceneAtomicAttempt
	failureStatuses  []GeneralSceneRuntimeStatus
	failureStatusIdx int
}

func (repository *generalSceneRuntimeRepositoryStub) ClaimGeneralScene(
	_ context.Context,
	sceneType evaluation.SceneType,
	_ GeneralSceneRuntimeConfiguration,
) (GeneralSceneClaim, bool, error) {
	if len(repository.claims) > 0 {
		if repository.claimIndex >= len(repository.claims) ||
			sceneType != repository.claims[repository.claimIndex].SceneType {
			return GeneralSceneClaim{}, false, nil
		}
		claim := repository.claims[repository.claimIndex]
		repository.claimIndex++
		return claim, true, nil
	}
	if repository.claimed || sceneType != repository.claim.SceneType {
		return GeneralSceneClaim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *generalSceneRuntimeRepositoryStub) LoadGeneralSceneAtomicResults(
	context.Context,
	GeneralSceneClaim,
) ([]GeneralSceneAtomicResult, error) {
	repository.atomicLoads++
	result := make([]GeneralSceneAtomicResult, 0, len(repository.readyAtoms))
	for _, atom := range repository.readyAtoms {
		result = append(result, atom)
	}
	return result, nil
}

func (repository *generalSceneRuntimeRepositoryStub) RecordGeneralSceneAtomicAttempt(
	_ context.Context,
	_ GeneralSceneClaim,
	attempt GeneralSceneAtomicAttempt,
) error {
	repository.atomicAttempts = append(repository.atomicAttempts, attempt)
	if attempt.Result != nil {
		repository.readyAtoms[attempt.Key] = *attempt.Result
	}
	return nil
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
	if repository.failureStatusIdx < len(repository.failureStatuses) {
		status := repository.failureStatuses[repository.failureStatusIdx]
		repository.failureStatusIdx++
		return status, nil
	}
	return GeneralSceneRuntimeFailed, nil
}
