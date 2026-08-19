package migrations

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var applicationTables = []string{
	"agent_message_attachments",
	"agent_messages",
	"agent_runs",
	"agent_threads",
	"agent_voice_drafts",
	"auth_sessions",
	"coaching_user_profiles",
	"credentials",
	"evaluation_feedback_items",
	"evaluations",
	"interview_preparations",
	"media_assets",
	"practice_plans",
	"practice_questions",
	"practice_sessions",
	"practice_turns",
	"users",
}

func TestEveryMigrationPairIsEmbedded(t *testing.T) {
	files, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"000001_clean_baseline.down.sql",
		"000001_clean_baseline.up.sql",
		"000002_agent_run_domain_completion.down.sql",
		"000002_agent_run_domain_completion.up.sql",
		"000003_archive_practice_plans.down.sql",
		"000003_archive_practice_plans.up.sql",
		"000004_question_tip_translation.down.sql",
		"000004_question_tip_translation.up.sql",
		"000005_scene_selection_source.down.sql",
		"000005_scene_selection_source.up.sql",
		"000006_user_profile_avatar.down.sql",
		"000006_user_profile_avatar.up.sql",
		"000007_pending_practice_actions.down.sql",
		"000007_pending_practice_actions.up.sql",
	}
	slices.Sort(files)
	if !slices.Equal(files, want) {
		t.Fatalf("embedded migrations = %v, want %v", files, want)
	}
}

func TestMigrationsAreTransactional(t *testing.T) {
	for _, name := range []string{
		"000001_clean_baseline.up.sql",
		"000001_clean_baseline.down.sql",
		"000002_agent_run_domain_completion.up.sql",
		"000002_agent_run_domain_completion.down.sql",
		"000003_archive_practice_plans.up.sql",
		"000003_archive_practice_plans.down.sql",
		"000004_question_tip_translation.up.sql",
		"000004_question_tip_translation.down.sql",
		"000005_scene_selection_source.up.sql",
		"000005_scene_selection_source.down.sql",
		"000006_user_profile_avatar.up.sql",
		"000006_user_profile_avatar.down.sql",
		"000007_pending_practice_actions.up.sql",
		"000007_pending_practice_actions.down.sql",
	} {
		sql := readMigration(t, name)
		if !strings.HasPrefix(sql, "BEGIN;") {
			t.Errorf("%s must start with BEGIN", name)
		}
		if !strings.HasSuffix(sql, "COMMIT;") {
			t.Errorf("%s must end with COMMIT", name)
		}
	}
}

func TestCleanBaselineCreatesExactlyTheApplicationTables(t *testing.T) {
	sql := readMigration(t, "000001_clean_baseline.up.sql")
	expression := regexp.MustCompile(`(?m)^CREATE TABLE ([a-z_]+) \(`)
	matches := expression.FindAllStringSubmatch(sql, -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	slices.Sort(got)
	if !slices.Equal(got, applicationTables) {
		t.Fatalf("created tables = %v, want %v", got, applicationTables)
	}
}

func TestCleanBaselineHasNoDatabaseProgrammingObjects(t *testing.T) {
	sql := strings.ToUpper(readMigration(t, "000001_clean_baseline.up.sql"))
	for _, forbidden := range []string{
		"CREATE EXTENSION",
		"CREATE FUNCTION",
		"CREATE TRIGGER",
		"CREATE TYPE",
		"CREATE VIEW",
		"CREATE MATERIALIZED VIEW",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("baseline must not contain %q", forbidden)
		}
	}
}

func TestCleanBaselineKeepsCriticalOwnershipAndConcurrencyConstraints(
	t *testing.T,
) {
	sql := readMigration(t, "000001_clean_baseline.up.sql")
	for _, required := range []string{
		"UNIQUE (id, user_id)",
		"FOREIGN KEY (source_thread_id, user_id)",
		"FOREIGN KEY (plan_id, user_id)",
		"FOREIGN KEY (session_id, question_id)",
		"practice_turns_one_confirmed_effective_question_idx",
		"WHERE turn_kind = 'EFFECTIVE' AND status = 'confirmed'",
		"agent_runs_one_nonterminal_per_thread_idx",
		"agent_voice_drafts_thread_idx",
		"created_at DESC, plan_id DESC",
		"practice_questions_tip_check",
		"practice_sessions_state_check",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("baseline is missing %q", required)
		}
	}
}

func TestPracticePlanArchiveMigrationExtendsStatusConstraint(t *testing.T) {
	up := readMigration(t, "000003_archive_practice_plans.up.sql")
	if !strings.Contains(up, "status IN ('draft', 'ready', 'archived')") {
		t.Fatal("practice plan archive migration must allow archived status")
	}
	down := readMigration(t, "000003_archive_practice_plans.down.sql")
	if !strings.Contains(down, "SET status = 'ready'") ||
		!strings.Contains(down, "status IN ('draft', 'ready')") {
		t.Fatal("practice plan archive rollback must preserve archived plans as ready")
	}
}

func TestQuestionTipTranslationMigrationRequiresCompleteBilingualContent(t *testing.T) {
	up := readMigration(t, "000004_question_tip_translation.up.sql")
	for _, required := range []string{
		"ADD COLUMN tip_translation text",
		"tip_content = btrim(tip_content)",
		"tip_translation = btrim(tip_translation)",
		"WHERE tip_status = 'completed'",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("Question Tip translation migration is missing %q", required)
		}
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	content, err := Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}
