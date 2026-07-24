package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	platformmigration "github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
)

const usage = `Usage:
  go run ./cmd/migrate up
  go run ./cmd/migrate version
  go run ./cmd/migrate status
  go run ./cmd/migrate down-one --local-test-only
  go run ./cmd/migrate force VERSION --confirm schema-inspected:VERSION

DATABASE_URL must be set. down-one is restricted to disposable local/test
databases and also requires MIGRATION_ENV=local or MIGRATION_ENV=test.
force only repairs a dirty migration state after manual schema inspection.
`

var (
	errUsage             = errors.New("invalid migration command")
	errDirtyStatus       = errors.New("migration state is dirty")
	errUnsafeEnvironment = errors.New("destructive migration command requires MIGRATION_ENV=local or test")
)

type commandKind string

const (
	commandUp      commandKind = "up"
	commandVersion commandKind = "version"
	commandDownOne commandKind = "down-one"
	commandForce   commandKind = "force"
)

type command struct {
	kind         commandKind
	forceVersion int
	confirmation string
}

type migrationRunner interface {
	Close() error
	Up() (bool, error)
	Version() (platformmigration.Status, error)
	DownOne() (bool, error)
	Force(version int, confirmation string) error
}

func main() {
	if err := execute(
		os.Args[1:],
		os.Getenv("DATABASE_URL"),
		os.Getenv("MIGRATION_ENV"),
		os.Stdout,
	); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "migration command failed: %v\n", err)
		if errors.Is(err, errUsage) {
			_, _ = fmt.Fprint(os.Stderr, usage)
		}
		os.Exit(1)
	}
}

func execute(
	args []string,
	databaseURL string,
	migrationEnvironment string,
	output io.Writer,
) (err error) {
	cmd, err := parseCommand(args)
	if err != nil {
		return err
	}
	if err := validateCommandEnvironment(cmd, migrationEnvironment); err != nil {
		return err
	}

	runner, err := platformmigration.Open(databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, runner.Close())
	}()

	return executeCommand(cmd, runner, output)
}

func validateCommandEnvironment(cmd command, environment string) error {
	if cmd.kind != commandDownOne {
		return nil
	}
	if environment == "local" || environment == "test" {
		return nil
	}
	return errUnsafeEnvironment
}

func executeCommand(
	cmd command,
	runner migrationRunner,
	output io.Writer,
) error {
	switch cmd.kind {
	case commandUp:
		changed, err := runner.Up()
		if err != nil {
			return err
		}
		if changed {
			_, err = fmt.Fprintln(output, "migrations=applied")
		} else {
			_, err = fmt.Fprintln(output, "migrations=unchanged")
		}
		return err

	case commandVersion:
		status, err := runner.Version()
		if err != nil {
			return err
		}
		if status.Present {
			_, err = fmt.Fprintf(
				output,
				"version=%d dirty=%t\n",
				status.Version,
				status.Dirty,
			)
		} else {
			_, err = fmt.Fprintln(output, "version=none dirty=false")
		}
		if err != nil {
			return err
		}
		if status.Dirty {
			return errDirtyStatus
		}
		return nil

	case commandDownOne:
		changed, err := runner.DownOne()
		if err != nil {
			return err
		}
		if changed {
			_, err = fmt.Fprintln(output, "migration=reverted-one")
		} else {
			_, err = fmt.Fprintln(output, "migration=unchanged")
		}
		return err

	case commandForce:
		if err := runner.Force(cmd.forceVersion, cmd.confirmation); err != nil {
			return err
		}
		_, err := fmt.Fprintf(
			output,
			"version=%d dirty=false forced=true\n",
			cmd.forceVersion,
		)
		return err

	default:
		return errUsage
	}
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return command{}, errUsage
	}

	switch args[0] {
	case "up":
		if len(args) != 1 {
			return command{}, errUsage
		}
		return command{kind: commandUp}, nil

	case "version", "status":
		if len(args) != 1 {
			return command{}, errUsage
		}
		return command{kind: commandVersion}, nil

	case "down-one":
		if len(args) != 2 || args[1] != "--local-test-only" {
			return command{}, fmt.Errorf(
				"%w: down-one requires --local-test-only",
				errUsage,
			)
		}
		return command{kind: commandDownOne}, nil

	case "force":
		if len(args) != 4 || args[2] != "--confirm" {
			return command{}, fmt.Errorf(
				"%w: force requires VERSION and --confirm",
				errUsage,
			)
		}

		version, err := strconv.Atoi(args[1])
		if err != nil {
			return command{}, fmt.Errorf("%w: invalid force VERSION", errUsage)
		}
		if err := platformmigration.ValidateForceConfirmation(
			version,
			args[3],
		); err != nil {
			return command{}, fmt.Errorf("%w: %v", errUsage, err)
		}
		return command{
			kind:         commandForce,
			forceVersion: version,
			confirmation: args[3],
		}, nil

	default:
		return command{}, fmt.Errorf("%w: unknown command %q", errUsage, args[0])
	}
}
