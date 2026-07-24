package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunnerAppliesIdempotentlyAndRevertsLatestMigration(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)

	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	defer func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	}()

	changed, err := runner.Up()
	if err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if !changed {
		t.Fatal("first Up reported no change")
	}

	status, err := runner.Version()
	if err != nil {
		t.Fatalf("Version after Up: %v", err)
	}
	if !status.Present || status.Version == 0 || status.Dirty {
		t.Fatalf("status after Up = %+v, want a positive clean version", status)
	}
	latestVersion := status.Version

	changed, err = runner.Up()
	if err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if changed {
		t.Fatal("second Up reported a change")
	}

	changed, err = runner.DownOne()
	if err != nil {
		t.Fatalf("DownOne: %v", err)
	}
	if !changed {
		t.Fatal("DownOne reported no change")
	}

	status, err = runner.Version()
	if err != nil {
		t.Fatalf("Version after DownOne: %v", err)
	}
	if latestVersion == 1 {
		if status.Present {
			t.Fatalf("status after DownOne = %+v, want no applied version", status)
		}
	} else if !status.Present || status.Dirty || status.Version >= latestVersion {
		t.Fatalf(
			"status after DownOne = %+v, want a clean version before %d",
			status,
			latestVersion,
		)
	}

	changed, err = runner.Up()
	if err != nil {
		t.Fatalf("Up after DownOne: %v", err)
	}
	if !changed {
		t.Fatal("Up after DownOne reported no change")
	}

	status, err = runner.Version()
	if err != nil {
		t.Fatalf("Version after re-applying migrations: %v", err)
	}
	if !status.Present || status.Version != latestVersion || status.Dirty {
		t.Fatalf(
			"status after re-applying migrations = %+v, want version %d and clean",
			status,
			latestVersion,
		)
	}

	if err := runner.Force(
		int(latestVersion),
		ForceConfirmation(int(latestVersion)),
	); !errors.Is(err, ErrForceRequiresDirty) {
		t.Fatalf("Force on clean database error = %v, want %v", err, ErrForceRequiresDirty)
	}
}

func TestIdentityMigrationEnforcesConstraintsAndIndexes(t *testing.T) {
	migrationConfig, admin, schema := isolatedMigrationConfig(t)

	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	changed, err := runner.Up()
	if err != nil {
		t.Fatalf("apply identity migration: %v", err)
	}
	if !changed {
		t.Fatal("identity migration reported no change on an empty schema")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	users := pgx.Identifier{schema, "identity_users"}.Sanitize()
	credentials := pgx.Identifier{schema, "identity_credentials"}.Sanitize()
	sessions := pgx.Identifier{schema, "identity_auth_sessions"}.Sanitize()

	const (
		firstUserID     = "10000000-0000-4000-8000-000000000001"
		secondUserID    = "10000000-0000-4000-8000-000000000002"
		unknownUserID   = "10000000-0000-4000-8000-000000000099"
		firstSessionID  = "20000000-0000-4000-8000-000000000001"
		secondSessionID = "20000000-0000-4000-8000-000000000002"
		thirdSessionID  = "20000000-0000-4000-8000-000000000003"
		passwordHash    = "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo"
	)
	createdAt := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(24 * time.Hour)

	if _, err := admin.Exec(
		ctx,
		"INSERT INTO "+users+
			" (id, canonical_email, created_at, updated_at) VALUES ($1, $2, $3, $3)",
		firstUserID,
		"first@example.com",
		createdAt,
	); err != nil {
		t.Fatalf("insert first user: %v", err)
	}
	if _, err := admin.Exec(
		ctx,
		"INSERT INTO "+users+
			" (id, canonical_email, created_at, updated_at) VALUES ($1, $2, $3, $3)",
		secondUserID,
		"second@example.com",
		createdAt,
	); err != nil {
		t.Fatalf("insert second user: %v", err)
	}
	if _, err := admin.Exec(
		ctx,
		"INSERT INTO "+credentials+" (user_id, password_hash, updated_at) VALUES ($1, $2, $3)",
		firstUserID,
		passwordHash,
		createdAt,
	); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := admin.Exec(
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)",
		firstSessionID,
		firstUserID,
		bytesOf(0x11, 32),
		createdAt,
		expiresAt,
	); err != nil {
		t.Fatalf("insert first session: %v", err)
	}
	if _, err := admin.Exec(
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)",
		secondSessionID,
		firstUserID,
		bytesOf(0x22, 32),
		createdAt.Add(time.Minute),
		expiresAt,
	); err != nil {
		t.Fatalf("insert second session for same user: %v", err)
	}

	var sessionCount int
	if err := admin.QueryRow(
		ctx,
		"SELECT count(*) FROM "+sessions+" WHERE user_id = $1",
		firstUserID,
	).Scan(&sessionCount); err != nil {
		t.Fatalf("count multiple sessions: %v", err)
	}
	if sessionCount != 2 {
		t.Fatalf("session count = %d, want 2", sessionCount)
	}

	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+users+" (id, canonical_email) VALUES ($1, $2)",
		[]any{"10000000-0000-4000-8000-000000000003", "first@example.com"},
		"23505",
		"identity_users_canonical_email_key",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+users+" (id, canonical_email) VALUES ($1, $2)",
		[]any{"10000000-0000-4000-8000-000000000004", "Upper@example.com"},
		"23514",
		"identity_users_canonical_email_lowercase_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+users+" (id, canonical_email) VALUES ($1, $2)",
		[]any{"10000000-0000-4000-8000-000000000005", "space @example.com"},
		"23514",
		"identity_users_canonical_email_ascii_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+users+" (id, canonical_email, account_status) VALUES ($1, $2, $3)",
		[]any{"10000000-0000-4000-8000-000000000006", "status@example.com", "disabled"},
		"23514",
		"identity_users_account_status_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+users+
			" (id, canonical_email, created_at, updated_at) VALUES ($1, $2, $3, $4)",
		[]any{
			"10000000-0000-4000-8000-000000000007",
			"time@example.com",
			createdAt,
			createdAt.Add(-time.Second),
		},
		"23514",
		"identity_users_timestamps_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+credentials+" (user_id, password_hash) VALUES ($1, $2)",
		[]any{firstUserID, passwordHash},
		"23505",
		"identity_credentials_pkey",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+credentials+" (user_id, password_hash) VALUES ($1, $2)",
		[]any{unknownUserID, passwordHash},
		"23503",
		"identity_credentials_user_id_fkey",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+credentials+" (user_id, password_hash) VALUES ($1, $2)",
		[]any{secondUserID, strings.Repeat("x", 80)},
		"23514",
		"identity_credentials_password_hash_phc_shape_check",
	)
	malformedPasswordHashes := []struct {
		name string
		hash string
	}{
		{
			name: "missing version",
			hash: "$argon2id$$m=65536,t=3,p=4$" +
				strings.Repeat("c", 24) + "$" + strings.Repeat("d", 43),
		},
		{
			name: "missing parameters",
			hash: "$argon2id$v=19$$" +
				strings.Repeat("c", 24) + "$" + strings.Repeat("d", 43),
		},
		{
			name: "missing salt",
			hash: "$argon2id$v=19$m=65536,t=3,p=4$$" +
				strings.Repeat("d", 43),
		},
		{
			name: "missing hash",
			hash: "$argon2id$v=19$m=65536,t=3,p=4$" +
				strings.Repeat("c", 64) + "$",
		},
	}
	for _, malformedPasswordHash := range malformedPasswordHashes {
		t.Run("rejects PHC "+malformedPasswordHash.name, func(t *testing.T) {
			assertConstraintViolation(
				t,
				admin,
				ctx,
				"INSERT INTO "+credentials+" (user_id, password_hash) VALUES ($1, $2)",
				[]any{secondUserID, malformedPasswordHash.hash},
				"23514",
				"identity_credentials_password_hash_phc_shape_check",
			)
		})
	}
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)",
		[]any{thirdSessionID, unknownUserID, bytesOf(0x33, 32), createdAt, expiresAt},
		"23503",
		"identity_auth_sessions_user_id_fkey",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)",
		[]any{thirdSessionID, secondUserID, bytesOf(0x33, 31), createdAt, expiresAt},
		"23514",
		"identity_auth_sessions_token_digest_length_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)",
		[]any{thirdSessionID, secondUserID, bytesOf(0x11, 32), createdAt, expiresAt},
		"23505",
		"identity_auth_sessions_token_digest_key",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at) VALUES ($1, $2, $3, $4, $4)",
		[]any{thirdSessionID, secondUserID, bytesOf(0x33, 32), createdAt},
		"23514",
		"identity_auth_sessions_expiry_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at, revoked_at)"+
			" VALUES ($1, $2, $3, $4, $5, $6)",
		[]any{
			thirdSessionID,
			secondUserID,
			bytesOf(0x33, 32),
			createdAt,
			expiresAt,
			createdAt.Add(time.Minute),
		},
		"23514",
		"identity_auth_sessions_revocation_pair_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at, revoked_at, revocation_reason)"+
			" VALUES ($1, $2, $3, $4, $5, $6, $7)",
		[]any{
			thirdSessionID,
			secondUserID,
			bytesOf(0x33, 32),
			createdAt,
			expiresAt,
			createdAt.Add(-time.Second),
			"logout",
		},
		"23514",
		"identity_auth_sessions_revoked_at_check",
	)
	assertConstraintViolation(
		t,
		admin,
		ctx,
		"INSERT INTO "+sessions+
			" (id, user_id, token_digest, created_at, expires_at, revoked_at, revocation_reason)"+
			" VALUES ($1, $2, $3, $4, $5, $6, $7)",
		[]any{
			thirdSessionID,
			secondUserID,
			bytesOf(0x33, 32),
			createdAt,
			expiresAt,
			createdAt.Add(time.Minute),
			"contains spaces",
		},
		"23514",
		"identity_auth_sessions_revocation_reason_check",
	)

	assertIdentityIndexes(t, admin, ctx, schema)
	assertNoRawIdentitySecrets(t, admin, ctx, schema)

	if _, err := admin.Exec(ctx, "DELETE FROM "+users+" WHERE id = $1", firstUserID); err != nil {
		t.Fatalf("delete user for cascade check: %v", err)
	}
	var credentialCount int
	if err := admin.QueryRow(
		ctx,
		"SELECT count(*) FROM "+credentials+" WHERE user_id = $1",
		firstUserID,
	).Scan(&credentialCount); err != nil {
		t.Fatalf("count credentials after user delete: %v", err)
	}
	if credentialCount != 0 {
		t.Fatalf("credential count after user delete = %d, want 0", credentialCount)
	}
	if err := admin.QueryRow(
		ctx,
		"SELECT count(*) FROM "+sessions+" WHERE user_id = $1",
		firstUserID,
	).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions after user delete: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("session count after user delete = %d, want 0", sessionCount)
	}

	changed, err = runner.DownOne()
	if err != nil {
		t.Fatalf("revert identity migration: %v", err)
	}
	if !changed {
		t.Fatal("identity down migration reported no change")
	}
	status, err := runner.Version()
	if err != nil {
		t.Fatalf("read version after identity down migration: %v", err)
	}
	if !status.Present || status.Version != 1 || status.Dirty {
		t.Fatalf("status after identity down migration = %+v, want version 1 clean", status)
	}
	for _, table := range []string{
		"identity_users",
		"identity_credentials",
		"identity_auth_sessions",
	} {
		var relation *string
		if err := admin.QueryRow(
			ctx,
			"SELECT to_regclass($1)",
			schema+"."+table,
		).Scan(&relation); err != nil {
			t.Fatalf("inspect %s after down migration: %v", table, err)
		}
		if relation != nil {
			t.Fatalf("%s still exists after down migration as %q", table, *relation)
		}
	}

	changed, err = runner.Up()
	if err != nil {
		t.Fatalf("reapply identity migration from baseline: %v", err)
	}
	if !changed {
		t.Fatal("identity migration reported no change when reapplied from baseline")
	}
}

func TestNilVersionDirtyStateIsVisibleAndRecoverable(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)

	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	if err := runner.database.Lock(); err != nil {
		t.Fatalf("lock migration driver: %v", err)
	}
	setVersionErr := runner.database.SetVersion(
		migratedatabase.NilVersion,
		true,
	)
	unlockErr := runner.database.Unlock()
	if err := errors.Join(setVersionErr, unlockErr); err != nil {
		t.Fatalf("create nil-version dirty state: %v", err)
	}

	status, err := runner.Version()
	if err != nil {
		t.Fatalf("Version in nil-version dirty state: %v", err)
	}
	if !status.Present ||
		status.Version != migratedatabase.NilVersion ||
		!status.Dirty {
		t.Fatalf(
			"status = %+v, want version %d dirty true",
			status,
			migratedatabase.NilVersion,
		)
	}

	if err := runner.Force(
		migratedatabase.NilVersion,
		ForceConfirmation(migratedatabase.NilVersion),
	); err != nil {
		t.Fatalf("Force nil-version dirty state: %v", err)
	}

	status, err = runner.Version()
	if err != nil {
		t.Fatalf("Version after Force: %v", err)
	}
	if status.Present || status.Dirty {
		t.Fatalf("status after Force = %+v, want no applied version", status)
	}
}

func TestConcurrentRunnersSerializeBaseline(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)

	first, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open first migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first migration runner: %v", err)
		}
	})

	second, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open second migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second migration runner: %v", err)
		}
	})

	type result struct {
		changed bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)

	for _, runner := range []*Runner{first, second} {
		runner := runner
		go func() {
			<-start
			changed, err := runner.Up()
			results <- result{changed: changed, err: err}
		}()
	}
	close(start)

	changedCount := 0
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent Up: %v", result.err)
			}
			if result.changed {
				changedCount++
			}
		case <-time.After(LockTimeout + 5*time.Second):
			t.Fatal("concurrent Up did not serialize within the lock timeout")
		}
	}

	if changedCount != 1 {
		t.Fatalf("changed runner count = %d, want exactly 1", changedCount)
	}

	status, err := first.Version()
	if err != nil {
		t.Fatalf("Version after concurrent Up: %v", err)
	}
	if !status.Present || status.Version == 0 || status.Dirty {
		t.Fatalf(
			"status after concurrent Up = %+v, want a positive clean version",
			status,
		)
	}
}

func TestRunnerInitializationLockIsBounded(t *testing.T) {
	migrationConfig, admin, schema := isolatedMigrationConfig(t)

	var databaseName string
	if err := admin.QueryRow(
		context.Background(),
		"SELECT current_database()",
	).Scan(&databaseName); err != nil {
		t.Fatalf("read current database: %v", err)
	}

	lockID, err := migratedatabase.GenerateAdvisoryLockId(
		databaseName,
		schema,
		"schema_migrations",
	)
	if err != nil {
		t.Fatalf("generate migration lock ID: %v", err)
	}
	if _, err := admin.Exec(
		context.Background(),
		"SELECT pg_advisory_lock($1)",
		lockID,
	); err != nil {
		t.Fatalf("hold migration advisory lock: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"SELECT pg_advisory_unlock($1)",
			lockID,
		); err != nil {
			t.Errorf("release migration advisory lock: %v", err)
		}
	})

	const testLockTimeout = 300 * time.Millisecond
	startedAt := time.Now()
	runner, err := openConfigWithFSAndLockTimeout(
		migrationConfig,
		fstest.MapFS{
			"000001_lock_test.up.sql": {
				Data: []byte("BEGIN;\nSELECT 1;\nCOMMIT;\n"),
			},
			"000001_lock_test.down.sql": {
				Data: []byte("BEGIN;\nSELECT 1;\nCOMMIT;\n"),
			},
		},
		testLockTimeout,
	)
	elapsed := time.Since(startedAt)
	if runner != nil {
		_ = runner.Close()
	}
	if err == nil {
		t.Fatal("runner initialization unexpectedly acquired a held lock")
	}
	if elapsed > 2*time.Second {
		t.Fatalf(
			"runner initialization exceeded bounded lock wait: %s",
			elapsed,
		)
	}
}

func TestFailedTransactionalMigrationStaysDirtyAndRollsBack(t *testing.T) {
	migrationConfig, admin, schema := isolatedMigrationConfig(t)

	failedMigration := fstest.MapFS{
		"000001_intentional_failure.up.sql": {
			Data: []byte(`BEGIN;
CREATE TABLE dirty_probe (id integer);
SELECT * FROM relation_that_must_not_exist;
COMMIT;
`),
		},
		"000001_intentional_failure.down.sql": {
			Data: []byte(`BEGIN;
DROP TABLE IF EXISTS dirty_probe;
COMMIT;
`),
		},
	}

	runner, err := openConfigWithFS(migrationConfig, failedMigration)
	if err != nil {
		t.Fatalf("open failing migration runner: %v", err)
	}

	if _, err := runner.Up(); err == nil {
		t.Fatal("failing migration unexpectedly succeeded")
	}
	// A failed explicit transaction leaves that session aborted. The command
	// closes it on exit; a fresh invocation then observes the durable dirty
	// marker while PostgreSQL rolls the failed transaction back.
	if err := runner.Close(); err != nil {
		t.Fatalf("close failing migration runner: %v", err)
	}

	inspector, err := openConfigWithFS(migrationConfig, failedMigration)
	if err != nil {
		t.Fatalf("open migration inspector: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Errorf("close migration inspector: %v", err)
		}
	})

	status, err := inspector.Version()
	if err != nil {
		t.Fatalf("Version after failed Up: %v", err)
	}
	if !status.Present || status.Version != 1 || !status.Dirty {
		t.Fatalf("status after failed Up = %+v, want version 1 and dirty", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var probe *string
	if err := admin.QueryRow(
		ctx,
		"SELECT to_regclass($1)",
		schema+".dirty_probe",
	).Scan(&probe); err != nil {
		t.Fatalf("inspect transactional probe: %v", err)
	}
	if probe != nil {
		t.Fatalf("dirty_probe still exists as %q; failed transaction was not rolled back", *probe)
	}

	if err := inspector.Force(-1, ForceConfirmation(-1)); err != nil {
		t.Fatalf("Force after schema inspection: %v", err)
	}
	status, err = inspector.Version()
	if err != nil {
		t.Fatalf("Version after Force recovery: %v", err)
	}
	if status.Present {
		t.Fatalf("status after Force recovery = %+v, want no applied version", status)
	}
}

func assertConstraintViolation(
	t *testing.T,
	conn *pgx.Conn,
	ctx context.Context,
	query string,
	arguments []any,
	code string,
	constraint string,
) {
	t.Helper()

	_, err := conn.Exec(ctx, query, arguments...)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded; want constraint %s", constraint)
	}

	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("statement error = %v, want PostgreSQL constraint violation", err)
	}
	if postgresError.Code != code || postgresError.ConstraintName != constraint {
		t.Fatalf(
			"statement error code/constraint = %s/%s, want %s/%s",
			postgresError.Code,
			postgresError.ConstraintName,
			code,
			constraint,
		)
	}
}

func assertIdentityIndexes(
	t *testing.T,
	conn *pgx.Conn,
	ctx context.Context,
	schema string,
) {
	t.Helper()

	rows, err := conn.Query(
		ctx,
		`SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = $1
  AND tablename IN ('identity_users', 'identity_auth_sessions')`,
		schema,
	)
	if err != nil {
		t.Fatalf("query identity indexes: %v", err)
	}
	defer rows.Close()

	indexes := make(map[string]string)
	for rows.Next() {
		var name string
		var definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatalf("scan identity index: %v", err)
		}
		indexes[name] = definition
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate identity indexes: %v", err)
	}

	expectedFragments := map[string][]string{
		"identity_users_canonical_email_key": {
			"UNIQUE INDEX",
			"(canonical_email)",
		},
		"identity_auth_sessions_token_digest_key": {
			"UNIQUE INDEX",
			"(token_digest)",
		},
		"identity_auth_sessions_user_created_idx": {
			"(user_id, created_at DESC)",
		},
		"identity_auth_sessions_active_user_idx": {
			"(user_id)",
			"WHERE (revoked_at IS NULL)",
		},
		"identity_auth_sessions_active_expiry_idx": {
			"(expires_at)",
			"WHERE (revoked_at IS NULL)",
		},
	}
	for name, fragments := range expectedFragments {
		definition, ok := indexes[name]
		if !ok {
			t.Errorf("identity index %s is missing", name)
			continue
		}
		for _, fragment := range fragments {
			if !strings.Contains(definition, fragment) {
				t.Errorf(
					"identity index %s definition %q does not contain %q",
					name,
					definition,
					fragment,
				)
			}
		}
	}
}

func assertNoRawIdentitySecrets(
	t *testing.T,
	conn *pgx.Conn,
	ctx context.Context,
	schema string,
) {
	t.Helper()

	var forbiddenColumnCount int
	if err := conn.QueryRow(
		ctx,
		`SELECT count(*)
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name IN (
      'identity_users',
      'identity_credentials',
      'identity_auth_sessions'
  )
  AND (
      (
          column_name ~ '(^|_)(password|token|authorization)(_|$)'
          AND column_name NOT IN ('password_hash', 'token_digest')
      )
      OR column_name = 'actor_id'
  )`,
		schema,
	).Scan(&forbiddenColumnCount); err != nil {
		t.Fatalf("inspect forbidden identity columns: %v", err)
	}
	if forbiddenColumnCount != 0 {
		t.Fatalf("found %d forbidden raw identity columns", forbiddenColumnCount)
	}
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func isolatedMigrationConfig(
	t *testing.T,
) (*pgx.ConnConfig, *pgx.Conn, string) {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnectTimeout = ConnectTimeout

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(context.Background()); err != nil {
			t.Errorf("close integration database: %v", err)
		}
	})

	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if _, err := admin.Exec(dropCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	migrationConfig := config.Copy()
	migrationConfig.RuntimeParams["search_path"] = schema
	return migrationConfig, admin, schema
}
