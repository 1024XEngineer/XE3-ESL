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

func TestSpeechFeedbackAcousticProviderBoundaryMigrationIsEmbedded(
	t *testing.T,
) {
	t.Parallel()

	for _, name := range []string{
		"000063_speech_feedback_acoustic_provider_boundary.up.sql",
		"000063_speech_feedback_acoustic_provider_boundary.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf(
				"read SpeechFeedback acoustic Provider migration %q: %v",
				name,
				err,
			)
		}
	}

	upContent, err := Files.ReadFile(
		"000063_speech_feedback_acoustic_provider_boundary.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upContent)
	for _, required := range []string{
		"octet_length(provider) BETWEEN 1 AND 128",
		"provider ~ '^[A-Za-z0-9._:-]+$'",
		"provider_session_id !~ '^[[:space:]]*$'",
		"category IN ('read_word', 'read_sentence', 'topic')",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("Provider boundary migration is missing %q", required)
		}
	}
	downContent, err := Files.ReadFile(
		"000063_speech_feedback_acoustic_provider_boundary.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down := string(downContent)
	if !strings.Contains(down, "provider = 'xfyun-ise'") {
		t.Error("Provider boundary rollback must restore the XFYUN constraint")
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
		"000042_agent_image_assets.up.sql",
		"000042_agent_image_assets.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read embedded Agent image migration %q: %v", name, err)
		}
	}
}

func TestSpeechFeedbackISEEvidenceMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000043_speech_feedback_ise_evidence.up.sql",
		"000043_speech_feedback_ise_evidence.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf(
				"read embedded SpeechFeedback ISE evidence migration %q: %v",
				name,
				err,
			)
		}
	}
}

func TestSpeechFeedbackPracticeSessionIDMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000044_speech_feedback_practice_session_ids.up.sql",
		"000044_speech_feedback_practice_session_ids.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf(
				"read embedded SpeechFeedback practice session migration %q: %v",
				name,
				err,
			)
		}
	}
}

func TestPracticeFollowUpMigrationsAreEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000047_practice_follow_up_turns.up.sql",
		"000047_practice_follow_up_turns.down.sql",
		"000048_follow_up_confirmed_turn_shape.up.sql",
		"000048_follow_up_confirmed_turn_shape.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read embedded Practice follow-up migration %q: %v", name, err)
		}
	}
}

func TestGoalAuthorityMigrationIsEmbeddedAndUsesExplicitDrops(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000049_goal_authority_models.up.sql",
		"000049_goal_authority_models.down.sql",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql := readMigration(t, name)
			for _, line := range strings.Split(sql, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "DROP TABLE") &&
					strings.Contains(line, "CASCADE") {
					t.Fatalf(
						"Goal authority migration contains cascading table drop %q",
						line,
					)
				}
			}
			if !strings.Contains(sql, "RECREATE THE DEVELOPMENT OR TEST DATABASE") {
				t.Error("Goal authority migration must explain its empty-data requirement")
			}
		})
	}
}

func TestSceneAuthorityMigrationIsEmbeddedAndHasOneVersionAuthority(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000050_scene_authority_catalog.up.sql",
		"000050_scene_authority_catalog.down.sql",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql := readMigration(t, name)
			if strings.Contains(sql, "SCENARIO_CONFIG") {
				t.Error("Scene authority migration must not create ScenarioConfig identity")
			}
			for _, line := range strings.Split(sql, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "DROP TABLE") &&
					strings.Contains(line, "CASCADE") {
					t.Fatalf(
						"Scene authority migration contains cascading table drop %q",
						line,
					)
				}
			}
		})
	}
}

func TestPreparationPlanAuthorityMigrationIsEmbeddedAndHasNoLegacyPlanTable(
	t *testing.T,
) {
	t.Parallel()

	for _, name := range []string{
		"000051_preparation_plan_authority.up.sql",
		"000051_preparation_plan_authority.down.sql",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql := readMigration(t, name)
			for _, line := range strings.Split(sql, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "DROP TABLE") &&
					strings.Contains(line, "CASCADE") {
					t.Fatalf(
						"Preparation Plan migration contains cascading table drop %q",
						line,
					)
				}
			}
			if !strings.Contains(sql, "RECREATE THE DEVELOPMENT OR TEST DATABASE") {
				t.Error("Preparation Plan migration must explain its empty-data requirement")
			}
		})
	}
}

func TestResumeMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000060_resumes.up.sql",
		"000060_resumes.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read Resume migration %q: %v", name, err)
		}
	}
}

func TestIELTSSpeakingAcousticPayloadMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000067_evaluation_ielts_speaking_acoustic_payload.up.sql",
		"000067_evaluation_ielts_speaking_acoustic_payload.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf(
				"read embedded IELTS acoustic payload migration %q: %v",
				name,
				err,
			)
		}
	}
}

func TestPreparationResumeRevisionMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000068_preparation_resume_revision.up.sql",
		"000068_preparation_resume_revision.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read Preparation Resume migration %q: %v", name, err)
		}
	}
	up := readMigration(t, "000068_preparation_resume_revision.up.sql")
	if !strings.Contains(up, "RECREATE THE DEVELOPMENT OR TEST DATABASE") {
		t.Fatal("Preparation Resume migration must state its initialization requirement")
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
