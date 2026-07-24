package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEveryEmbeddedMigrationIsTransactional(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatalf("enumerate embedded migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded migrations found")
	}

	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql := readMigration(t, name)
			if !strings.HasPrefix(sql, "BEGIN;") {
				t.Error("migration must start with an explicit BEGIN")
			}
			if !strings.HasSuffix(sql, "COMMIT;") {
				t.Error("migration must end with an explicit COMMIT")
			}
		})
	}
}

func TestDatabaseBaselineContainsNoBusinessDDL(t *testing.T) {
	t.Parallel()

	baselineFiles := []string{
		"000001_database_baseline.up.sql",
		"000001_database_baseline.down.sql",
	}
	for _, name := range baselineFiles {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql := readMigration(t, name)
			if strings.Contains(sql, "CREATE TABLE") {
				t.Error("database baseline must not create business tables")
			}
		})
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()

	content, err := Files.ReadFile(name)
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
	return strings.ToUpper(strings.TrimSpace(string(content)))
}
