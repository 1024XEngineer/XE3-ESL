package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	platformmigration "github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		want      command
		wantErr   error
		errorText string
	}{
		{
			name: "up",
			args: []string{"up"},
			want: command{kind: commandUp},
		},
		{
			name: "version",
			args: []string{"version"},
			want: command{kind: commandVersion},
		},
		{
			name: "status alias",
			args: []string{"status"},
			want: command{kind: commandVersion},
		},
		{
			name: "local down one",
			args: []string{"down-one", "--local-test-only"},
			want: command{kind: commandDownOne},
		},
		{
			name: "force",
			args: []string{
				"force",
				"7",
				"--confirm",
				"schema-inspected:7",
			},
			want: command{
				kind:         commandForce,
				forceVersion: 7,
				confirmation: "schema-inspected:7",
			},
		},
		{
			name:    "missing command",
			wantErr: errUsage,
		},
		{
			name:      "down one without local guard",
			args:      []string{"down-one"},
			wantErr:   errUsage,
			errorText: "--local-test-only",
		},
		{
			name: "force without confirmation",
			args: []string{
				"force",
				"7",
			},
			wantErr:   errUsage,
			errorText: "--confirm",
		},
		{
			name: "force with mismatched confirmation",
			args: []string{
				"force",
				"7",
				"--confirm",
				"schema-inspected:6",
			},
			wantErr:   errUsage,
			errorText: "schema-inspected:7",
		},
		{
			name:      "unknown command",
			args:      []string{"drop"},
			wantErr:   errUsage,
			errorText: "unknown command",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCommand(test.args)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
			if test.errorText != "" &&
				(err == nil || !strings.Contains(err.Error(), test.errorText)) {
				t.Fatalf("error = %v, want text %q", err, test.errorText)
			}
			if got != test.want {
				t.Fatalf("command = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDirtyStatusPrintsStateAndFails(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		status: platformmigration.Status{
			Version: 12,
			Dirty:   true,
			Present: true,
		},
	}
	var output bytes.Buffer

	err := executeCommand(
		command{kind: commandVersion},
		runner,
		&output,
	)
	if !errors.Is(err, errDirtyStatus) {
		t.Fatalf("error = %v, want %v", err, errDirtyStatus)
	}
	if got, want := output.String(), "version=12 dirty=true\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestNilVersionDirtyStatusRemainsVisible(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		status: platformmigration.Status{
			Version: -1,
			Dirty:   true,
			Present: true,
		},
	}
	var output bytes.Buffer

	err := executeCommand(
		command{kind: commandVersion},
		runner,
		&output,
	)
	if !errors.Is(err, errDirtyStatus) {
		t.Fatalf("error = %v, want %v", err, errDirtyStatus)
	}
	if got, want := output.String(), "version=-1 dirty=true\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestDownOneRequiresDisposableEnvironment(t *testing.T) {
	t.Parallel()

	cmd := command{kind: commandDownOne}
	for _, environment := range []string{"local", "test"} {
		if err := validateCommandEnvironment(cmd, environment); err != nil {
			t.Fatalf("environment %q was rejected: %v", environment, err)
		}
	}
	for _, environment := range []string{"", "development", "staging", "production"} {
		err := validateCommandEnvironment(cmd, environment)
		if !errors.Is(err, errUnsafeEnvironment) {
			t.Fatalf(
				"environment %q error = %v, want %v",
				environment,
				err,
				errUnsafeEnvironment,
			)
		}
	}

	if err := validateCommandEnvironment(
		command{kind: commandUp},
		"production",
	); err != nil {
		t.Fatalf("non-destructive command was rejected: %v", err)
	}
}

type fakeRunner struct {
	status platformmigration.Status
}

func (f *fakeRunner) Close() error {
	return nil
}

func (f *fakeRunner) Up() (bool, error) {
	return false, nil
}

func (f *fakeRunner) Version() (platformmigration.Status, error) {
	return f.status, nil
}

func (f *fakeRunner) DownOne() (bool, error) {
	return false, nil
}

func (f *fakeRunner) Force(int, string) error {
	return nil
}
