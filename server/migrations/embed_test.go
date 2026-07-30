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

func TestEveryEmbeddedMigrationVersionIsUniqueAndPaired(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatalf("enumerate embedded migrations: %v", err)
	}
	type migrationPair struct {
		name string
		up   bool
		down bool
	}
	versions := make(map[string]migrationPair)
	for _, filename := range files {
		var direction string
		switch {
		case strings.HasSuffix(filename, ".up.sql"):
			direction = "up"
		case strings.HasSuffix(filename, ".down.sql"):
			direction = "down"
		default:
			t.Fatalf("migration %q has an invalid direction", filename)
		}

		name := strings.TrimSuffix(filename, "."+direction+".sql")
		version, _, ok := strings.Cut(name, "_")
		if !ok || len(version) != 6 {
			t.Fatalf("migration %q has an invalid version", filename)
		}

		pair := versions[version]
		if pair.name != "" && pair.name != name {
			t.Fatalf(
				"migration version %s is used by %q and %q",
				version,
				pair.name,
				name,
			)
		}
		pair.name = name
		if direction == "up" {
			pair.up = true
		} else {
			pair.down = true
		}
		versions[version] = pair
	}

	for version, pair := range versions {
		if !pair.up || !pair.down {
			t.Errorf(
				"migration version %s must contain matching up and down files",
				version,
			)
		}
	}
}

func TestIELTSSpeakingSectionModelMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000035_ielts_speaking_section_models.up.sql",
		"000035_ielts_speaking_section_models.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read embedded IELTS section-model migration %q: %v", name, err)
		}
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

func TestAgentImageMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000039_agent_image_assets.up.sql",
		"000039_agent_image_assets.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read embedded Agent image migration %q: %v", name, err)
		}
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
