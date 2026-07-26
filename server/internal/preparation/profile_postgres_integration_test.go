package preparation_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

const (
	preparationUserA    = "10000000-0000-4000-8000-000000000111"
	preparationSessionA = "20000000-0000-4000-8000-000000000111"
	preparationUserB    = "10000000-0000-4000-8000-000000000222"
	preparationSessionB = "20000000-0000-4000-8000-000000000222"
)

func TestPostgresProfileRepositoryPersistsReplaysAndScopesResources(
	t *testing.T,
) {
	repository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA, preparationUserB)
	ctx := context.Background()
	actorA := preparationActor(preparationUserA, preparationSessionA)
	actorB := preparationActor(preparationUserB, preparationSessionB)

	profileRequest := preparation.CreateProfileRequest{
		ResumeRef:         "oss://resume/object-v1",
		JobDescriptionRef: "oss://job/object-v1",
		BackgroundSummary: "Backend engineer with distributed systems work.",
	}
	profileCommand := preparation.CreateProfileCommand{
		ProfileID: "profile-original",
		Request:   profileRequest,
		Intent: profileIntent(
			"profile-create-key",
			profileRequest,
		),
	}
	profile, replayed, err := repository.CreateProfile(
		ctx,
		actorA,
		profileCommand,
	)
	if err != nil || replayed {
		t.Fatalf("CreateProfile replayed = %v, error = %v", replayed, err)
	}
	if profile.ID != profileCommand.ProfileID ||
		profile.UserID != actorA.UserID ||
		profile.Version != 1 ||
		profile.UpdatedAt.IsZero() {
		t.Fatal("CreateProfile returned an invalid persisted profile")
	}

	replayCommand := profileCommand
	replayCommand.ProfileID = "profile-replay-must-be-ignored"
	replayedProfile, replayed, err := repository.CreateProfile(
		ctx,
		actorA,
		replayCommand,
	)
	if err != nil || !replayed {
		t.Fatalf("profile replay replayed = %v, error = %v", replayed, err)
	}
	if !reflect.DeepEqual(replayedProfile, profile) {
		t.Fatal("profile replay did not return the original 201 resource")
	}

	changedProfileRequest := profileRequest
	changedProfileRequest.BackgroundSummary = "Changed payload."
	conflictingProfileCommand := profileCommand
	conflictingProfileCommand.Request = changedProfileRequest
	conflictingProfileCommand.Intent = profileIntent(
		profileCommand.Intent.Key,
		changedProfileRequest,
	)
	if _, _, err := repository.CreateProfile(
		ctx,
		actorA,
		conflictingProfileCommand,
	); !errors.Is(err, preparation.ErrProfileIdempotencyConflict) {
		t.Fatalf(
			"changed profile replay error = %v, want idempotency conflict",
			err,
		)
	}

	readProfile, err := repository.ReadProfile(ctx, actorA, profile.ID)
	if err != nil || !reflect.DeepEqual(readProfile, profile) {
		t.Fatalf("ReadProfile did not return the persisted resource: %v", err)
	}
	if _, err := repository.ReadProfile(
		ctx,
		actorB,
		profile.ID,
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("cross-user ReadProfile error = %v, want not found", err)
	}

	snapshotRequest := preparation.CreateSnapshotRequest{SourceVersion: 1}
	snapshotCommand := preparation.CreateSnapshotCommand{
		SnapshotID: "snapshot-original",
		ProfileID:  profile.ID,
		Request:    snapshotRequest,
		Intent: snapshotIntent(
			profile.ID,
			"snapshot-create-key",
			snapshotRequest,
		),
	}
	snapshot, replayed, err := repository.CreateSnapshot(
		ctx,
		actorA,
		snapshotCommand,
	)
	if err != nil || replayed {
		t.Fatalf("CreateSnapshot replayed = %v, error = %v", replayed, err)
	}
	if snapshot.ResumeSnapshot != profile.ResumeRef ||
		snapshot.JobDescriptionSnapshot != profile.JobDescriptionRef ||
		snapshot.BackgroundSnapshot != profile.BackgroundSummary ||
		snapshot.SourceVersion != profile.Version ||
		snapshot.CreatedAt.IsZero() {
		t.Fatal("snapshot did not copy the exact source profile version")
	}

	replaySnapshotCommand := snapshotCommand
	replaySnapshotCommand.SnapshotID = "snapshot-replay-must-be-ignored"
	replayedSnapshot, replayed, err := repository.CreateSnapshot(
		ctx,
		actorA,
		replaySnapshotCommand,
	)
	if err != nil || !replayed {
		t.Fatalf("snapshot replay replayed = %v, error = %v", replayed, err)
	}
	if !reflect.DeepEqual(replayedSnapshot, snapshot) {
		t.Fatal("snapshot replay did not return the original 201 resource")
	}

	changedSnapshotRequest := preparation.CreateSnapshotRequest{
		SourceVersion: 2,
	}
	conflictingSnapshotCommand := snapshotCommand
	conflictingSnapshotCommand.Request = changedSnapshotRequest
	conflictingSnapshotCommand.Intent = snapshotIntent(
		profile.ID,
		snapshotCommand.Intent.Key,
		changedSnapshotRequest,
	)
	if _, _, err := repository.CreateSnapshot(
		ctx,
		actorA,
		conflictingSnapshotCommand,
	); !errors.Is(err, preparation.ErrProfileIdempotencyConflict) {
		t.Fatalf(
			"changed snapshot replay error = %v, want idempotency conflict",
			err,
		)
	}

	wrongVersionCommand := snapshotCommand
	wrongVersionCommand.SnapshotID = "snapshot-wrong-version"
	wrongVersionCommand.Request.SourceVersion = 2
	wrongVersionCommand.Intent = snapshotIntent(
		profile.ID,
		"snapshot-wrong-version-key",
		wrongVersionCommand.Request,
	)
	if _, _, err := repository.CreateSnapshot(
		ctx,
		actorA,
		wrongVersionCommand,
	); !errors.Is(err, preparation.ErrProfileConflict) {
		t.Fatalf("wrong source version error = %v, want conflict", err)
	}

	crossUserCommand := snapshotCommand
	crossUserCommand.SnapshotID = "snapshot-cross-user"
	crossUserCommand.Intent = snapshotIntent(
		profile.ID,
		"snapshot-cross-user-key",
		crossUserCommand.Request,
	)
	if _, _, err := repository.CreateSnapshot(
		ctx,
		actorB,
		crossUserCommand,
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("cross-user CreateSnapshot error = %v, want not found", err)
	}
	if _, err := repository.ReadSnapshot(
		ctx,
		actorB,
		snapshot.ID,
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("cross-user ReadSnapshot error = %v, want not found", err)
	}
	readSnapshot, err := repository.ReadSnapshot(ctx, actorA, snapshot.ID)
	if err != nil || !reflect.DeepEqual(readSnapshot, snapshot) {
		t.Fatalf("ReadSnapshot did not return persisted snapshot: %v", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE preparation_snapshots
		SET background_snapshot = 'mutation must fail'
		WHERE owner_user_id = $1 AND snapshot_id = $2
	`, actorA.UserID, snapshot.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "55000" {
		t.Fatalf("snapshot mutation error = %v, want SQLSTATE 55000", err)
	}
	unchanged, err := repository.ReadSnapshot(ctx, actorA, snapshot.ID)
	if err != nil || !reflect.DeepEqual(unchanged, snapshot) {
		t.Fatal("immutable snapshot changed after rejected database update")
	}

	databaseURL := pool.Config().ConnString()
	pool.Close()
	restartedPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("restart isolated PostgreSQL pool")
	}
	t.Cleanup(restartedPool.Close)
	repository = preparation.NewPostgresProfileRepository(restartedPool)

	restartedProfile, replayed, err := repository.CreateProfile(
		ctx,
		actorA,
		replayCommand,
	)
	if err != nil || !replayed ||
		!reflect.DeepEqual(restartedProfile, profile) {
		t.Fatalf(
			"profile replay after restart replayed = %v, error = %v",
			replayed,
			err,
		)
	}
	restartedSnapshot, replayed, err := repository.CreateSnapshot(
		ctx,
		actorA,
		replaySnapshotCommand,
	)
	if err != nil || !replayed ||
		!reflect.DeepEqual(restartedSnapshot, snapshot) {
		t.Fatalf(
			"snapshot replay after restart replayed = %v, error = %v",
			replayed,
			err,
		)
	}
}

func TestPostgresProfileRepositoryConcurrentIdempotencyIsExactlyOnce(
	t *testing.T,
) {
	repository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	actor := preparationActor(preparationUserA, preparationSessionA)
	request := preparation.CreateProfileRequest{
		BackgroundSummary: "Concurrent request.",
	}
	intent := profileIntent("concurrent-profile-key", request)

	const workers = 24
	start := make(chan struct{})
	results := make(chan concurrentProfileResult, workers)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			profile, replayed, err := repository.CreateProfile(
				context.Background(),
				actor,
				preparation.CreateProfileCommand{
					ProfileID: fmt.Sprintf("profile-concurrent-%02d", index),
					Request:   request,
					Intent:    intent,
				},
			)
			results <- concurrentProfileResult{
				profile:  profile,
				replayed: replayed,
				err:      err,
			}
		}(worker)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	initialWriteCount := 0
	resourceID := ""
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent CreateProfile error = %v", result.err)
		}
		if !result.replayed {
			initialWriteCount++
		}
		if resourceID == "" {
			resourceID = result.profile.ID
		} else if result.profile.ID != resourceID {
			t.Fatal("concurrent replay returned more than one resource")
		}
	}
	if initialWriteCount != 1 {
		t.Fatalf(
			"initial write count = %d, want exactly 1",
			initialWriteCount,
		)
	}

	assertPreparationRowCount(
		t,
		pool,
		"preparation_profiles",
		preparationUserA,
		1,
	)
	assertPreparationRowCount(
		t,
		pool,
		"preparation_idempotency_records",
		preparationUserA,
		1,
	)
}

func TestPostgresProfileRepositoryDeletionFenceAndOrder(t *testing.T) {
	repository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA, preparationUserB)
	ctx := context.Background()
	actorA := preparationActor(preparationUserA, preparationSessionA)
	actorB := preparationActor(preparationUserB, preparationSessionB)

	profileRequest := preparation.CreateProfileRequest{
		BackgroundSummary: "Data that must be deleted.",
	}
	profile, _, err := repository.CreateProfile(
		ctx,
		actorA,
		preparation.CreateProfileCommand{
			ProfileID: "profile-delete",
			Request:   profileRequest,
			Intent:    profileIntent("profile-delete-key", profileRequest),
		},
	)
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	snapshotRequest := preparation.CreateSnapshotRequest{SourceVersion: 1}
	if _, _, err := repository.CreateSnapshot(
		ctx,
		actorA,
		preparation.CreateSnapshotCommand{
			SnapshotID: "snapshot-delete",
			ProfileID:  profile.ID,
			Request:    snapshotRequest,
			Intent: snapshotIntent(
				profile.ID,
				"snapshot-delete-key",
				snapshotRequest,
			),
		},
	); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	jobTargetRepository := preparation.NewPostgresJobTargetRepository(pool)
	jobTargetRequest := preparation.CreateJobTargetRequest{
		Source:   preparation.JobTargetSourceQuickStart,
		JobTitle: "Backend engineer",
	}
	if _, _, err := jobTargetRepository.Create(
		ctx,
		actorA,
		preparation.CreateJobTargetCommand{
			TargetID: "job-target-delete",
			Request:  jobTargetRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets",
				"job-target-delete-key",
				jobTargetRequest,
			),
		},
	); err != nil {
		t.Fatalf("seed job target: %v", err)
	}
	installPreparationDeleteAudit(t, pool)

	if err := repository.DeleteProfileData(
		ctx,
		preparation.DeleteProfileDataCommand{
			UserID:     preparationUserB,
			Generation: 1,
		},
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("active-user deletion error = %v, want not found", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting',
		    updated_at = transaction_timestamp()
		WHERE id = $1
	`, preparationUserA); err != nil {
		t.Fatalf("transition identity user to deleting: %v", err)
	}

	if err := repository.DeleteProfileData(
		ctx,
		preparation.DeleteProfileDataCommand{
			UserID:     preparationUserA,
			Generation: 2,
		},
	); err != nil {
		t.Fatalf("DeleteProfileData generation 2: %v", err)
	}
	assertPreparationDeleteOrder(t, pool)
	for _, table := range []string{
		"preparation_job_target_idempotency_records",
		"preparation_job_targets",
		"preparation_idempotency_records",
		"preparation_snapshots",
		"preparation_profiles",
	} {
		assertPreparationRowCount(t, pool, table, preparationUserA, 0)
	}

	if _, err := repository.ReadProfile(
		ctx,
		actorA,
		profile.ID,
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("read after deletion error = %v, want not found", err)
	}
	if _, _, err := repository.CreateProfile(
		ctx,
		actorA,
		preparation.CreateProfileCommand{
			ProfileID: "profile-must-not-resurrect",
			Request:   profileRequest,
			Intent:    profileIntent("profile-after-delete", profileRequest),
		},
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("write after deletion error = %v, want not found", err)
	}

	deletion := preparation.DeleteProfileDataCommand{
		UserID:     preparationUserA,
		Generation: 2,
	}
	if err := repository.DeleteProfileData(ctx, deletion); err != nil {
		t.Fatalf("same-generation deletion replay: %v", err)
	}
	deletion.Generation = 1
	if err := repository.DeleteProfileData(
		ctx,
		deletion,
	); !errors.Is(err, preparation.ErrProfileDeletionGeneration) {
		t.Fatalf("stale deletion error = %v, want generation conflict", err)
	}

	if _, err := pool.Exec(
		ctx,
		`DELETE FROM identity_users WHERE id = $1`,
		preparationUserA,
	); err != nil {
		t.Fatalf("delete identity row after module cleanup: %v", err)
	}
	deletion.Generation = 2
	if err := repository.DeleteProfileData(ctx, deletion); err != nil {
		t.Fatalf("deletion retry after identity removal: %v", err)
	}
	deletion.Generation = 1
	if err := repository.DeleteProfileData(
		ctx,
		deletion,
	); !errors.Is(err, preparation.ErrProfileDeletionGeneration) {
		t.Fatalf(
			"stale deletion after identity removal error = %v",
			err,
		)
	}

	if _, err := repository.ReadProfile(
		ctx,
		actorB,
		profile.ID,
	); !errors.Is(err, preparation.ErrProfileNotFound) {
		t.Fatalf("other user unexpectedly observed deleted profile: %v", err)
	}
}

func TestPostgresProfileRepositoryBlocksRequestAcrossDeletionTransition(
	t *testing.T,
) {
	repository, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	ctx := context.Background()
	actor := preparationActor(preparationUserA, preparationSessionA)
	request := preparation.CreateProfileRequest{
		BackgroundSummary: "Request started before account transition.",
	}

	transition, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin identity transition: %v", err)
	}
	defer func() { _ = transition.Rollback(ctx) }()
	if _, err := transition.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting',
		    updated_at = transaction_timestamp()
		WHERE id = $1
	`, preparationUserA); err != nil {
		t.Fatalf("lock identity transition: %v", err)
	}

	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, _, err := repository.CreateProfile(
			context.Background(),
			actor,
			preparation.CreateProfileCommand{
				ProfileID: "profile-stale-request",
				Request:   request,
				Intent:    profileIntent("profile-stale-request", request),
			},
		)
		result <- err
	}()
	<-started

	select {
	case err := <-result:
		t.Fatalf("stale request passed the uncommitted transition: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := transition.Commit(ctx); err != nil {
		t.Fatalf("commit identity transition: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, preparation.ErrProfileNotFound) {
			t.Fatalf("stale request error = %v, want not found", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale request did not finish after identity transition")
	}
	if err := repository.DeleteProfileData(
		ctx,
		preparation.DeleteProfileDataCommand{
			UserID:     preparationUserA,
			Generation: 1,
		},
	); err != nil {
		t.Fatalf("DeleteProfileData after transition: %v", err)
	}
	assertPreparationRowCount(
		t,
		pool,
		"preparation_profiles",
		preparationUserA,
		0,
	)
}

type concurrentProfileResult struct {
	profile  preparation.Profile
	replayed bool
	err      error
}

func preparationActor(
	userID string,
	sessionID string,
) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    userID,
		SessionID: sessionID,
	}
}

func profileIntent(
	key string,
	request preparation.CreateProfileRequest,
) preparation.IdempotencyIntent {
	return preparationIntent(
		"/v1/preparation-profiles",
		key,
		request,
	)
}

func snapshotIntent(
	profileID string,
	key string,
	request preparation.CreateSnapshotRequest,
) preparation.IdempotencyIntent {
	return preparationIntent(
		"/v1/preparation-profiles/"+profileID+"/snapshots",
		key,
		request,
	)
}

func preparationIntent(
	path string,
	key string,
	request any,
) preparation.IdempotencyIntent {
	encoded, err := json.Marshal(request)
	if err != nil {
		panic("test request must be JSON encodable")
	}
	return preparation.IdempotencyIntent{
		Method:             "POST",
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(encoded),
	}
}

func insertPreparationUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	userIDs ...string,
) {
	t.Helper()
	for index, userID := range userIDs {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO identity_users (id, canonical_email)
			VALUES ($1, $2)
		`,
			userID,
			fmt.Sprintf("preparation-%d@example.test", index),
		); err != nil {
			t.Fatalf("insert identity fixture: %v", err)
		}
	}
}

func assertPreparationRowCount(
	t *testing.T,
	pool *pgxpool.Pool,
	table string,
	userID string,
	want int,
) {
	t.Helper()
	allowed := map[string]bool{
		"preparation_job_target_idempotency_records": true,
		"preparation_job_targets":                    true,
		"preparation_idempotency_records":            true,
		"preparation_snapshots":                      true,
		"preparation_profiles":                       true,
	}
	if !allowed[table] {
		t.Fatalf("unsupported test table %q", table)
	}
	var got int
	query := "SELECT count(*) FROM " + pgx.Identifier{table}.Sanitize() +
		" WHERE owner_user_id = $1"
	if err := pool.QueryRow(
		context.Background(),
		query,
		userID,
	).Scan(&got); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}

func installPreparationDeleteAudit(
	t *testing.T,
	pool *pgxpool.Pool,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE preparation_delete_audit (
			position bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			resource_kind text NOT NULL
		);

		CREATE FUNCTION record_preparation_delete()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			INSERT INTO preparation_delete_audit (resource_kind)
			VALUES (TG_ARGV[0]);
			RETURN OLD;
		END;
		$$;

		CREATE TRIGGER preparation_idempotency_delete_audit
		BEFORE DELETE ON preparation_idempotency_records
		FOR EACH ROW EXECUTE FUNCTION record_preparation_delete('idempotency');

		CREATE TRIGGER preparation_snapshot_delete_audit
		BEFORE DELETE ON preparation_snapshots
		FOR EACH ROW EXECUTE FUNCTION record_preparation_delete('snapshot');

		CREATE TRIGGER preparation_profile_delete_audit
		BEFORE DELETE ON preparation_profiles
		FOR EACH ROW EXECUTE FUNCTION record_preparation_delete('profile');
	`); err != nil {
		t.Fatalf("install deletion-order audit: %v", err)
	}
}

func assertPreparationDeleteOrder(
	t *testing.T,
	pool *pgxpool.Pool,
) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT resource_kind
		FROM preparation_delete_audit
		ORDER BY position
	`)
	if err != nil {
		t.Fatalf("read deletion-order audit: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var resourceKind string
		if err := rows.Scan(&resourceKind); err != nil {
			t.Fatalf("scan deletion-order audit: %v", err)
		}
		got = append(got, resourceKind)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate deletion-order audit: %v", err)
	}
	want := []string{"idempotency", "idempotency", "snapshot", "profile"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deletion order = %v, want %v", got, want)
	}
}

var preparationSchemaCounter atomic.Uint64

func newPreparationRepository(
	t *testing.T,
) (*preparation.PostgresProfileRepository, *pgxpool.Pool) {
	t.Helper()
	databaseURL, cleanup := isolatedPreparationDatabaseURL(t)

	runner, err := migration.Open(databaseURL)
	if err != nil {
		cleanup()
		t.Fatal("open isolated migration runner")
	}
	changed, err := runner.Up()
	closeErr := runner.Close()
	if err != nil || closeErr != nil || !changed {
		cleanup()
		t.Fatalf(
			"apply isolated migration stack: changed=%v migration_error=%v close_error=%v",
			changed,
			err,
			closeErr,
		)
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		cleanup()
		t.Fatal("parse isolated PostgreSQL pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		cleanup()
		t.Fatal("open isolated PostgreSQL pool")
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup()
	})
	return preparation.NewPostgresProfileRepository(pool), pool
}

func isolatedPreparationDatabaseURL(t *testing.T) (string, func()) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is invalid")
	}

	adminConfig, err := pgx.ParseConfig(baseURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is invalid")
	}
	admin, err := pgx.ConnectConfig(
		context.Background(),
		adminConfig,
	)
	if err != nil {
		t.Fatal("connect to PostgreSQL test database")
	}

	schema := fmt.Sprintf(
		"preparation_test_%d_%d",
		time.Now().UnixNano(),
		preparationSchemaCounter.Add(1),
	)
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		_ = admin.Close(context.Background())
		t.Fatal("create isolated Preparation schema")
	}

	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(
			ctx,
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated Preparation schema: %v", err)
		}
		if err := admin.Close(ctx); err != nil {
			t.Errorf("close PostgreSQL admin connection: %v", err)
		}
	}
	return parsedURL.String(), cleanup
}
