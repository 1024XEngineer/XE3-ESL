package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIELTSSpeakingShadowConcurrentClaimAndComplete(
	t *testing.T,
) {
	pool, repository, configuration, evaluation :=
		prepareIELTSSpeakingShadowRuntime(t)
	const callers = 8
	start := make(chan struct{})
	claims := make(chan scoring.IELTSSpeakingShadowClaim, callers)
	failures := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			claim, acquired, err :=
				repository.ClaimIELTSSpeakingShadow(
					context.Background(),
					configuration,
				)
			if err != nil {
				failures <- err
				return
			}
			if acquired {
				claims <- claim
			}
		}()
	}
	close(start)
	wait.Wait()
	close(claims)
	close(failures)
	for err := range failures {
		var definition string
		_ = pool.QueryRow(context.Background(), `
			SELECT pg_get_constraintdef(oid)
			FROM pg_constraint
			WHERE conrelid = 'evaluation_module_runs'::regclass
			  AND conname = 'evaluation_module_runs_scene_check'
		`).Scan(&definition)
		t.Errorf(
			"concurrent claim: %v; scene constraint=%s",
			err,
			definition,
		)
	}
	var winners []scoring.IELTSSpeakingShadowClaim
	for claim := range claims {
		winners = append(winners, claim)
	}
	if len(winners) != 1 || !winners[0].Valid() {
		t.Fatalf("claim winners = %#v", winners)
	}
	claim := winners[0]
	result := evaluateIELTSSpeakingClaim(t, claim)
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("CompleteIELTSSpeakingShadow: %v", err)
	}
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("idempotent completion: %v", err)
	}
	state, err := repository.GetIELTSSpeakingShadowState(
		context.Background(),
		testOwnerA,
		evaluation.ID,
		evaluation.Revision.ID,
	)
	if err != nil {
		t.Fatalf("GetIELTSSpeakingShadowState: %v", err)
	}
	if state.ModuleStatus != scoring.IELTSSpeakingShadowRuntimeReady ||
		state.Result == nil ||
		state.Result.SnapshotID != claim.Snapshot.ID {
		t.Fatalf("ready state = %#v", state)
	}
	var resultCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM evaluation_ielts_speaking_scene_results
		WHERE evaluation_revision_id = $1
	`, evaluation.Revision.ID).Scan(&resultCount); err != nil {
		t.Fatalf("count IELTS results: %v", err)
	}
	if resultCount != 1 {
		t.Fatalf("result count = %d, want 1", resultCount)
	}
}

func TestPostgresIELTSSpeakingReportReadIsOwnerScoped(
	t *testing.T,
) {
	_, repository, configuration, evaluation :=
		prepareIELTSSpeakingShadowRuntime(t)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, claim)
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetCurrentIELTSSpeakingReportState(
		context.Background(),
		testOwnerA,
		evaluation.PracticeSessionID,
	)
	if err != nil {
		t.Fatalf("GetCurrentIELTSSpeakingReportState: %v", err)
	}
	if state.Evaluation.ID != evaluation.ID ||
		state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimeReady ||
		state.Snapshot == nil {
		t.Fatalf("report state = %#v", state)
	}
	report, err := report.ProjectIELTSSpeakingReport(
		*state.Snapshot,
		*state.Runtime.Result,
	)
	if err != nil || !report.Valid() {
		t.Fatalf("project report valid=%t error=%v", report.Valid(), err)
	}
	if _, err := repository.GetCurrentIELTSSpeakingReportState(
		context.Background(),
		integrationOwnerB,
		evaluation.PracticeSessionID,
	); !errors.Is(err, evaluationcore.ErrNotFound) {
		t.Fatalf("cross-owner report error = %v", err)
	}
}

func TestPostgresIELTSSpeakingResultConstraintRejectsFCBand(
	t *testing.T,
) {
	pool, repository, configuration, _ :=
		prepareIELTSSpeakingShadowRuntime(t)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, claim)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO evaluation_ielts_speaking_scene_results (
			module_run_id,
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			strategy_ref,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scene_type,
			snapshot_hash,
			full_config_hash,
			prompt_version,
			provider,
			model,
			provider_request_id,
			fencing_token,
			result_payload
		)
		VALUES (
			$1, $2, $3, $4, 'SCENE', $5, $6, $7, $8,
			'IELTS_SPEAKING', $9, $10, $11, $12, $13,
			$14, $15,
			jsonb_set(
				$16::jsonb,
				'{criteria,0,estimated_band}',
				'9'::jsonb
			)
		)
	`, claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload)
	if err == nil {
		t.Fatal("database accepted an FC Band")
	}
}

func TestPostgresIELTSSpeakingResultBindingRejectsSnapshotMismatch(
	t *testing.T,
) {
	pool, repository, configuration, _ :=
		prepareIELTSSpeakingShadowRuntime(t)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, claim)

	var emptyPart2Valid bool
	if err := pool.QueryRow(context.Background(), `
		SELECT ielts_assignment_is_valid_v1(
			'FULL_MOCK',
			canonical_payload #> '{practice_context,task_blueprints}',
			jsonb_set(
				canonical_payload #>
					'{practice_context,ielts_assignment}',
				'{parts,1,turn_blueprints}',
				'[]'::jsonb
			)
		)
		FROM evaluation_evidence_snapshots
		WHERE id = $1
	`, claim.Snapshot.ID).Scan(&emptyPart2Valid); err != nil {
		t.Fatalf("validate empty IELTS Part 2 assignment: %v", err)
	}
	if emptyPart2Valid {
		t.Fatal("database accepted an empty IELTS Part 2 assignment")
	}

	statusMismatch := result
	statusMismatch.QuestionResults = append(
		[]scoring.IELTSSpeakingShadowQuestionResult(nil),
		result.QuestionResults...,
	)
	statusMismatch.QuestionResults[0].OpportunityStatus =
		scoring.IELTSOpportunityNotProvided
	statusMismatch.QuestionResults[0].AssessmentStatus =
		scoring.IELTSAssessmentNotAssessed
	statusMismatch.QuestionResults[0].ResponseTurnID = ""
	statusMismatch.QuestionResults[0].EvidenceRefIDs = []string{}
	assertIELTSResultPartBindingRejected(
		t,
		insertRawIELTSSpeakingResult(t, pool, claim, statusMismatch),
	)

	wrongPositionRef := result
	wrongPositionRef.QuestionResults = append(
		[]scoring.IELTSSpeakingShadowQuestionResult(nil),
		result.QuestionResults...,
	)
	wrongPositionRef.QuestionResults[0].EvidenceRefIDs = []string{
		result.QuestionResults[1].EvidenceRefIDs[0],
	}
	assertIELTSResultPartBindingRejected(
		t,
		insertRawIELTSSpeakingResult(t, pool, claim, wrongPositionRef),
	)
}

func insertRawIELTSSpeakingResult(
	t *testing.T,
	pool *pgxpool.Pool,
	claim scoring.IELTSSpeakingShadowClaim,
	result scoring.IELTSSpeakingShadowResult,
) error {
	t.Helper()
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode raw IELTS result: %v", err)
	}
	var providerRequestID any
	if result.Provider != nil {
		providerRequestID = result.Provider.RequestID
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO evaluation_ielts_speaking_scene_results (
			module_run_id,
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			strategy_ref,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scene_type,
			snapshot_hash,
			full_config_hash,
			prompt_version,
			provider,
			model,
			provider_request_id,
			fencing_token,
			result_payload
		)
		VALUES (
			$1, $2, $3, $4, 'SCENE', $5, $6, $7, $8,
			'IELTS_SPEAKING', $9, $10, $11, $12, $13,
			$14, $15, $16
		)
	`, claim.ModuleRunID, claim.EvaluationID,
		claim.EvaluationRevisionID, claim.OwnerUserID,
		claim.StrategyRef, claim.Snapshot.PracticeSessionID,
		claim.Snapshot.ID, claim.Snapshot.InputRevision,
		claim.Snapshot.SnapshotHash[:], claim.FullConfigHash[:],
		claim.PromptVersion, claim.Provider, claim.Model,
		providerRequestID, claim.FencingToken, payload)
	return err
}

func assertIELTSResultPartBindingRejected(t *testing.T, err error) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" ||
		databaseError.Message != "invalid IELTS Part result binding" {
		t.Fatalf("IELTS result Part binding error = %v", err)
	}
}

func TestPostgresIELTSSpeakingResultShapeFailsClosed(t *testing.T) {
	pool, repository, configuration, _ :=
		prepareIELTSSpeakingShadowRuntime(t)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, claim)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var valid bool
	var missingSchema bool
	var missingSnapshot bool
	var missingGate bool
	var hiddenOverall bool
	var extraPRReason bool
	var missingQuestionIndex bool
	var rootDowngrade bool
	var criterionDowngrade bool
	if err := pool.QueryRow(context.Background(), `
		SELECT
			evaluation_ielts_result_shape_is_valid($1::jsonb),
			evaluation_ielts_result_shape_is_valid(
				$1::jsonb - 'schema_version'
			),
			evaluation_ielts_result_shape_is_valid(
				$1::jsonb - 'snapshot_id'
			),
			evaluation_ielts_result_shape_is_valid(
				$1::jsonb - 'gate_status'
			),
			evaluation_ielts_result_shape_is_valid(
				jsonb_set($1::jsonb, '{Overall}', '5'::jsonb)
			),
			evaluation_ielts_result_shape_is_valid(
				jsonb_set(
					$1::jsonb,
					'{criteria,3,reason_codes}',
					($1::jsonb #> '{criteria,3,reason_codes}')
						|| '["OTHER_REASON"]'::jsonb
				)
			),
			evaluation_ielts_result_shape_is_valid(
				$1::jsonb #- '{question_results,0,index}'
			),
			evaluation_ielts_result_shape_is_valid(
				jsonb_set(
					$1::jsonb,
					'{scoreability_status}',
					'"INSUFFICIENT"'::jsonb
				)
			),
			evaluation_ielts_result_shape_is_valid(
				jsonb_set(
					$1::jsonb,
					'{criteria,1,scoreability_status}',
					'"INSUFFICIENT"'::jsonb
				)
			)
	`, payload).Scan(
		&valid,
		&missingSchema,
		&missingSnapshot,
		&missingGate,
		&hiddenOverall,
		&extraPRReason,
		&missingQuestionIndex,
		&rootDowngrade,
		&criterionDowngrade,
	); err != nil {
		t.Fatalf("inspect IELTS result shape: %v", err)
	}
	if !valid || missingSchema || missingSnapshot || missingGate ||
		hiddenOverall || extraPRReason || missingQuestionIndex ||
		rootDowngrade || criterionDowngrade {
		t.Fatalf(
			"shape valid=%t missing_schema=%t missing_snapshot=%t "+
				"missing_gate=%t "+
				"hidden_overall=%t "+
				"extra_pr_reason=%t missing_question_index=%t "+
				"root_downgrade=%t criterion_downgrade=%t",
			valid,
			missingSchema,
			missingSnapshot,
			missingGate,
			hiddenOverall,
			extraPRReason,
			missingQuestionIndex,
			rootDowngrade,
			criterionDowngrade,
		)
	}
}

func TestPostgresIELTSSpeakingStaleFenceCannotComplete(t *testing.T) {
	pool, repository, configuration, _ :=
		prepareIELTSSpeakingShadowRuntime(t)
	first := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, first)
	expireInterviewShadowLease(t, pool, first.OutboxID)
	second := claimIELTSSpeakingShadow(t, repository, configuration)
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		first,
		result,
	); !errors.Is(err, scoring.ErrRuntimeLeaseLost) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		second,
		result,
	); err != nil {
		t.Fatalf("takeover completion: %v", err)
	}
}

func TestPostgresIELTSSpeakingFailureExhaustionDoesNotPublishReport(
	t *testing.T,
) {
	pool, repository, configuration, value :=
		prepareIELTSSpeakingShadowRuntime(t)
	configuration.MaxAttempts = 1
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	status, err := repository.FailIELTSSpeakingShadow(
		context.Background(),
		claim,
		scoring.IELTSSpeakingShadowFailure{
			Code:      "provider_timeout",
			Retryable: true,
		},
		configuration,
	)
	if err != nil || status != scoring.IELTSSpeakingShadowRuntimeFailed {
		t.Fatalf("FailIELTSSpeakingShadow = %q, %v", status, err)
	}
	state, err := repository.GetCurrentIELTSSpeakingReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	)
	if err != nil {
		t.Fatalf("failed IELTS report state: %v", err)
	}
	if state.Evaluation.Revision.Status != evaluationcore.StatusFailed ||
		state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimeFailed ||
		state.Runtime.Result != nil ||
		state.Runtime.Failure == nil ||
		state.Runtime.Failure.Code != "provider_timeout" ||
		state.Snapshot != nil {
		t.Fatalf("failed IELTS report state = %#v", state)
	}
	var resultCount int
	var reportCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*)
			 FROM evaluation_ielts_speaking_scene_results
			 WHERE evaluation_revision_id = $1),
			(SELECT count(*)
			 FROM evaluation_formal_reports
			 WHERE evaluation_revision_id = $1)
	`, value.Revision.ID).Scan(&resultCount, &reportCount); err != nil {
		t.Fatalf("count failed IELTS artifacts: %v", err)
	}
	if resultCount != 0 || reportCount != 0 {
		t.Fatalf(
			"failed IELTS artifacts results=%d reports=%d",
			resultCount,
			reportCount,
		)
	}
}

func TestIELTSSpeakingShadowRuntimeMigrationRoundTrip(t *testing.T) {
	pool := evaluationDatabase(t)
	ctx := context.Background()
	down, err := migrations.Files.ReadFile(
		"000039_evaluation_ielts_speaking_shadow_runtime.down.sql",
	)
	if err != nil {
		t.Fatalf("read IELTS Speaking Shadow down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply IELTS Speaking Shadow down migration: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass(
			'evaluation_ielts_speaking_scene_results'
		) IS NOT NULL
	`).Scan(&exists); err != nil {
		t.Fatalf("inspect down-migrated IELTS table: %v", err)
	}
	if exists {
		t.Fatal("IELTS result table still exists after down migration")
	}
	up, err := migrations.Files.ReadFile(
		"000039_evaluation_ielts_speaking_shadow_runtime.up.sql",
	)
	if err != nil {
		t.Fatalf("read IELTS Speaking Shadow up migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("reapply IELTS Speaking Shadow up migration: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass(
			'evaluation_ielts_speaking_scene_results'
		) IS NOT NULL
	`).Scan(&exists); err != nil {
		t.Fatalf("inspect reapplied IELTS table: %v", err)
	}
	if !exists {
		t.Fatal("IELTS result table missing after migration reapply")
	}
}

func prepareIELTSSpeakingShadowRuntime(
	t *testing.T,
) (
	*pgxpool.Pool,
	*PostgresRepository,
	scoring.IELTSSpeakingShadowRuntimeConfiguration,
	evaluationcore.Evaluation,
) {
	t.Helper()
	pool := evaluationDatabase(t)
	insertEvaluationUsers(
		t,
		pool,
		testOwnerA,
		integrationOwnerB,
	)
	repository := NewPostgresRepository(pool)
	evidenceRepository := evidence.NewPostgresRepository(pool)
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsPostgresQuestionCount)
	persistEvidenceSnapshotFixture(t, pool, snapshot)
	service := evaluationcore.NewService(repository, evidenceRepository)
	evaluation, replayed, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		evaluationcore.CreateRequest{
			PracticeSessionID: snapshot.PracticeSessionID,
			InputSnapshotID:   snapshot.ID,
			InputRevision:     snapshot.InputRevision,
			Scope:             evaluationcore.ScopeSession,
			SceneType:         evaluationcore.SceneIELTSSpeaking,
			Channels:          []evaluationcore.Channel{evaluationcore.ChannelScene},
			SceneStrategyRef:  scoring.IELTSSpeakingShadowStrategyRef,
			PipelineVersion:   scoring.IELTSSpeakingShadowPipelineVersion,
		},
	)
	if err != nil || replayed {
		t.Fatalf(
			"create IELTS Evaluation replayed=%t error=%v",
			replayed,
			err,
		)
	}
	configuration := scoring.IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   5 * time.Second,
		StrategyRef:     scoring.IELTSSpeakingShadowStrategyRef,
		PipelineVersion: scoring.IELTSSpeakingShadowPipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("ielts-shadow-integration-config/v1"),
		),
		PromptVersion: scoring.IELTSSpeakingShadowPromptVersion,
		Provider:      "qianwen",
		Model:         "qwen-plus",
	}
	if !configuration.Valid() {
		t.Fatalf("invalid IELTS runtime config: %#v", configuration)
	}
	return pool, repository, configuration, evaluation
}

func claimIELTSSpeakingShadow(
	t *testing.T,
	repository *PostgresRepository,
	configuration scoring.IELTSSpeakingShadowRuntimeConfiguration,
) scoring.IELTSSpeakingShadowClaim {
	t.Helper()
	claim, acquired, err := repository.ClaimIELTSSpeakingShadow(
		context.Background(),
		configuration,
	)
	if err != nil {
		t.Fatalf("ClaimIELTSSpeakingShadow: %v", err)
	}
	if !acquired || !claim.Valid() {
		t.Fatalf("claim acquired=%t value=%#v", acquired, claim)
	}
	return claim
}

func evaluateIELTSSpeakingClaim(
	t *testing.T,
	claim scoring.IELTSSpeakingShadowClaim,
) scoring.IELTSSpeakingShadowResult {
	t.Helper()
	result, err := scoring.NewIELTSSpeakingShadowEngine(
		&ieltsProviderStub{},
	).Evaluate(context.Background(), claim.Snapshot)
	if err != nil {
		t.Fatalf("evaluate IELTS claim: %v", err)
	}
	if result.Provider != nil {
		result.Provider.Provider = claim.Provider
		result.Provider.Model = claim.Model
	}
	return result
}
