package migrations

import (
	"strings"
	"testing"
)

func TestEmbeddedBaselineIsTransactionalAndContainsNoBusinessDDL(t *testing.T) {
	t.Parallel()

	baselineFiles := []string{
		"000001_database_baseline.up.sql",
		"000001_database_baseline.down.sql",
	}
	for _, name := range baselineFiles {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content, err := Files.ReadFile(name)
			if err != nil {
				t.Fatalf("read embedded migration: %v", err)
			}

			sql := strings.ToUpper(strings.TrimSpace(string(content)))
			if !strings.HasPrefix(sql, "BEGIN;") {
				t.Error("migration must start with an explicit BEGIN")
			}
			if !strings.HasSuffix(sql, "COMMIT;") {
				t.Error("migration must end with an explicit COMMIT")
			}
			if strings.Contains(sql, "CREATE TABLE") {
				t.Error("database baseline must not create business tables")
			}
		})
	}
}
