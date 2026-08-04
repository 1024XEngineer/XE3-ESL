package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const integrationOwnerB = "20000000-0000-4000-8000-000000000002"

func TestPostgresAcceptsDigitLeadingPracticeSessionID(t *testing.T) {
	const practiceSessionID = "20000000-0000-4000-8000-000000000001"
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	repository := NewPostgresRepository(pool)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(validEvidenceSnapshotPayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.Preparation.BackgroundSnapshotHash = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	snapshot := postgresTestSnapshotForOwnerAndSession(
		t,
		payload,
		evaluationcore.SceneInterview,
		testOwnerA,
		practiceSessionID,
	)
	persistEvidenceSnapshotFixture(t, pool, snapshot)
	request := validCreateRequest()
	request.PracticeSessionID = practiceSessionID
	request.InputSnapshotID = snapshot.ID
	request.InputRevision = snapshot.InputRevision
	request.SceneStrategyRef = scoring.InterviewShadowStrategyRef
	request.PipelineVersion = scoring.InterviewShadowPipelineVersion
	created, replayed, err := evaluationcore.NewService(
		repository,
		evidence.NewPostgresRepository(pool),
	).Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if replayed || created.PracticeSessionID != practiceSessionID ||
		!created.Valid() {
		t.Fatalf("created = %#v, replayed = %v", created, replayed)
	}
	configuration := scoring.InterviewShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   5 * time.Second,
		StrategyRef:     scoring.InterviewShadowStrategyRef,
		PipelineVersion: scoring.InterviewShadowPipelineVersion,
		FullConfigHash: sha256.Sum256(
			[]byte("practice-resource-id-integration/v1"),
		),
		PromptVersion: scoring.InterviewShadowPromptVersion,
		Provider:      "qianwen",
		Model:         "qwen-plus",
	}
	claim := claimInterviewShadow(t, repository, configuration)
	if claim.EvaluationID != created.ID ||
		claim.Snapshot.PracticeSessionID != practiceSessionID {
		t.Fatalf("claim = %#v", claim)
	}

	down, err := migrations.Files.ReadFile(
		"000038_evaluation_practice_resource_ids.down.sql",
	)
	if err != nil {
		t.Fatalf("read resource ID down migration: %v", err)
	}
	connection, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer connection.Release()
	if _, err = connection.Exec(context.Background(), string(down)); err == nil {
		t.Fatal("down migration accepted digit-leading Practice data")
	}
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) ||
		databaseError.Code != "23514" ||
		databaseError.ConstraintName !=
			"evaluation_module_runs_practice_session_check" {
		t.Fatalf("down migration error = %v", err)
	}
	if _, rollbackErr := connection.Exec(
		context.Background(),
		"ROLLBACK",
	); rollbackErr != nil {
		t.Fatalf("rollback rejected down migration: %v", rollbackErr)
	}
	var preservedRows int64
	if err := pool.QueryRow(context.Background(), `
		SELECT
		    (SELECT count(*)
		       FROM evaluation_ledgers
		      WHERE practice_session_id = $1)
		  + (SELECT count(*)
		       FROM evaluation_evidence_snapshots
		      WHERE practice_session_id = $1)
		  + (SELECT count(*)
		       FROM evaluation_module_runs
		      WHERE practice_session_id = $1)
	`, practiceSessionID).Scan(&preservedRows); err != nil {
		t.Fatalf("inspect preserved Practice rows: %v", err)
	}
	if preservedRows != 3 {
		t.Fatalf("preserved Practice rows = %d, want 3", preservedRows)
	}
}

func TestPostgresLedgerRevisionIdempotencyAndIsolation(t *testing.T) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA, integrationOwnerB)
	service, request := serviceWithEvidenceSnapshot(t, pool, testOwnerA)
	ctx := testActorContext(testOwnerA)

	request.Channels = []evaluationcore.Channel{evaluationcore.ChannelScene, evaluationcore.ChannelCore4D}
	request.Core4DStrategyRef = "core4d/v1"
	created, replayed, err := service.Create(
		ctx,
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if replayed || !created.Valid() || created.Revision.Number != 1 {
		t.Fatalf("created Evaluation = %#v, replayed = %v", created, replayed)
	}
	request.ClientRequestID = "retry-trace"
	replay, replayed, err := service.Create(
		ctx,
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("replay Create: %v", err)
	}
	if !replayed || replay.ID != created.ID ||
		replay.Revision.ID != created.Revision.ID {
		t.Fatalf("replay = %#v, replayed = %v", replay, replayed)
	}
	input := evaluationcore.CreateInput{
		PracticeSessionID: created.PracticeSessionID,
		InputSnapshotID:   created.InputSnapshotID,
		InputRevision:     created.InputRevision,
		Scope:             created.Scope,
		SceneType:         created.SceneType,
		Config: evaluationcore.RevisionConfig{
			Channels:          created.Revision.Channels,
			SceneStrategyRef:  created.Revision.SceneStrategyRef,
			Core4DStrategyRef: created.Revision.Core4DStrategyRef,
			PipelineVersion:   created.Revision.PipelineVersion,
			SchemaVersion:     created.Revision.SchemaVersion,
			ClientRequestID:   created.Revision.ClientRequestID,
		},
	}
	var rootKeyBytes, rootFingerprintBytes, revisionFingerprintBytes []byte
	if err := pool.QueryRow(context.Background(), `
		SELECT ledger.root_idempotency_key,
		       ledger.root_request_fingerprint,
		       revision.request_fingerprint
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		WHERE ledger.id = $1 AND revision.revision = 1
	`, created.ID).Scan(
		&rootKeyBytes,
		&rootFingerprintBytes,
		&revisionFingerprintBytes,
	); err != nil {
		t.Fatalf("read persisted Evaluation identity: %v", err)
	}
	var rootKey, rootFingerprint, revisionFingerprint [sha256.Size]byte
	copy(rootKey[:], rootKeyBytes)
	copy(rootFingerprint[:], rootFingerprintBytes)
	copy(revisionFingerprint[:], revisionFingerprintBytes)
	conflictingFingerprint := rootFingerprint
	conflictingFingerprint[0] ^= 0xff
	_, _, err = NewPostgresRepository(pool).Ensure(ctx, evaluationcore.EnsureCommand{
		OwnerUserID:         testOwnerA,
		RootIdempotencyKey:  rootKey,
		RootFingerprint:     conflictingFingerprint,
		RevisionFingerprint: revisionFingerprint,
		Input:               input,
	})
	if !errors.Is(err, evaluationcore.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	if _, err := service.Get(
		ctx,
		testActor(integrationOwnerB),
		created.ID,
	); !errors.Is(err, evaluationcore.ErrNotFound) {
		t.Fatalf("cross-owner Get error = %v", err)
	}

	revisionTwoRequest := evaluationcore.ReevaluateRequest{
		Channels:          []evaluationcore.Channel{evaluationcore.ChannelCore4D, evaluationcore.ChannelScene},
		SceneStrategyRef:  "interview/v2",
		Core4DStrategyRef: "core4d/v1",
		PipelineVersion:   "pipeline/v2",
		ClientRequestID:   "reevaluate-2",
	}
	revisionTwo, replayed, err := service.Reevaluate(
		ctx,
		testActor(testOwnerA),
		created.ID,
		revisionTwoRequest,
	)
	if err != nil {
		t.Fatalf("Reevaluate: %v", err)
	}
	if replayed || revisionTwo.Revision.Number != 2 ||
		revisionTwo.Revision.SupersedesRevisionID != created.Revision.ID {
		t.Fatalf("revision two = %#v, replayed = %v", revisionTwo, replayed)
	}
	revisionTwoRequest.ClientRequestID = "reevaluate-2-retry"
	revisionTwoReplay, replayed, err := service.Reevaluate(
		ctx,
		testActor(testOwnerA),
		created.ID,
		revisionTwoRequest,
	)
	if err != nil {
		t.Fatalf("replay Reevaluate: %v", err)
	}
	if !replayed || revisionTwoReplay.Revision.ID != revisionTwo.Revision.ID {
		t.Fatalf(
			"revision replay = %#v, replayed = %v",
			revisionTwoReplay,
			replayed,
		)
	}

	request.ClientRequestID = "create-retry-after-reevaluation"
	createReplayAfterReevaluation, replayed, err := service.Create(
		ctx,
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("replay Create after re-evaluation: %v", err)
	}
	if !replayed ||
		createReplayAfterReevaluation.ID != created.ID ||
		createReplayAfterReevaluation.Revision.ID != revisionTwo.Revision.ID ||
		createReplayAfterReevaluation.Revision.Number != 2 {
		t.Fatalf(
			"create replay after re-evaluation = %#v, replayed = %v",
			createReplayAfterReevaluation,
			replayed,
		)
	}

	assertEvaluationCounts(t, pool, created.ID, 2, 4)
	var oldStatus string
	if err := pool.QueryRow(ctx, `
		SELECT evaluation_status
		FROM evaluation_revision_states
		WHERE revision_id = $1
	`, created.Revision.ID).Scan(&oldStatus); err != nil {
		t.Fatalf("read superseded status: %v", err)
	}
	if oldStatus != "SUPERSEDED" {
		t.Fatalf("old status = %q, want SUPERSEDED", oldStatus)
	}
	var oldIsFinal bool
	if err := pool.QueryRow(ctx, `
		SELECT is_final
		FROM evaluation_revision_states
		WHERE revision_id = $1
	`, created.Revision.ID).Scan(&oldIsFinal); err != nil {
		t.Fatalf("read superseded final marker: %v", err)
	}
	if oldIsFinal {
		t.Fatal("superseding a Shadow revision promoted it to final")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE evaluation_revisions
		SET pipeline_version = 'tampered/v9'
		WHERE id = $1
	`, revisionTwo.Revision.ID); postgresCode(err) != "55000" {
		t.Fatalf("immutable update error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
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
		VALUES (
			$1, $2, 4, $3, ARRAY['SCENE'], 'interview/v4',
			'pipeline/v4', $4, decode(repeat('ab', 32), 'hex')
		)
	`, created.ID, testOwnerA, revisionTwo.Revision.ID,
		evaluationcore.SchemaVersion); postgresCode(err) != "23514" {
		t.Fatalf("non-contiguous revision error = %v", err)
	}
}

func TestPostgresServiceReevaluateSameConfigCreatesRevisionThenReplays(
	t *testing.T,
) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA, integrationOwnerB)
	service, createRequest := serviceWithEvidenceSnapshot(
		t,
		pool,
		testOwnerA,
	)
	created, replayed, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		createRequest,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if replayed || created.Revision.Number != 1 {
		t.Fatalf("created = %#v, replayed = %v", created, replayed)
	}

	request := evaluationcore.ReevaluateRequest{
		Channels:          createRequest.Channels,
		SceneStrategyRef:  createRequest.SceneStrategyRef,
		Core4DStrategyRef: createRequest.Core4DStrategyRef,
		PipelineVersion:   createRequest.PipelineVersion,
		ClientRequestID:   "same-config-first-trace",
	}
	revisionTwo, replayed, err := service.Reevaluate(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		created.ID,
		request,
	)
	if err != nil {
		t.Fatalf("same-config Reevaluate: %v", err)
	}
	if replayed || revisionTwo.Revision.Number != 2 ||
		revisionTwo.Revision.SupersedesRevisionID != created.Revision.ID {
		t.Fatalf("revision two = %#v, replayed = %v", revisionTwo, replayed)
	}

	request.ClientRequestID = "same-config-retry-trace"
	retry, replayed, err := service.Reevaluate(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		created.ID,
		request,
	)
	if err != nil {
		t.Fatalf("retry same-config Reevaluate: %v", err)
	}
	if !replayed || retry.Revision.ID != revisionTwo.Revision.ID ||
		retry.Revision.Number != revisionTwo.Revision.Number ||
		retry.Revision.ClientRequestID != "same-config-first-trace" {
		t.Fatalf("retry = %#v, replayed = %v", retry, replayed)
	}

	if _, _, err := service.Reevaluate(
		testActorContext(integrationOwnerB),
		testActor(integrationOwnerB),
		created.ID,
		request,
	); !errors.Is(err, evaluationcore.ErrNotFound) {
		t.Fatalf("cross-owner Reevaluate error = %v", err)
	}
	current, err := service.Get(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		created.ID,
	)
	if err != nil {
		t.Fatalf("Get after cross-owner Reevaluate: %v", err)
	}
	if current.Revision.ID != revisionTwo.Revision.ID {
		t.Fatalf(
			"current revision after cross-owner Reevaluate = %q, want %q",
			current.Revision.ID,
			revisionTwo.Revision.ID,
		)
	}
	assertEvaluationCounts(t, pool, created.ID, 2, 2)
}

func TestPostgresServiceReevaluateRetriesFailedSameConfig(
	t *testing.T,
) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	service, createRequest := serviceWithEvidenceSnapshot(
		t,
		pool,
		testOwnerA,
	)
	created, _, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		createRequest,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	request := evaluationcore.ReevaluateRequest{
		Channels:          createRequest.Channels,
		SceneStrategyRef:  createRequest.SceneStrategyRef,
		Core4DStrategyRef: createRequest.Core4DStrategyRef,
		PipelineVersion:   createRequest.PipelineVersion,
		ClientRequestID:   "failed-retry-first-trace",
	}
	failed, replayed, err := service.Reevaluate(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		created.ID,
		request,
	)
	if err != nil || replayed {
		t.Fatalf("first Reevaluate = %#v, replayed = %v, err = %v", failed, replayed, err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'FAILED',
		    is_final = true,
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE revision_id = $1
	`, failed.Revision.ID); err != nil {
		t.Fatalf("mark revision failed: %v", err)
	}
	request.ClientRequestID = "failed-retry-second-trace"
	retry, replayed, err := service.Reevaluate(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		created.ID,
		request,
	)
	if err != nil {
		t.Fatalf("retry failed Reevaluate: %v", err)
	}
	if replayed || retry.Revision.Number != 3 ||
		retry.Revision.SupersedesRevisionID != failed.Revision.ID {
		t.Fatalf("retry = %#v, replayed = %v", retry, replayed)
	}
	assertEvaluationCounts(t, pool, created.ID, 3, 3)
}

func TestPostgresConcurrentReevaluationCreatesOneRevision(t *testing.T) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	service, createRequest := serviceWithEvidenceSnapshot(
		t,
		pool,
		testOwnerA,
	)
	created, _, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		createRequest,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	request := evaluationcore.ReevaluateRequest{
		Channels:         []evaluationcore.Channel{evaluationcore.ChannelScene},
		SceneStrategyRef: "interview/v2",
		PipelineVersion:  "pipeline/v2",
		ClientRequestID:  "concurrent-trace",
	}

	const callers = 12
	start := make(chan struct{})
	results := make(chan evaluationcore.Evaluation, callers)
	failures := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			evaluation, _, reevaluateErr := service.Reevaluate(
				context.Background(),
				testActor(testOwnerA),
				created.ID,
				request,
			)
			if reevaluateErr != nil {
				failures <- reevaluateErr
				return
			}
			results <- evaluation
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		t.Errorf("concurrent Reevaluate: %v", failure)
	}
	var revisionID string
	for result := range results {
		if result.Revision.Number != 2 {
			t.Errorf("revision = %d, want 2", result.Revision.Number)
		}
		if revisionID == "" {
			revisionID = result.Revision.ID
		} else if result.Revision.ID != revisionID {
			t.Errorf("revision id = %q, want %q", result.Revision.ID, revisionID)
		}
	}
	assertEvaluationCounts(t, pool, created.ID, 2, 2)
}

func TestPostgresOutboxFailureRollsBackRevisionAndSupersede(t *testing.T) {
	pool := evaluationDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	service, createRequest := serviceWithEvidenceSnapshot(
		t,
		pool,
		testOwnerA,
	)
	created, _, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		createRequest,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		CREATE FUNCTION reject_evaluation_test_outbox()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			RAISE EXCEPTION 'forced outbox failure';
		END;
		$$;
		CREATE TRIGGER reject_evaluation_test_outbox
		BEFORE INSERT ON evaluation_outbox
		FOR EACH ROW EXECUTE FUNCTION reject_evaluation_test_outbox();
	`); err != nil {
		t.Fatalf("install outbox failure trigger: %v", err)
	}
	_, _, err = service.Reevaluate(
		context.Background(),
		testActor(testOwnerA),
		created.ID,
		evaluationcore.ReevaluateRequest{
			Channels:         []evaluationcore.Channel{evaluationcore.ChannelScene},
			SceneStrategyRef: "interview/v2",
			PipelineVersion:  "pipeline/v2",
		},
	)
	if err == nil {
		t.Fatal("Reevaluate unexpectedly survived outbox failure")
	}
	assertEvaluationCounts(t, pool, created.ID, 1, 1)
	var status string
	if err := pool.QueryRow(context.Background(), `
		SELECT evaluation_status
		FROM evaluation_revision_states
		WHERE revision_id = $1
	`, created.Revision.ID).Scan(&status); err != nil {
		t.Fatalf("read original state: %v", err)
	}
	if status != "QUEUED" {
		t.Fatalf("rolled-back original status = %q, want QUEUED", status)
	}
}

func TestEvaluationMigrationDownRemovesOwnedSchema(t *testing.T) {
	pool := evaluationDatabaseThrough(
		t,
		"000024_evaluation_ledger.up.sql",
	)
	down, err := migrations.Files.ReadFile(
		"000024_evaluation_ledger.down.sql",
	)
	if err != nil {
		t.Fatalf("read Evaluation down migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(down)); err != nil {
		t.Fatalf("apply Evaluation down migration: %v", err)
	}
	for _, table := range []string{
		"evaluation_ledgers",
		"evaluation_revisions",
		"evaluation_revision_states",
		"evaluation_outbox",
	} {
		var exists bool
		if err := pool.QueryRow(context.Background(), `
			SELECT to_regclass($1) IS NOT NULL
		`, table).Scan(&exists); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if exists {
			t.Errorf("%s still exists after down migration", table)
		}
	}
}

var evaluationSchemaSequence atomic.Uint64

func evaluationDatabase(t *testing.T) *pgxpool.Pool {
	return evaluationDatabaseThrough(t, "")
}

func evaluationDatabaseThrough(
	t *testing.T,
	lastMigration string,
) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf(
		"evaluation_%d_%d",
		time.Now().UnixNano(),
		evaluationSchemaSequence.Add(1),
	)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create Evaluation test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, err := admin.Exec(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+schema+" CASCADE",
		); err != nil {
			t.Errorf("drop Evaluation test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated Evaluation pool: %v", err)
	}
	t.Cleanup(pool.Close)
	upMigrations, err := fs.Glob(migrations.Files, "*.up.sql")
	if err != nil {
		t.Fatalf("enumerate migrations: %v", err)
	}
	for _, migration := range upMigrations {
		if lastMigration != "" && migration > lastMigration {
			break
		}
		up, readErr := migrations.Files.ReadFile(migration)
		if readErr != nil {
			t.Fatalf("read %s: %v", migration, readErr)
		}
		if _, applyErr := pool.Exec(ctx, string(up)); applyErr != nil {
			var databaseError *pgconn.PgError
			if errors.As(applyErr, &databaseError) {
				t.Fatalf(
					"apply %s: %v position=%d internal_position=%d where=%s",
					migration,
					applyErr,
					databaseError.Position,
					databaseError.InternalPosition,
					databaseError.Where,
				)
			}
			t.Fatalf("apply %s: %v", migration, applyErr)
		}
	}
	return pool
}

func serviceWithEvidenceSnapshot(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
) (*evaluationcore.Service, evaluationcore.CreateRequest) {
	t.Helper()
	repository := NewPostgresRepository(pool)
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led the migration and reduced release risk.",
	)
	snapshot.OwnerUserID = ownerUserID
	if ownerUserID != testOwnerA {
		var payload evidence.SnapshotPayload
		if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		snapshot = postgresTestSnapshotForOwner(
			t,
			payload,
			evaluationcore.SceneInterview,
			ownerUserID,
		)
	}
	persistEvidenceSnapshotFixture(t, pool, snapshot)
	evidenceRepository := evidence.NewPostgresRepository(pool)
	request := validCreateRequest()
	request.PracticeSessionID = snapshot.PracticeSessionID
	request.InputSnapshotID = snapshot.ID
	request.InputRevision = snapshot.InputRevision
	return evaluationcore.NewService(repository, evidenceRepository), request
}

func insertEvaluationUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	userIDs ...string,
) {
	t.Helper()
	for _, userID := range userIDs {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO identity_users (id, canonical_email)
			VALUES ($1, $2)
		`, userID, "evaluation-"+userID+"@example.test"); err != nil {
			t.Fatalf("insert Identity user %s: %v", userID, err)
		}
	}
}

func assertEvaluationCounts(
	t *testing.T,
	pool *pgxpool.Pool,
	evaluationID string,
	wantRevisions int,
	wantOutbox int,
) {
	t.Helper()
	var revisions int
	var outbox int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM evaluation_revisions
		WHERE evaluation_id = $1
	`, evaluationID).Scan(&revisions); err != nil {
		t.Fatalf("count Evaluation revisions: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM evaluation_outbox
		WHERE evaluation_id = $1
	`, evaluationID).Scan(&outbox); err != nil {
		t.Fatalf("count Evaluation outbox: %v", err)
	}
	if revisions != wantRevisions || outbox != wantOutbox {
		t.Fatalf(
			"counts = revisions %d, outbox %d; want %d, %d",
			revisions,
			outbox,
			wantRevisions,
			wantOutbox,
		)
	}
}

func postgresCode(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	return ""
}
