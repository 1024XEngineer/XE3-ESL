package postgres

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestPracticeResourceIDMigrationOnlyReplacesIdentifierChecks(
	t *testing.T,
) {
	t.Parallel()
	up, err := migrations.Files.ReadFile(
		"000038_evaluation_practice_resource_ids.up.sql",
	)
	if err != nil {
		t.Fatalf("read resource ID up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile(
		"000038_evaluation_practice_resource_ids.down.sql",
	)
	if err != nil {
		t.Fatalf("read resource ID down migration: %v", err)
	}
	for name, content := range map[string]string{
		"up":   strings.ToLower(string(up)),
		"down": strings.ToLower(string(down)),
	} {
		for _, constraint := range []string{
			"evaluation_ledgers_practice_session_check",
			"evaluation_evidence_snapshots_practice_session_check",
			"evaluation_module_runs_practice_session_check",
		} {
			if !strings.Contains(content, constraint) {
				t.Errorf("%s migration is missing %q", name, constraint)
			}
		}
		for _, mutation := range []string{"update ", "delete ", "truncate "} {
			if strings.Contains(content, mutation) {
				t.Errorf("%s migration contains %q", name, mutation)
			}
		}
	}
	if !strings.Contains(
		string(up),
		"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$",
	) {
		t.Fatal("up migration does not allow digit-leading resources")
	}
	if !strings.Contains(
		string(down),
		"^[A-Za-z][A-Za-z0-9._:-]{0,127}$",
	) {
		t.Fatal("down migration does not restore the prior constraint")
	}
}
