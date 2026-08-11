package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
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
	result, err := scoring.NewGeneralSceneEngine(&generalSceneProviderStub{}).Evaluate(
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
	if err := NewPostgresDeletionRepository(pool).DeleteUserData(
		context.Background(),
		evaluationcore.DeleteUserDataCommand{
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
	result, err := scoring.NewGeneralSceneEngine(provider).Evaluate(
		context.Background(),
		claim.Snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 ||
		result.ScoreabilityStatus != scoring.GeneralSceneScoreabilityInsufficient {
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

func TestPostgresGeneralSceneAtomicAttemptsResumeOnlyMissingDimension(
	t *testing.T,
) {
	snapshot := generalSceneTestSnapshot(
		t,
		evaluationcore.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I read every evening because it helps me relax.",
	)
	pool, repository, configuration, evaluationID :=
		prepareGeneralScenePostgresEvaluation(t, snapshot)
	provider := &atomicGeneralSceneProviderStub{
		rejectOnce: scoring.GeneralSceneDimensionClarity,
		calls:      make(map[scoring.GeneralSceneDimension]int),
	}
	firstWorker, err := scoring.NewGeneralSceneWorker(
		repository,
		scoring.NewGeneralSceneEngine(provider),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstSweep, err := firstWorker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if firstSweep != (scoring.GeneralSceneSweepResult{Claimed: 1, Retried: 1}) {
		t.Fatalf("first sweep = %#v", firstSweep)
	}
	assertGeneralSceneAtomicAttemptCounts(t, pool, evaluationID, 3, 1)
	var failedProviderRequestID *string
	if err := pool.QueryRow(context.Background(), `
		SELECT attempt.provider_request_id
		FROM evaluation_general_scene_atomic_attempts AS attempt
		JOIN evaluation_module_runs AS run
		  ON run.id = attempt.module_run_id
		WHERE run.evaluation_id = $1
		  AND attempt.status = 'FAILED'
	`, evaluationID).Scan(&failedProviderRequestID); err != nil {
		t.Fatal(err)
	}
	wantFailedRequestID := "atomic-request-CLARITY_COHERENCE-1"
	if failedProviderRequestID == nil ||
		*failedProviderRequestID != wantFailedRequestID {
		t.Fatalf(
			"failed provider request id = %v, want %q",
			failedProviderRequestID,
			wantFailedRequestID,
		)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE evaluation_outbox
		SET available_at = transaction_timestamp()
		WHERE evaluation_id = $1
		  AND delivery_status = 'PENDING'
	`, evaluationID); err != nil {
		t.Fatal(err)
	}
	secondWorker, err := scoring.NewGeneralSceneWorker(
		repository,
		scoring.NewGeneralSceneEngine(provider),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	secondSweep, err := secondWorker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if secondSweep != (scoring.GeneralSceneSweepResult{Claimed: 1, Completed: 1}) {
		t.Fatalf("second sweep = %#v", secondSweep)
	}
	assertGeneralSceneAtomicAttemptCounts(t, pool, evaluationID, 4, 1)
	var aggregateProviderRequestID *string
	if err := pool.QueryRow(context.Background(), `
		SELECT result.provider_request_id
		FROM evaluation_general_scene_results AS result
		JOIN evaluation_module_runs AS run
		  ON run.id = result.module_run_id
		WHERE run.evaluation_id = $1
	`, evaluationID).Scan(&aggregateProviderRequestID); err != nil {
		t.Fatal(err)
	}
	if aggregateProviderRequestID != nil {
		t.Fatalf(
			"aggregate provider request id = %q, want NULL",
			*aggregateProviderRequestID,
		)
	}
	var persistedAtomicRequestIDs int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(DISTINCT attempt.provider_request_id)
		FROM evaluation_general_scene_atomic_attempts AS attempt
		JOIN evaluation_module_runs AS run
		  ON run.id = attempt.module_run_id
		WHERE run.evaluation_id = $1
		  AND attempt.status = 'READY'
	`, evaluationID).Scan(&persistedAtomicRequestIDs); err != nil {
		t.Fatal(err)
	}
	if persistedAtomicRequestIDs != len(scoring.GeneralSceneDimensions()) {
		t.Fatalf(
			"persisted atomic request ids = %d, want %d",
			persistedAtomicRequestIDs,
			len(scoring.GeneralSceneDimensions()),
		)
	}
	provider.mu.Lock()
	for _, dimension := range scoring.GeneralSceneDimensions() {
		want := 1
		if dimension == scoring.GeneralSceneDimensionClarity {
			want = 2
		}
		if provider.calls[dimension] != want {
			t.Fatalf(
				"dimension %s calls=%d want=%d",
				dimension,
				provider.calls[dimension],
				want,
			)
		}
	}
	provider.mu.Unlock()
	assertGeneralSceneProjectionCounts(t, pool, testOwnerA, 1, 4, 4)
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatal(err)
	}
	if err := NewPostgresDeletionRepository(pool).DeleteUserData(
		context.Background(),
		evaluationcore.DeleteUserDataCommand{
			OwnerUserID:        testOwnerA,
			DeletionGeneration: 1,
		},
	); err != nil {
		t.Fatalf("delete atomic general Scene data: %v", err)
	}
	assertGeneralSceneAtomicAttemptCounts(t, pool, evaluationID, 0, 0)
	assertGeneralSceneProjectionCounts(t, pool, testOwnerA, 0, 0, 0)
}

func TestPostgresGeneralSceneRejectsAtomicAggregateWithoutPersistedAtoms(
	t *testing.T,
) {
	snapshot := generalSceneTestSnapshot(
		t,
		evaluationcore.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I read every evening because it helps me relax.",
	)
	_, repository, configuration, _ :=
		prepareGeneralScenePostgresEvaluation(t, snapshot)
	claim, acquired, err := repository.ClaimGeneralScene(
		context.Background(),
		snapshot.SceneType,
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim acquired=%t error=%v", acquired, err)
	}
	provider := &atomicGeneralSceneProviderStub{
		calls: make(map[scoring.GeneralSceneDimension]int),
	}
	engine := scoring.NewGeneralSceneEngine(provider)
	atoms := make([]scoring.GeneralSceneAtomicResult, 0, 4)
	for _, dimension := range scoring.GeneralSceneDimensions() {
		atom, evaluateErr := engine.EvaluateAtomic(
			context.Background(),
			snapshot,
			scoring.GeneralSceneAtomicKey{
				Part:      scoring.IELTSPart1,
				Dimension: dimension,
			},
		)
		if evaluateErr != nil {
			t.Fatal(evaluateErr)
		}
		atoms = append(atoms, atom)
	}
	result, err := scoring.AggregateGeneralSceneAtoms(snapshot, atoms)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.CompleteGeneralScene(context.Background(), claim, result)
	if !errors.Is(err, evaluationcore.ErrInvalidRequest) {
		t.Fatalf("complete without persisted atoms error = %v", err)
	}
	for index := range atoms {
		if err := repository.RecordGeneralSceneAtomicAttempt(
			context.Background(),
			claim,
			scoring.GeneralSceneAtomicAttempt{
				Key:               atoms[index].Key,
				AttemptCount:      claim.AttemptCount,
				Status:            scoring.GeneralSceneAtomicAttemptReady,
				ProviderRequestID: atoms[index].Provider.RequestID,
				Result:            &atoms[index],
			},
		); err != nil {
			t.Fatalf("persist atom %d: %v", index, err)
		}
	}
	if err := repository.CompleteGeneralScene(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("complete with persisted atoms: %v", err)
	}
	if err := repository.CompleteGeneralScene(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("replay atomic completion: %v", err)
	}
}

func TestPostgresGeneralSceneAtomicAttemptRejectsSupersededRevision(
	t *testing.T,
) {
	snapshot := generalSceneTestSnapshot(
		t,
		evaluationcore.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I read every evening because it helps me relax.",
	)
	pool, repository, configuration, _ :=
		prepareGeneralScenePostgresEvaluation(t, snapshot)
	claim, acquired, err := repository.ClaimGeneralScene(
		context.Background(),
		snapshot.SceneType,
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim acquired=%t error=%v", acquired, err)
	}
	provider := &atomicGeneralSceneProviderStub{
		calls: make(map[scoring.GeneralSceneDimension]int),
	}
	atom, err := scoring.NewGeneralSceneEngine(provider).EvaluateAtomic(
		context.Background(),
		snapshot,
		scoring.GeneralSceneAtomicKey{
			Part:      scoring.IELTSPart1,
			Dimension: scoring.GeneralSceneDimensionTaskAchievement,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO evaluation_revisions (
			evaluation_id,
			owner_user_id,
			revision,
			supersedes_revision_id,
			channels,
			scene_strategy_ref,
			pipeline_version,
			schema_version,
			request_fingerprint
		)
		SELECT
			revision.evaluation_id,
			revision.owner_user_id,
			2,
			revision.id,
			revision.channels,
			revision.scene_strategy_ref,
			revision.pipeline_version,
			revision.schema_version,
			decode(repeat('77', 32), 'hex')
		FROM evaluation_revisions AS revision
		WHERE revision.id = $1
	`, claim.EvaluationRevisionID); err != nil {
		t.Fatalf("insert later revision: %v", err)
	}
	err = repository.RecordGeneralSceneAtomicAttempt(
		context.Background(),
		claim,
		scoring.GeneralSceneAtomicAttempt{
			Key:               atom.Key,
			AttemptCount:      claim.AttemptCount,
			Status:            scoring.GeneralSceneAtomicAttemptReady,
			ProviderRequestID: atom.Provider.RequestID,
			Result:            &atom,
		},
	)
	if postgresCode(err) != "23514" {
		t.Fatalf("superseded revision insert error = %v", err)
	}
}

func assertGeneralSceneAtomicAttemptCounts(
	t *testing.T,
	pool *pgxpool.Pool,
	evaluationID string,
	wantReady int,
	wantFailed int,
) {
	t.Helper()
	var ready, failed int
	if err := pool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE attempt.status = 'READY'),
			count(*) FILTER (WHERE attempt.status = 'FAILED')
		FROM evaluation_general_scene_atomic_attempts AS attempt
		JOIN evaluation_module_runs AS run
		  ON run.id = attempt.module_run_id
		WHERE run.evaluation_id = $1
	`, evaluationID).Scan(&ready, &failed); err != nil {
		t.Fatal(err)
	}
	if ready != wantReady || failed != wantFailed {
		t.Fatalf(
			"atomic attempts ready/failed=%d/%d want=%d/%d",
			ready,
			failed,
			wantReady,
			wantFailed,
		)
	}
}

type atomicGeneralSceneProviderStub struct {
	mu         sync.Mutex
	calls      map[scoring.GeneralSceneDimension]int
	failOnce   scoring.GeneralSceneDimension
	rejectOnce scoring.GeneralSceneDimension
}

func (*atomicGeneralSceneProviderStub) AnalyzeGeneralScene(
	context.Context,
	scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	return scoring.GeneralSceneProviderResult{}, fmt.Errorf(
		"legacy general Scene provider must not be called",
	)
}

func (provider *atomicGeneralSceneProviderStub) AnalyzeGeneralSceneAtom(
	_ context.Context,
	input scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	dimension := input.AssessableDimensions[0]
	provider.calls[dimension]++
	requestID := fmt.Sprintf(
		"atomic-request-%s-%d",
		dimension,
		provider.calls[dimension],
	)
	if dimension == provider.failOnce && provider.calls[dimension] == 1 {
		return scoring.GeneralSceneProviderResult{}, context.DeadlineExceeded
	}
	if dimension == provider.rejectOnce && provider.calls[dimension] == 1 {
		return scoring.GeneralSceneProviderResult{
			Payload:   json.RawMessage(`{"schema_version":`),
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: requestID,
		}, nil
	}
	var response *scoring.GeneralSceneResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil {
			response = opportunity.Response
			break
		}
	}
	if response == nil {
		return scoring.GeneralSceneProviderResult{}, fmt.Errorf("missing response")
	}
	dimensionIndex := slices.Index(scoring.GeneralSceneDimensions(), dimension)
	payload, err := json.Marshal(map[string]any{
		"schema_version": scoring.GeneralSceneAtomicProviderSchemaVersion,
		"dimension": map[string]any{
			"dimension_id": dimension,
			"score":        60 + dimensionIndex*10,
			"strengths":    []any{},
			"improvements": []any{map[string]any{
				"template_id": string(dimension) + ":IMPROVEMENT:v1",
				"evidence": []any{map[string]any{
					"evidence_ref_id": response.EvidenceRefID,
					"quote":           response.Transcript,
					"occurrence":      1,
				}},
			}},
			"recommended_examples": []any{},
		},
	})
	return scoring.GeneralSceneProviderResult{
		Payload: payload, Provider: "qianwen", Model: "qwen-plus",
		RequestID: requestID,
	}, err
}

func prepareGeneralScenePostgresRuntime(
	t *testing.T,
	transcript string,
) (*pgxpool.Pool, *PostgresRepository, scoring.GeneralSceneClaim) {
	t.Helper()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluationcore.SceneOverseasWorkplace,
		scene.PracticeExperienceWorkplace,
		scene.SceneCategoryWorkplaceGeneral,
		scene.PracticeModeFullSimulation,
		transcript,
	)
	pool, repository, configuration, evaluationID :=
		prepareGeneralScenePostgresEvaluation(t, snapshot)
	claim, acquired, err := repository.ClaimGeneralScene(
		context.Background(),
		snapshot.SceneType,
		configuration,
	)
	if err != nil || !acquired || claim.EvaluationID != evaluationID {
		t.Fatalf("claim general Scene acquired=%t claim=%#v error=%v", acquired, claim, err)
	}
	return pool, repository, claim
}

func prepareGeneralScenePostgresEvaluation(
	t *testing.T,
	snapshot evidence.EvidenceSnapshot,
) (
	*pgxpool.Pool,
	*PostgresRepository,
	scoring.GeneralSceneRuntimeConfiguration,
	string,
) {
	t.Helper()
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	repository := NewPostgresRepository(pool)
	evidenceRepository := evidence.NewPostgresRepository(pool)
	persistEvidenceSnapshotFixture(t, pool, snapshot)
	created, replayed, err := evaluationcore.NewService(
		repository,
		evidenceRepository,
	).CreateCompleted(
		context.Background(),
		testOwnerA,
		evaluationcore.CreateRequest{
			PracticeSessionID: snapshot.PracticeSessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             evaluationcore.ScopeSession,
			SceneType:         snapshot.SceneType,
			Channels:          []evaluationcore.Channel{evaluationcore.ChannelScene},
			SceneStrategyRef:  scoring.GeneralSceneStrategyRef,
			PipelineVersion:   scoring.GeneralScenePipelineVersion,
		},
	)
	if err != nil || replayed {
		t.Fatalf("create general Scene Evaluation replayed=%t error=%v", replayed, err)
	}
	configuration := scoring.GeneralSceneRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   5 * time.Second,
		StrategyRef:     scoring.GeneralSceneStrategyRef,
		PipelineVersion: scoring.GeneralScenePipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("general-scene-integration-config/v1"),
		),
		IELTSFullConfigHash: sha256.Sum256(
			[]byte("general-scene-integration-config/ielts-atomic/v1"),
		),
		PromptVersion: scoring.GeneralScenePromptVersion,
		Provider:      "qianwen",
		Model:         "qwen-plus",
	}
	return pool, repository, configuration, created.ID
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
