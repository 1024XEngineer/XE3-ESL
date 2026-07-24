package migration

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	migratedatabase "github.com/golang-migrate/migrate/v4/database"
)

func TestValidateForceConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		version      int
		confirmation string
		wantErr      error
	}{
		{
			name:         "matching version",
			version:      7,
			confirmation: "schema-inspected:7",
		},
		{
			name:         "nil version recovery",
			version:      -1,
			confirmation: "schema-inspected:-1",
		},
		{
			name:         "wrong version",
			version:      7,
			confirmation: "schema-inspected:6",
			wantErr:      ErrForceConfirmation,
		},
		{
			name:         "missing confirmation",
			version:      7,
			confirmation: "",
			wantErr:      ErrForceConfirmation,
		},
		{
			name:         "invalid version",
			version:      -2,
			confirmation: "schema-inspected:-2",
			wantErr:      ErrForceVersion,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateForceConfirmation(test.version, test.confirmation)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}
}

func TestForceChecksDirtyAndWritesWhileHoldingMigrationLock(t *testing.T) {
	t.Parallel()

	driver := &forceSequenceDriver{
		version: 4,
		dirty:   true,
	}
	runner := &Runner{database: driver}

	if err := runner.Force(3, ForceConfirmation(3)); err != nil {
		t.Fatalf("Force: %v", err)
	}

	wantCalls := []string{"lock", "version", "set-version", "unlock"}
	if !reflect.DeepEqual(driver.calls, wantCalls) {
		t.Fatalf("driver calls = %v, want %v", driver.calls, wantCalls)
	}
	if driver.version != 3 || driver.dirty {
		t.Fatalf(
			"driver state = version %d dirty %t, want version 3 dirty false",
			driver.version,
			driver.dirty,
		)
	}
}

func TestMigrationTimeoutsStayBounded(t *testing.T) {
	t.Parallel()

	if LockTimeout != 15*time.Second {
		t.Fatalf("LockTimeout = %s, want 15s", LockTimeout)
	}
	if ConnectTimeout <= 0 {
		t.Fatalf("ConnectTimeout = %s, want a positive duration", ConnectTimeout)
	}
}

func TestOpenRejectsMissingDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := Open(" \t ")
	if !errors.Is(err, ErrDatabaseURLRequired) {
		t.Fatalf("Open error = %v, want %v", err, ErrDatabaseURLRequired)
	}
}

func TestOpenRedactsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	const databaseURL = "postgres://sensitive-user:super-secret@localhost:not-a-port/database"
	_, err := Open(databaseURL)
	if !errors.Is(err, ErrDatabaseURLInvalid) {
		t.Fatalf("Open error = %v, want %v", err, ErrDatabaseURLInvalid)
	}
	for _, sensitive := range []string{
		databaseURL,
		"sensitive-user",
		"super-secret",
		"localhost",
	} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("Open error %q contains sensitive value %q", err, sensitive)
		}
	}
}

func TestOpenRedactsConnectionFailure(t *testing.T) {
	const databaseURL = "postgres://sensitive-user:super-secret@127.0.0.1:1/database?sslmode=disable"

	_, err := Open(databaseURL)
	if !errors.Is(err, ErrDatabaseUnavailable) {
		t.Fatalf("Open error = %v, want %v", err, ErrDatabaseUnavailable)
	}
	for _, sensitive := range []string{
		databaseURL,
		"sensitive-user",
		"super-secret",
		"127.0.0.1",
	} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("Open error %q contains sensitive value %q", err, sensitive)
		}
	}
}

type forceSequenceDriver struct {
	migratedatabase.Driver
	calls   []string
	locked  bool
	version int
	dirty   bool
}

func (d *forceSequenceDriver) Lock() error {
	d.calls = append(d.calls, "lock")
	if d.locked {
		return migratedatabase.ErrLocked
	}
	d.locked = true
	return nil
}

func (d *forceSequenceDriver) Unlock() error {
	d.calls = append(d.calls, "unlock")
	if !d.locked {
		return migratedatabase.ErrNotLocked
	}
	d.locked = false
	return nil
}

func (d *forceSequenceDriver) Version() (int, bool, error) {
	d.calls = append(d.calls, "version")
	if !d.locked {
		return 0, false, errors.New("version read outside migration lock")
	}
	return d.version, d.dirty, nil
}

func (d *forceSequenceDriver) SetVersion(version int, dirty bool) error {
	d.calls = append(d.calls, "set-version")
	if !d.locked {
		return errors.New("version written outside migration lock")
	}
	d.version = version
	d.dirty = dirty
	return nil
}
