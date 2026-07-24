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
)

func TestRunnerAppliesIdempotentlyAndRevertsBaseline(t *testing.T) {
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
