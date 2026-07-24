// Package migration owns the explicit, repository-backed database migration
// lifecycle. Application startup must not call this package automatically.
package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	gomigrate "github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	migrationfiles "github.com/1024XEngineer/XE3-ESL/server/migrations"
)

const (
	// LockTimeout bounds how long competing migration executors may wait for
	// golang-migrate's PostgreSQL advisory lock.
	LockTimeout = 15 * time.Second

	// ConnectTimeout prevents a migration command from hanging indefinitely
	// while establishing its initial database connection.
	ConnectTimeout = 10 * time.Second

	forceConfirmationPrefix = "schema-inspected:"
	lockTimeoutMargin       = 100 * time.Millisecond
)

var (
	ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")
	ErrDatabaseURLInvalid  = errors.New("DATABASE_URL is invalid")
	ErrDatabaseUnavailable = errors.New("database is unavailable")
	ErrForceVersion        = errors.New("force version must be -1 or greater")
	ErrForceConfirmation   = errors.New("force requires confirmation that the schema was inspected")
	ErrForceRequiresDirty  = errors.New("force requires a dirty migration state")
)

// Status is the current schema_migrations state. Present is false only for a
// clean NilVersion; a failed first down migration remains present as
// version -1 with Dirty set.
type Status struct {
	Version int
	Dirty   bool
	Present bool
}

// Runner executes the embedded, append-only SQL migration history.
type Runner struct {
	migrate  *gomigrate.Migrate
	database migratedatabase.Driver
}

// boundedLockDriver makes the PostgreSQL driver's blocking advisory-lock
// calls terminate on the server as well as in golang-migrate. The upstream
// driver uses context.Background for both its version-table initialization
// lock and regular locks, so migrate.LockTimeout alone cannot cancel them.
type boundedLockDriver struct {
	migratedatabase.Driver
	statementTimeout time.Duration
}

// Open validates databaseURL, establishes a pgx-backed database handle, and
// prepares a migration runner. It never applies a migration implicitly.
func Open(databaseURL string) (*Runner, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, ErrDatabaseURLRequired
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// pgx parse errors retain the original connection string. Do not wrap
		// them because that could disclose credentials in command output.
		return nil, ErrDatabaseURLInvalid
	}
	config.ConnectTimeout = ConnectTimeout

	runner, err := openConfig(config)
	if err != nil {
		// Connection errors can include host and user details. The migration
		// command exposes only a stable operational classification.
		return nil, ErrDatabaseUnavailable
	}
	return runner, nil
}

func openConfig(config *pgx.ConnConfig) (*Runner, error) {
	return openConfigWithFS(config, migrationfiles.Files)
}

func openConfigWithFS(
	config *pgx.ConnConfig,
	migrationFS fs.FS,
) (*Runner, error) {
	return openConfigWithFSAndLockTimeout(
		config,
		migrationFS,
		LockTimeout,
	)
}

func openConfigWithFSAndLockTimeout(
	config *pgx.ConnConfig,
	migrationFS fs.FS,
	lockTimeout time.Duration,
) (*Runner, error) {
	if lockTimeout <= 0 {
		return nil, errors.New("migration lock timeout must be positive")
	}

	sourceDriver, err := iofs.New(migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}

	driverTimeout := boundedDriverTimeout(lockTimeout)
	connectionConfig := config.Copy()
	if connectionConfig.RuntimeParams == nil {
		connectionConfig.RuntimeParams = make(map[string]string)
	}
	connectionConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(
		driverTimeout.Milliseconds(),
	)

	sqlDatabase := stdlib.OpenDB(*connectionConfig)
	sqlDatabase.SetMaxOpenConns(1)
	sqlDatabase.SetMaxIdleConns(1)

	databaseDriver, err := pgxmigrate.WithInstance(sqlDatabase, &pgxmigrate.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		_ = sourceDriver.Close()
		_ = sqlDatabase.Close()
		return nil, fmt.Errorf("initialize migration database: %w", err)
	}

	boundedDriver := &boundedLockDriver{
		Driver:           databaseDriver,
		statementTimeout: driverTimeout,
	}
	if err := boundedDriver.setStatementTimeout(0); err != nil {
		_ = sourceDriver.Close()
		_ = boundedDriver.Close()
		return nil, fmt.Errorf("reset migration statement timeout: %w", err)
	}

	instance, err := gomigrate.NewWithInstance(
		"iofs",
		sourceDriver,
		"pgx5",
		boundedDriver,
	)
	if err != nil {
		_ = sourceDriver.Close()
		_ = boundedDriver.Close()
		return nil, fmt.Errorf("initialize migration runner: %w", err)
	}
	instance.LockTimeout = lockTimeout

	return &Runner{
		migrate:  instance,
		database: boundedDriver,
	}, nil
}

func boundedDriverTimeout(limit time.Duration) time.Duration {
	if limit > lockTimeoutMargin {
		return limit - lockTimeoutMargin
	}
	return limit
}

func (d *boundedLockDriver) Lock() error {
	if err := d.setStatementTimeout(d.statementTimeout); err != nil {
		return err
	}

	if err := d.Driver.Lock(); err != nil {
		return errors.Join(err, d.setStatementTimeout(0))
	}

	if err := d.setStatementTimeout(0); err != nil {
		return errors.Join(err, d.Driver.Unlock())
	}
	return nil
}

func (d *boundedLockDriver) Unlock() error {
	if err := d.setStatementTimeout(d.statementTimeout); err != nil {
		return err
	}

	unlockErr := d.Driver.Unlock()
	resetErr := d.setStatementTimeout(0)
	return errors.Join(unlockErr, resetErr)
}

func (d *boundedLockDriver) setStatementTimeout(timeout time.Duration) error {
	statement := fmt.Sprintf(
		"SET statement_timeout = %d",
		timeout.Milliseconds(),
	)
	if err := d.Driver.Run(strings.NewReader(statement)); err != nil {
		return fmt.Errorf("set migration lock timeout: %w", err)
	}
	return nil
}

// Close releases the embedded source and database resources.
func (r *Runner) Close() error {
	sourceErr, databaseErr := r.migrate.Close()
	return errors.Join(sourceErr, databaseErr)
}

// Up applies all pending migrations. changed is false when the database was
// already current.
func (r *Runner) Up() (changed bool, err error) {
	err = r.migrate.Up()
	if errors.Is(err, gomigrate.ErrNoChange) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("apply migrations: %w", err)
	}
	return true, nil
}

// Version returns the current version and dirty flag without changing schema.
func (r *Runner) Version() (Status, error) {
	version, dirty, err := r.database.Version()
	if err != nil {
		return Status{}, fmt.Errorf("read migration version: %w", err)
	}
	if version == migratedatabase.NilVersion && !dirty {
		return Status{}, nil
	}
	return Status{
		Version: version,
		Dirty:   dirty,
		Present: true,
	}, nil
}

// DownOne reverts exactly one migration. It is exposed only for isolated local
// and test databases; production rollback should use a forward migration.
func (r *Runner) DownOne() (changed bool, err error) {
	err = r.migrate.Steps(-1)
	if errors.Is(err, gomigrate.ErrNoChange) ||
		errors.Is(err, gomigrate.ErrNilVersion) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("revert one migration: %w", err)
	}
	return true, nil
}

// ForceConfirmation returns the exact acknowledgement required by Force.
// Force only repairs migration metadata; callers must inspect the real schema
// before using it.
func ForceConfirmation(version int) string {
	return forceConfirmationPrefix + fmt.Sprint(version)
}

// ValidateForceConfirmation verifies both the target and the operator's
// explicit acknowledgement without touching the database.
func ValidateForceConfirmation(version int, confirmation string) error {
	if version < -1 {
		return ErrForceVersion
	}
	if confirmation != ForceConfirmation(version) {
		return fmt.Errorf(
			"%w: expected %q",
			ErrForceConfirmation,
			ForceConfirmation(version),
		)
	}
	return nil
}

// Force sets the migration version and clears dirty only after an exact,
// version-bound confirmation. It never guesses the actual schema state.
func (r *Runner) Force(version int, confirmation string) (err error) {
	if err := ValidateForceConfirmation(version, confirmation); err != nil {
		return err
	}

	if err := r.database.Lock(); err != nil {
		return fmt.Errorf("lock migration recovery: %w", err)
	}
	defer func() {
		err = errors.Join(err, r.database.Unlock())
	}()

	_, dirty, err := r.database.Version()
	if err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	if !dirty {
		return ErrForceRequiresDirty
	}

	if err := r.database.SetVersion(version, false); err != nil {
		return fmt.Errorf("force migration version: %w", err)
	}
	return nil
}
