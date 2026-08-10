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

func TestIELTSSpeakingPromptV5MigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	up := readMigration(
		t,
		"000085_evaluation_ielts_speaking_prompt_v5.up.sql",
	)
	if !strings.Contains(
		up,
		"IELTS-SPEAKING-FULL-MOCK-SHADOW-PROMPT/V5",
	) {
		t.Error("IELTS Prompt v5 migration must admit the new lineage")
	}
	down := readMigration(
		t,
		"000085_evaluation_ielts_speaking_prompt_v5.down.sql",
	)
	if strings.Contains(
		down,
		"IELTS-SPEAKING-FULL-MOCK-SHADOW-PROMPT/V5",
	) {
		t.Error("IELTS Prompt v5 rollback must restore the v4 constraint")
	}
}

func TestProviderQualifiedModelIDsMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000086_provider_qualified_model_ids.up.sql")
	for _, required := range []string{
		"agent_runs_provider_check",
		"agent_runs_result_text_check",
		"agent_context_manifests_provider_check",
		"agent_memory_extraction_jobs_provider_check",
		"agent_thread_summary_checkpoints_model_check",
		"agent_thread_summary_jobs_versions_check",
		"agent_thread_title_jobs_generation_check",
		"^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$",
	} {
		if !strings.Contains(up, strings.ToUpper(required)) {
			t.Errorf("qualified model ID migration is missing %q", required)
		}
	}

	down := readMigration(t, "000086_provider_qualified_model_ids.down.sql")
	if !strings.Contains(
		down,
		strings.ToUpper("^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"),
	) {
		t.Error("qualified model ID rollback must restore the legacy model rule")
	}
}

func TestIELTSDedicatedAssignmentLimitsMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000072_ielts_dedicated_assignment_limits.up.sql")
	for _, required := range []string{
		"PART_1_QUESTIONS NOT BETWEEN 1 AND 24",
		"PART_3_QUESTIONS NOT BETWEEN 1 AND 6",
		"EXPECTED_MODE = 'FULL_MOCK'",
		"PREPARATION_PLAN_IELTS_ASSIGNMENT_IS_VALID_V1",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("IELTS dedicated limits migration is missing %q", required)
		}
	}

	down := readMigration(t, "000072_ielts_dedicated_assignment_limits.down.sql")
	if !strings.Contains(
		down,
		"PREPARATION_PLAN_IELTS_ASSIGNMENT_IS_VALID_V1",
	) {
		t.Error("IELTS dedicated limits rollback must restore v1 validation")
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

func TestSceneEvaluationPolicyReferenceMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000069_scene_evaluation_policy_ref.up.sql")
	for _, required := range []string{
		"ADD COLUMN EVALUATION_POLICY_REF",
		"INTERVIEW.SHADOW.EVALUATION.V1",
		"IELTS.SPEAKING_PRACTICE.EVALUATION.V1",
		"IELTS.SPEAKING_FULL_MOCK.EVALUATION.V1",
		"WORKPLACE.GENERAL.EVALUATION.V1",
		"DAILY.GENERAL.EVALUATION.V1",
		"SCN_PROGRAMMER_INTERVIEW",
		"SCN_IELTS_SPEAKING_FULL",
		"SCN_WORKPLACE_CUSTOM",
		"SCN_DAILY_CUSTOM",
		"SCENE VERSIONS WITHOUT AN EXPLICIT EVALUATION POLICY EXIST",
		"DISABLE TRIGGER COACHING_SCENE_VERSIONS_ARE_IMMUTABLE",
		"ENABLE TRIGGER COACHING_SCENE_VERSIONS_ARE_IMMUTABLE",
		"'EVALUATION_POLICY_REF', VERSION.EVALUATION_POLICY_REF",
		"RECREATE THE DEVELOPMENT OR TEST DATABASE",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing %q", required)
		}
	}
	if strings.Contains(up, "WHEN SCENE_FAMILY") ||
		strings.Contains(up, "WHEN SCENE_MODEL") {
		t.Error("up migration must not infer Evaluation Policy from Scene metadata")
	}

	down := readMigration(t, "000069_scene_evaluation_policy_ref.down.sql")
	if !strings.Contains(down, "DROP COLUMN EVALUATION_POLICY_REF") ||
		strings.Contains(
			down,
			"'EVALUATION_POLICY_REF', VERSION.EVALUATION_POLICY_REF",
		) {
		t.Error("down migration must restore the prior Scene JSON shape")
	}
}

func TestPracticeQuestionTipsMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"000070_practice_question_tips.up.sql",
		"000070_practice_question_tips.down.sql",
	} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatalf("read Practice Question Tip migration %q: %v", name, err)
		}
	}
}

func TestPracticeExecutionPolicyReferenceMigrationIsEmbedded(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000071_practice_execution_policy_refs.up.sql")
	for _, required := range []string{
		"RECREATE THE DEVELOPMENT OR TEST DATABASE",
		"SCN_DAILY_CUSTOM",
		"DAILY.PRACTICE.SESSION.V1",
		"SCN_WORKPLACE_CUSTOM",
		"WORKPLACE.PRACTICE.SESSION.V1",
		"SCN_INTERVIEW_CUSTOM",
		"INTERVIEW.PRACTICE.SESSION.V1",
		"SCN_SPEAKING_EXAM_CUSTOM",
		"EXAM.PRACTICE.SESSION.V1",
		"RETRY_ALLOWED",
		"QUESTION_TRANSLATION_ALLOWED",
		"JSONB_TYPEOF(PAYLOAD -> 'RETRY_ALLOWED') <> 'BOOLEAN'",
		"JSONB_TYPEOF(PAYLOAD -> 'QUESTION_TRANSLATION_ALLOWED') <> 'BOOLEAN'",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing %q", required)
		}
	}
	if strings.Contains(up, "SCENE_FAMILY") || strings.Contains(up, "SCENE_MODEL") {
		t.Error("up migration must not infer execution policy from Scene metadata")
	}

	down := readMigration(t, "000071_practice_execution_policy_refs.down.sql")
	if !strings.Contains(down, "GENERIC.PRACTICE.SESSION.V1") {
		t.Error("down migration must restore prior explicit policy references")
	}
	if !strings.Contains(
		down,
		"PRACTICE EXECUTION POLICY ROLLBACK REQUIRES EMPTY",
	) || strings.Contains(down, "QUESTION_TRANSLATION_ALLOWED") ||
		strings.Contains(down, "RETRY_ALLOWED") {
		t.Error("down migration must restore the prior Session Policy JSON shape")
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
